package ghttp

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gogf/gf/v2/container/gtype"
	"github.com/gogf/gf/v2/net/gclient"
	"github.com/gogf/gf/v2/net/goai"
	"github.com/gogf/gf/v2/net/gsvc"
	"github.com/gogf/gf/v2/os/gcache"
	"github.com/gogf/gf/v2/os/gsession"
	"github.com/gogf/gf/v2/test/gtest"
)

func newBenchmarkServer(name string) *Server {
	s := &Server{
		instance:         name,
		plugins:          make([]Plugin, 0),
		servers:          nil,
		closeChan:        make(chan struct{}, 16),
		serverCount:      gtype.NewInt(),
		statusHandlerMap: make(map[string][]HandlerFunc),
		serveTree:        make(map[string]*routeTreeNode),
		serveFastTree:    make(map[string]*routeFastNode),
		serveCache:       gcache.New(routeCacheLruCap),
		routesMap:        make(map[string][]*HandlerItem),
		openapi:          goai.New(),
		registrar:        gsvc.GetRegistry(),
	}
	if err := s.SetConfig(NewConfig()); err != nil {
		panic(err)
	}
	s.config.SessionStorage = gsession.NewStorageMemory()
	s.sessionManager = gsession.New(s.config.SessionMaxAge, s.config.SessionStorage)
	s.Use(internalMiddlewareServerTracing)
	return s
}

func newBenchmarkRequest(s *Server, method, target string, body string) (*Request, *httptest.ResponseRecorder) {
	var bodyReader *strings.Reader
	if body == "" {
		bodyReader = strings.NewReader("")
	} else {
		bodyReader = strings.NewReader(body)
	}
	rawRequest := httptest.NewRequest(method, target, bodyReader)
	recorder := httptest.NewRecorder()
	request := newRequest(s, rawRequest, recorder)
	return request, recorder
}

func closeBenchmarkRequest(b *testing.B, r *Request) {
	b.Helper()
	if err := r.Session.Close(); err != nil {
		b.Fatal(err)
	}
	r.Response.BufferWriter.Close()
	_ = r.Body.Close()
	releaseRequest(r)
}

func BenchmarkInternal_NewRequest(b *testing.B) {
	s := newBenchmarkServer("bench-request")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, _ := newBenchmarkRequest(s, http.MethodGet, "http://127.0.0.1/user/profile?id=1", "")
		closeBenchmarkRequest(b, r)
	}
}

func BenchmarkInternal_RouteSearch(b *testing.B) {
	handler := func(r *Request) {
		r.Response.Write("ok")
	}
	s := newBenchmarkServer("bench-router")
	for i := 0; i < 128; i++ {
		s.BindHandler(fmt.Sprintf("/api/static/%d/detail", i), handler)
	}
	s.BindHandler("/api/user/:id", handler)
	s.BindHandler("/api/user/:id/profile", handler)
	s.BindHandler("/api/files/*path", handler)
	s.BindHandler("/api/order/{id}.json", handler)
	s.BindHandler("/admin-goods-{page}", handler)
	s.BindHandler("/{hash}.{type}", handler)
	sNoComplex := newBenchmarkServer("bench-router-no-complex")
	sNoComplex.SetRouteComplexEnabled(false)
	for i := 0; i < 128; i++ {
		sNoComplex.BindHandler(fmt.Sprintf("/api/static/%d/detail", i), handler)
	}
	sNoComplex.BindHandler("/api/user/:id", handler)
	sNoComplex.BindHandler("/api/user/:id/profile", handler)
	sNoComplex.BindHandler("/api/files/*path", handler)
	sNoComplex.BindHandler("/api/order/{id}.json", handler)
	sNoComplex.BindHandler("/admin-goods-{page}", handler)
	sNoComplex.BindHandler("/{hash}.{type}", handler)

	b.Run("search_dynamic", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			handlers, serveItem, hasHook, hasServe := s.searchHandlers(http.MethodGet, fmt.Sprintf("/api/user/%d/profile", i), "")
			if len(handlers) == 0 || serveItem == nil || hasHook || !hasServe {
				b.Fatal("route search failed")
			}
		}
	})
	b.Run("search_fast_dynamic", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			handlers, serveItem, hasHook, hasServe := s.searchFastHandlers(http.MethodGet, fmt.Sprintf("/api/user/%d/profile", i), "")
			if len(handlers) == 0 || serveItem == nil || hasHook || !hasServe {
				b.Fatal("fast route search failed")
			}
		}
	})
	b.Run("search_fast_dynamic_no_complex", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			handlers, serveItem, hasHook, hasServe := sNoComplex.searchFastHandlers(http.MethodGet, fmt.Sprintf("/api/user/%d/profile", i), "")
			if len(handlers) == 0 || serveItem == nil || hasHook || !hasServe {
				b.Fatal("fast route search without complex fallback failed")
			}
		}
	})
	b.Run("search_mixed", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			handlers, serveItem, _, hasServe := s.searchHandlers(http.MethodGet, fmt.Sprintf("/admin-goods-%d", i), "")
			if len(handlers) == 0 || serveItem == nil || !hasServe {
				b.Fatal("mixed route search failed")
			}
		}
	})
	b.Run("search_fast_pattern_segment", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			handlers, serveItem, _, hasServe := s.searchFastHandlers(http.MethodGet, fmt.Sprintf("/api/order/%d.json", i), "")
			if len(handlers) == 0 || serveItem == nil || !hasServe {
				b.Fatal("fast pattern segment route search failed")
			}
		}
	})
	b.Run("search_fast_pattern_vs_param_overlap", func(b *testing.B) {
		sOverlap := newBenchmarkServer("bench-router-pattern-overlap")
		sOverlap.BindHandler("/api/order/:id", handler)
		sOverlap.BindHandler("/api/order/{id}.json", handler)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			handlers, serveItem, _, hasServe := sOverlap.searchFastHandlers(
				http.MethodGet,
				fmt.Sprintf("/api/order/%d.json", i),
				"",
			)
			if len(handlers) == 0 || serveItem == nil || !hasServe {
				b.Fatal("fast pattern overlap route search failed")
			}
		}
	})
}

func BenchmarkInternal_ParseRuleLookup(b *testing.B) {
	b.Run("serial", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if getCustomParseFunc("trim-space") == nil {
				b.Fatal("parse rule not found")
			}
		}
	})
	b.Run("parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if getCustomParseFunc("trim-space") == nil {
					b.Fatal("parse rule not found")
				}
			}
		})
	})
}

func TestInternal_RouteFastPath_MixedFallback(t *testing.T) {
	handler := func(r *Request) {
		r.Response.Write("ok")
	}
	s := newBenchmarkServer("test-router-fast")
	s.BindHandler("/api/user/:id/profile", handler)
	s.BindHandler("/api/object/{id}", handler)
	s.BindHandler("/api/files/*path", handler)
	s.BindHandler("/api/order/{id}.json", handler)
	s.BindHandler("/{hash}.{type}", handler)

	handlers, serveItem, hasHook, hasServe := s.searchFastHandlers(http.MethodGet, "/api/user/123/profile", "")
	if len(handlers) == 0 || serveItem == nil || hasHook || !hasServe {
		t.Fatal("simple REST route should use fast path")
	}
	if serveItem.Values["id"] != "123" {
		t.Fatalf(`unexpected route param "id": %q`, serveItem.Values["id"])
	}

	handlers, serveItem, hasHook, hasServe = s.searchFastHandlers(http.MethodGet, "/api/object/456", "")
	if len(handlers) == 0 || serveItem == nil || hasHook || !hasServe {
		t.Fatal("whole-segment {id} route should use fast path")
	}
	if serveItem.Values["id"] != "456" {
		t.Fatalf(`unexpected brace route param "id": %q`, serveItem.Values["id"])
	}

	handlers, serveItem, hasHook, hasServe = s.searchFastHandlers(http.MethodGet, "/api/files/a/b/c", "")
	if len(handlers) == 0 || serveItem == nil || hasHook || !hasServe {
		t.Fatal("tail catch-all route should use fast path")
	}
	if serveItem.Values["path"] != "a/b/c" {
		t.Fatalf(`unexpected catch-all param "path": %q`, serveItem.Values["path"])
	}

	handlers, serveItem, hasHook, hasServe = s.searchFastHandlers(http.MethodGet, "/data.json", "")
	if len(handlers) == 0 || serveItem == nil || hasHook || !hasServe {
		t.Fatal("complex route should use fast path")
	}
	if serveItem.Values["hash"] != "data" || serveItem.Values["type"] != "json" {
		t.Fatalf("unexpected complex route params: %#v", serveItem.Values)
	}

	handlers, serveItem, hasHook, hasServe = s.searchFastHandlers(http.MethodGet, "/api/order/123.json", "")
	if len(handlers) == 0 || serveItem == nil || hasHook || !hasServe {
		t.Fatal("static-prefix complex route should use fast path")
	}
	if serveItem.Values["id"] != "123" {
		t.Fatalf("unexpected static-prefix complex route params: %#v", serveItem.Values)
	}

	s = newBenchmarkServer("test-router-fast-no-complex")
	s.SetRouteComplexEnabled(false)
	s.BindHandler("/:name", handler)
	s.BindHandler("/{hash}.{type}", handler)
	handlers, serveItem, hasHook, hasServe = s.searchFastHandlers(http.MethodGet, "/data.json", "")
	if len(handlers) == 0 || serveItem == nil || hasHook || !hasServe {
		t.Fatal("disabled complex routing should keep the simple fast path result")
	}
	if serveItem.Values["name"] != "data.json" {
		t.Fatalf(`unexpected fast route param "name": %q`, serveItem.Values["name"])
	}

	s = newBenchmarkServer("test-router-fast-complex-disabled")
	s.SetRouteComplexEnabled(false)
	s.BindHandler("/{hash}.{type}", handler)
	request, _ := newBenchmarkRequest(s, http.MethodGet, "http://127.0.0.1/data.json", "")
	handlers, serveItem, hasHook, hasServe = s.getHandlersWithCache(request)
	if serveItem != nil || hasServe {
		t.Fatal("disabled complex routing should not fall back to the compatibility matcher")
	}
	_ = handlers
	_ = hasHook
}

func TestInternal_RouteFastPath_PatternSegmentsGolden(t *testing.T) {
	s := newBenchmarkServer("test-router-fast-pattern-golden")
	s.BindHandler("/api/order/list", writeRouteContent("list"))
	s.BindHandler("/api/order/list.json", writeRouteContent("list-json"))
	s.BindHandler("/api/order/:id", func(r *Request) {
		r.Response.Write("colon:", r.Get("id", ""))
	})
	s.BindHandler("/api/order/{id}", func(r *Request) {
		r.Response.Write("brace:", r.Get("id", ""))
	})
	s.BindHandler("/api/order/*name", func(r *Request) {
		r.Response.Write("any:", r.Get("name", ""))
	})
	s.BindHandler("/api/order/{id}.json", func(r *Request) {
		r.Response.Write("json:", r.Get("id", ""))
	})
	s.BindHandler("/api/order/{id}.{ext}", func(r *Request) {
		r.Response.Write("ext:", r.Get("id", ""), ".", r.Get("ext", ""))
	})
	s.BindHandler("/api/order/file-{id}.json", func(r *Request) {
		r.Response.Write("file:", r.Get("id", ""))
	})
	for _, path := range []string{
		"/api/order/list",
		"/api/order/123",
		"/api/order/123.json",
		"/api/order/a%2Bb.json",
		"/api/order/list.json",
		"/api/order/file-123.json",
		"/api/order/123.txt",
		"/api/order",
		"/api/order/",
		"/api/order/a/b/c",
	} {
		assertFastSearchEqualsLegacy(t, s, http.MethodGet, path, "")
	}
	prefix := startRouteSearchTestServer(t, s)
	gtest.C(t, func(t *gtest.T) {
		client := gclient.New()
		client.SetPrefix(prefix)
		t.Assert(client.GetContent(context.TODO(), "/api/order/list"), "list")
		t.Assert(client.GetContent(context.TODO(), "/api/order/123"), "colon:123")
		t.Assert(client.GetContent(context.TODO(), "/api/order/123.json"), "json:123")
		t.Assert(client.GetContent(context.TODO(), "/api/order/a%2Bb.json"), "json:a+b")
		t.Assert(client.GetContent(context.TODO(), "/api/order/list.json"), "list-json")
		t.Assert(client.GetContent(context.TODO(), "/api/order/file-123.json"), "file:123")
		t.Assert(client.GetContent(context.TODO(), "/api/order/123.txt"), "colon:123.txt")
		t.Assert(client.GetContent(context.TODO(), "/api/order/a/b/c"), "any:a/b/c")
	})
	_, serveItem, _, hasServe := s.searchFastHandlers(http.MethodGet, "/api/order/123", "")
	if serveItem == nil || !hasServe || serveItem.Handler.Router.Uri != "/api/order/:id" {
		t.Fatalf("whole-segment duplicate should keep the first :id route, got %#v", serveItem)
	}
}

func TestInternal_RouteFastPath_PatternSegmentsPriority(t *testing.T) {
	s := newBenchmarkServer("test-router-fast-pattern-priority")
	s.BindHandler("/api/order/:id", writeRouteContent("colon"))
	s.BindHandler("/api/order/*name", writeRouteContent("any"))
	s.BindHandler("/api/order/{id}.json", writeRouteContent("json"))
	s.BindHandler("/api/order/{id}.{ext}", writeRouteContent("ext"))
	s.BindHandler("/api/order/file-{id}.json", writeRouteContent("file"))
	s.BindHandler("/api/order/list", writeRouteContent("list"))
	s.BindHandler("/api/order/list.json", writeRouteContent("list-json"))

	for path, route := range map[string]string{
		"/api/order/list":          "/api/order/list",
		"/api/order/list.json":     "/api/order/list.json",
		"/api/order/123.json":      "/api/order/{id}.json",
		"/api/order/file-123.json": "/api/order/file-{id}.json",
		"/api/order/123":           "/api/order/:id",
		"/api/order/a/b":           "/api/order/*name",
	} {
		_, serveItem, _, hasServe := s.searchFastHandlers(http.MethodGet, path, "")
		if serveItem == nil || !hasServe {
			t.Fatalf("route %s should be served by fast path", path)
		}
		if serveItem.Handler.Router.Uri != route {
			t.Fatalf("unexpected route for %s: got %s, want %s", path, serveItem.Handler.Router.Uri, route)
		}
	}
}

func TestInternal_RouteFastPath_WholeNamedDuplicateSkipped(t *testing.T) {
	s := newBenchmarkServer("test-router-fast-whole-named-duplicate")
	s.BindHandler("/api/order/:id", func(r *Request) {
		r.Response.Write("colon:", r.Get("id", ""))
	})
	s.BindHandler("/api/order/{id}", func(r *Request) {
		r.Response.Write("brace:", r.Get("id", ""))
	})
	prefix := startRouteSearchTestServer(t, s)
	gtest.C(t, func(t *gtest.T) {
		client := gclient.New()
		client.SetPrefix(prefix)
		t.Assert(client.GetContent(context.TODO(), "/api/order/123"), "colon:123")
	})
	assertRouteRegistered(t, s, "/api/order/:id", true)
	assertRouteRegistered(t, s, "/api/order/{id}", false)

	s = newBenchmarkServer("test-router-fast-whole-named-duplicate-reverse")
	s.BindHandler("/api/order/{id}", func(r *Request) {
		r.Response.Write("brace:", r.Get("id", ""))
	})
	s.BindHandler("/api/order/:id", func(r *Request) {
		r.Response.Write("colon:", r.Get("id", ""))
	})
	prefix = startRouteSearchTestServer(t, s)
	gtest.C(t, func(t *gtest.T) {
		client := gclient.New()
		client.SetPrefix(prefix)
		t.Assert(client.GetContent(context.TODO(), "/api/order/123"), "brace:123")
	})
	assertRouteRegistered(t, s, "/api/order/{id}", true)
	assertRouteRegistered(t, s, "/api/order/:id", false)
}

func TestInternal_RouteFastPath_PatternSegmentsNestedGolden(t *testing.T) {
	s := newBenchmarkServer("test-router-fast-pattern-nested")
	s.BindHandler("/api/file/{name}.json/detail", func(r *Request) {
		r.Response.Write("file:", r.Get("name", ""))
	})
	s.BindHandler("/api/:type/{id}.json/detail", func(r *Request) {
		r.Response.Write("type:", r.Get("type", ""), ":", r.Get("id", ""))
	})
	s.BindHandler("/api/{version}/file-{id}.json", func(r *Request) {
		r.Response.Write("route:", r.Get("version", ""), ":", r.Get("id", ""))
	})
	s.BindMiddleware("/api/{version}/file-{id}.json", func(r *Request) {
		r.Response.Write("mw:", r.Get("version", ""), ":", r.Get("id", ""), "|")
		r.Middleware.Next()
	})
	s.BindHookHandlerByMap("/api/{version}/file-{id}.json", map[HookName]HandlerFunc{
		HookBeforeServe: func(r *Request) {},
	})

	for _, path := range []string{
		"/api/file/report.json/detail",
		"/api/order/123.json/detail",
		"/api/v1/file-123.json",
	} {
		assertFastSearchEqualsLegacy(t, s, http.MethodGet, path, "")
	}
	prefix := startRouteSearchTestServer(t, s)
	gtest.C(t, func(t *gtest.T) {
		client := gclient.New()
		client.SetPrefix(prefix)
		t.Assert(client.GetContent(context.TODO(), "/api/file/report.json/detail"), "file:report")
		t.Assert(client.GetContent(context.TODO(), "/api/order/123.json/detail"), "type:order:123")
		t.Assert(client.GetContent(context.TODO(), "/api/v1/file-123.json"), "mw:v1:123|route:v1:123")
	})
}

func TestInternal_RouteFastPath_PatternSegmentsComplexDisabled(t *testing.T) {
	s := newBenchmarkServer("test-router-fast-pattern-disabled")
	s.SetRouteComplexEnabled(false)
	s.BindHandler("/api/order/{id}.json", func(r *Request) {
		r.Response.Write("json:", r.Get("id", ""))
	})
	s.BindHandler("/api/order/{id}.{ext}", func(r *Request) {
		r.Response.Write("ext:", r.Get("id", ""), ".", r.Get("ext", ""))
	})
	s.BindHandler("/api/order/file-{id}.json", func(r *Request) {
		r.Response.Write("file:", r.Get("id", ""))
	})
	for _, path := range []string{
		"/api/order/123.json",
		"/api/order/123.txt",
		"/api/order/file-123.json",
	} {
		_, serveItem, _, hasServe := s.searchFastHandlers(http.MethodGet, path, "")
		if serveItem != nil || hasServe {
			t.Fatalf("disabled complex routing should not match pattern route for %s", path)
		}
	}
	prefix := startRouteSearchTestServer(t, s)
	gtest.C(t, func(t *gtest.T) {
		client := gclient.New()
		client.SetPrefix(prefix)
		t.Assert(client.GetContent(context.TODO(), "/api/order/123.json"), "Not Found")
		t.Assert(client.GetContent(context.TODO(), "/api/order/123.txt"), "Not Found")
		t.Assert(client.GetContent(context.TODO(), "/api/order/file-123.json"), "Not Found")
	})

	s = newBenchmarkServer("test-router-fast-simple-disabled")
	s.SetRouteComplexEnabled(false)
	s.BindHandler("/api/order/list", writeRouteContent("list"))
	s.BindHandler("/api/order/{id}", func(r *Request) {
		r.Response.Write("brace:", r.Get("id", ""))
	})
	s.BindHandler("/api/order/:name", func(r *Request) {
		r.Response.Write("colon:", r.Get("name", ""))
	})
	s.BindHandler("/api/files/*path", func(r *Request) {
		r.Response.Write("files:", r.Get("path", ""))
	})
	for _, path := range []string{
		"/api/order/list",
		"/api/order/123",
		"/api/order/john",
		"/api/files/a/b/c",
	} {
		handlers, serveItem, hasHook, hasServe := s.searchFastHandlers(http.MethodGet, path, "")
		if len(handlers) == 0 || serveItem == nil || hasHook || !hasServe {
			t.Fatalf("disabled complex routing should keep simple fast route for %s", path)
		}
	}
	prefix = startRouteSearchTestServer(t, s)
	gtest.C(t, func(t *gtest.T) {
		client := gclient.New()
		client.SetPrefix(prefix)
		t.Assert(client.GetContent(context.TODO(), "/api/order/list"), "list")
		t.Assert(client.GetContent(context.TODO(), "/api/order/123"), "colon:123")
		t.Assert(client.GetContent(context.TODO(), "/api/files/a/b/c"), "files:a/b/c")
	})
}

func TestInternal_RouteFastPath_RegexOnlyFallback(t *testing.T) {
	if _, ok := compileFastRoutePlan("/api/order/a*b"); ok {
		t.Fatal("segment wildcard regex route should stay in fallback")
	}
	if _, ok := compileFastRoutePlan("/api/*name/detail"); ok {
		t.Fatal("non-tail catch-all route should stay in fallback")
	}

	handler := func(r *Request) {
		r.Response.Write("regex")
	}
	s := newBenchmarkServer("test-router-fast-regex-fallback")
	s.BindHandler("/api/order/a*b", handler)
	handlers, serveItem, hasHook, hasServe := s.searchFastHandlers(http.MethodGet, "/api/order/a.b", "")
	if serveItem != nil || hasHook || hasServe {
		t.Fatal("regex-only route should not be served by fast path")
	}
	_ = handlers
	handlers, serveItem, hasHook, hasServe = s.searchHandlers(http.MethodGet, "/api/order/a.b", "")
	if len(handlers) == 0 || serveItem == nil || hasHook || !hasServe {
		t.Fatal("regex-only route should still be served by compatibility matcher")
	}
	prefix := startRouteSearchTestServer(t, s)
	gtest.C(t, func(t *gtest.T) {
		client := gclient.New()
		client.SetPrefix(prefix)
		t.Assert(client.GetContent(context.TODO(), "/api/order/a.b"), "regex")
	})

	s = newBenchmarkServer("test-router-fast-regex-fallback-disabled")
	s.SetRouteComplexEnabled(false)
	s.BindHandler("/api/order/a*b", handler)
	prefix = startRouteSearchTestServer(t, s)
	gtest.C(t, func(t *gtest.T) {
		client := gclient.New()
		client.SetPrefix(prefix)
		t.Assert(client.GetContent(context.TODO(), "/api/order/a.b"), "Not Found")
	})
}

func TestInternal_RouteFastPath_PatternDomainGolden(t *testing.T) {
	s := newBenchmarkServer("test-router-fast-pattern-domain")
	s.BindMiddleware("/api/{id}.json", func(r *Request) {
		r.Response.Write("default-mw:", r.Get("id", ""), "|")
		r.Middleware.Next()
	})
	s.Domain("example.com").BindMiddleware("/api/{id}.json", func(r *Request) {
		r.Response.Write("domain-mw:", r.Get("id", ""), "|")
		r.Middleware.Next()
	})
	s.Domain("example.com").BindHandler("/api/{id}.json", func(r *Request) {
		r.Response.Write("domain:", r.Get("id", ""))
	})

	assertFastSearchEqualsLegacy(t, s, http.MethodGet, "/api/123.json", "example.com")
	prefix := startRouteSearchTestServer(t, s)
	gtest.C(t, func(t *gtest.T) {
		client := gclient.New()
		client.SetPrefix(prefix)
		client.SetHeader("Host", "example.com")
		t.Assert(client.GetContent(context.TODO(), "/api/123.json"), "default-mw:123|domain-mw:123|domain:123")
	})
}

func TestInternal_RouteFastPath_DomainNamedGolden(t *testing.T) {
	s := newBenchmarkServer("test-router-fast-domain-named")
	d := s.Domain("localhost, local")
	d.BindHandler("/:name", func(r *Request) {
		r.Response.Write("/:name")
	})
	d.BindHandler("/:name/update", func(r *Request) {
		r.Response.Write(r.Get("name"))
	})
	d.BindHandler("/:name/:action", func(r *Request) {
		r.Response.Write(r.Get("action"))
	})
	d.BindHandler("/:name/*any", func(r *Request) {
		r.Response.Write(r.Get("any"))
	})
	d.BindHandler("/user/list/{field}.html", func(r *Request) {
		r.Response.Write(r.Get("field"))
	})

	assertFastSearchEqualsLegacy(t, s, http.MethodGet, "/john/update", "localhost")
	assertFastSearchEqualsLegacy(t, s, http.MethodGet, "/john/edit", "localhost")
	assertFastSearchEqualsLegacy(t, s, http.MethodGet, "/user/list/100.html", "localhost")
	assertFastSearchEqualsLegacy(t, s, http.MethodGet, "/john/update", "local")
	prefix := startRouteSearchTestServer(t, s)
	gtest.C(t, func(t *gtest.T) {
		client := gclient.New()
		client.SetPrefix(prefix)
		client.SetHeader("Host", "localhost")
		t.Assert(client.GetContent(context.TODO(), "/john"), "")
		t.Assert(client.GetContent(context.TODO(), "/john/update"), "john")
		t.Assert(client.GetContent(context.TODO(), "/john/edit"), "edit")
		t.Assert(client.GetContent(context.TODO(), "/user/list/100.html"), "100")
	})
	gtest.C(t, func(t *gtest.T) {
		client := gclient.New()
		client.SetPrefix(prefix)
		client.SetHeader("Host", "local")
		t.Assert(client.GetContent(context.TODO(), "/john"), "")
		t.Assert(client.GetContent(context.TODO(), "/john/update"), "john")
		t.Assert(client.GetContent(context.TODO(), "/john/edit"), "edit")
		t.Assert(client.GetContent(context.TODO(), "/user/list/100.html"), "100")
	})
}

func TestInternal_RouteFastPath_StaticRouteKeepsParamMiddleware(t *testing.T) {
	handler := func(r *Request) {
		r.Response.Write("ok")
	}
	s := newBenchmarkServer("test-router-fast-static-param-middleware")
	s.BindHandler("/test/test", handler)
	s.BindMiddleware("/test/:name", handler)

	handlers, serveItem, hasHook, hasServe := s.searchFastHandlers(http.MethodGet, "/test/test", "")
	if len(handlers) == 0 || serveItem == nil || hasHook || !hasServe {
		t.Fatal("static route should be served by fast path")
	}
	if serveItem.Handler.Router.Uri != "/test/test" {
		t.Fatalf("unexpected serve route: %s", serveItem.Handler.Router.Uri)
	}
	for _, item := range handlers {
		if item.Handler.Type == HandlerTypeMiddleware && item.Handler.Router.Uri == "/test/:name" {
			if item.Values["name"] != "test" {
				t.Fatalf(`unexpected middleware route param "name": %q`, item.Values["name"])
			}
			return
		}
	}
	t.Fatal("parameter middleware should be included when a static sibling route serves the request")
}

func TestInternal_RouteFastPath_CatchAllPriority(t *testing.T) {
	handler := func(r *Request) {
		r.Response.Write("ok")
	}
	s := newBenchmarkServer("test-router-fast-catch-all-priority")
	s.BindHandler("/:name", handler)
	s.BindHandler("/:name/*any", handler)

	_, serveItem, _, hasServe := s.searchFastHandlers(http.MethodGet, "/john", "")
	if serveItem == nil || !hasServe {
		t.Fatal("route should be served by fast path")
	}
	if serveItem.Handler.Router.Uri != "/:name/*any" {
		t.Fatalf("catch-all route should keep compatibility priority, got %s", serveItem.Handler.Router.Uri)
	}
	if serveItem.Values["any"] != "" {
		t.Fatalf(`unexpected catch-all route param "any": %q`, serveItem.Values["any"])
	}
}

func TestInternal_RouteFastPath_DomainMergesDefaultMiddleware(t *testing.T) {
	handler := func(r *Request) {
		r.Response.Write("ok")
	}
	s := newBenchmarkServer("test-router-fast-domain-merge")
	s.Domain("localhost").BindHandler("/hello", handler)

	handlers, serveItem, hasHook, hasServe := s.searchFastHandlers(http.MethodGet, "/hello", "localhost")
	if len(handlers) == 0 || serveItem == nil || hasHook || !hasServe {
		t.Fatal("domain route should be served by fast path")
	}
	if serveItem.Handler.Router.Domain != "localhost" {
		t.Fatalf("unexpected serve domain: %s", serveItem.Handler.Router.Domain)
	}
	if handlers[0].Handler.Type != HandlerTypeMiddleware || handlers[0].Handler.Router.Domain != DefaultDomainName {
		t.Fatalf("default-domain middleware should be preserved before domain handler: %#v", handlers[0].Handler.Router)
	}
	if handlers[len(handlers)-1] != serveItem {
		t.Fatal("domain serve handler should remain the final serving item")
	}
}

func assertFastSearchEqualsLegacy(t *testing.T, s *Server, method, path, domain string) {
	t.Helper()
	fastItems, fastServe, fastHasHook, fastHasServe := s.searchFastHandlers(method, path, domain)
	legacyItems, legacyServe, legacyHasHook, legacyHasServe := s.searchHandlers(method, path, domain)
	if fastHasHook != legacyHasHook {
		t.Fatalf("%s hasHook mismatch: fast=%v legacy=%v", path, fastHasHook, legacyHasHook)
	}
	if fastHasServe != legacyHasServe {
		t.Fatalf("%s hasServe mismatch: fast=%v legacy=%v", path, fastHasServe, legacyHasServe)
	}
	if handlerItemID(fastServe) != handlerItemID(legacyServe) {
		t.Fatalf(
			"%s serve handler mismatch: fast=%d legacy=%d",
			path,
			handlerItemID(fastServe),
			handlerItemID(legacyServe),
		)
	}
	if len(fastItems) != len(legacyItems) {
		t.Fatalf("%s parsed item count mismatch: fast=%d legacy=%d", path, len(fastItems), len(legacyItems))
	}
	for i := range fastItems {
		if fastItems[i].Handler.Id != legacyItems[i].Handler.Id {
			t.Fatalf(
				"%s parsed item %d handler mismatch: fast=%s legacy=%s",
				path,
				i,
				describeParsedRouteItem(fastItems[i]),
				describeParsedRouteItem(legacyItems[i]),
			)
		}
		if !reflect.DeepEqual(fastItems[i].Values, legacyItems[i].Values) {
			t.Fatalf(
				"%s parsed item %d values mismatch: fast=%#v legacy=%#v",
				path,
				i,
				fastItems[i].Values,
				legacyItems[i].Values,
			)
		}
	}
}

func startRouteSearchTestServer(t *testing.T, s *Server) string {
	t.Helper()
	s.SetDumpRouterMap(false)
	s.Start()
	t.Cleanup(func() {
		s.Shutdown()
	})
	time.Sleep(100 * time.Millisecond)
	return fmt.Sprintf("http://127.0.0.1:%d", s.GetListenedPort())
}

func writeRouteContent(content string) HandlerFunc {
	return func(r *Request) {
		r.Response.Write(content)
	}
}

func assertRouteRegistered(t *testing.T, s *Server, route string, expected bool) {
	t.Helper()
	for _, item := range s.GetRoutes() {
		if item.Type == HandlerTypeHandler && item.Route == route {
			if !expected {
				t.Fatalf("route %s should not be registered", route)
			}
			return
		}
	}
	if expected {
		t.Fatalf("route %s should be registered", route)
	}
}

func handlerItemID(item *HandlerItemParsed) int {
	if item == nil || item.Handler == nil {
		return 0
	}
	return item.Handler.Id
}

func describeParsedRouteItem(item *HandlerItemParsed) string {
	if item == nil || item.Handler == nil {
		return "<nil>"
	}
	return fmt.Sprintf(
		"id=%d type=%s uri=%s hook=%s values=%v",
		item.Handler.Id,
		item.Handler.Type,
		item.Handler.Router.Uri,
		item.Handler.HookName,
		item.Values,
	)
}

func TestInternal_RequestStructPool(t *testing.T) {
	type benchReq struct {
		Name string `json:"name"`
	}
	var got string
	s := newBenchmarkServer("test-request-struct-pool")
	s.SetRequestStructPoolEnabled(true)
	funcInfo, err := s.checkAndCreateFuncInfo(
		func(ctx context.Context, req *benchReq) (res any, err error) {
			got = req.Name
			return req.Name, nil
		},
		"",
		"",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	handlerItem := &HandlerItem{
		Type: HandlerTypeHandler,
		Info: funcInfo,
	}
	for _, name := range []string{"john", "smith"} {
		request, _ := newBenchmarkRequest(
			s,
			http.MethodPost,
			"http://127.0.0.1/bench/request-pool",
			fmt.Sprintf(`{"name":"%s"}`, name),
		)
		request.Header.Set("Content-Type", "application/json")
		request.serveHandler = &HandlerItemParsed{Handler: handlerItem}
		request.handlers = []*HandlerItemParsed{request.serveHandler}
		funcInfo.Func(request)
		if request.error != nil {
			t.Fatal(request.error)
		}
		if got != name {
			t.Fatalf("unexpected pooled request value: got %q, want %q", got, name)
		}
		if err = request.Session.Close(); err != nil {
			t.Fatal(err)
		}
		request.Response.BufferWriter.Close()
		_ = request.Body.Close()
		releaseRequest(request)
	}
}

func TestInternal_CreateRouterFunc_ContextOnly(t *testing.T) {
	var called bool
	s := newBenchmarkServer("test-router-func-context-only")
	handler := func(ctx context.Context) error {
		called = true
		return nil
	}
	funcInfo := handlerFuncInfo{
		Type:  reflect.TypeOf(handler),
		Value: reflect.ValueOf(handler),
	}
	funcInfo.Func = createRouterFunc(funcInfo)
	request, _ := newBenchmarkRequest(s, http.MethodGet, "http://127.0.0.1/bench/context-only", "")
	funcInfo.Func(request)
	if request.error != nil {
		t.Fatal(request.error)
	}
	if !called {
		t.Fatal("context-only handler was not called")
	}
	if err := request.Session.Close(); err != nil {
		t.Fatal(err)
	}
	request.Response.BufferWriter.Close()
	_ = request.Body.Close()
	releaseRequest(request)
}

func BenchmarkInternal_GzipMiddleware(b *testing.B) {
	s := newBenchmarkServer("bench-gzip")
	payload := strings.Repeat("gzip-payload-", 1024)
	handlerItem := &HandlerItem{
		Type: HandlerTypeHandler,
		Info: handlerFuncInfo{
			Func: func(r *Request) {
				r.Response.WriteString(payload)
			},
		},
	}
	gzipItem := &HandlerItem{
		Type: HandlerTypeMiddleware,
		Info: handlerFuncInfo{
			Func: MiddlewareGzip,
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		request, _ := newBenchmarkRequest(s, http.MethodGet, "http://127.0.0.1/bench/gzip", "")
		request.Header.Set("Accept-Encoding", "gzip")
		request.handlers = []*HandlerItemParsed{
			{Handler: gzipItem},
			{Handler: handlerItem},
		}
		request.serveHandler = request.handlers[1]
		request.Middleware.Next()
		request.Response.Flush()
		closeBenchmarkRequest(b, request)
	}
}

func BenchmarkInternal_StrictBind(b *testing.B) {
	benchmarkInternalStrictBind(b, false)
}

func BenchmarkInternal_StrictBindRequestPool(b *testing.B) {
	benchmarkInternalStrictBind(b, true)
}

func BenchmarkInternal_StrictHandler(b *testing.B) {
	benchmarkInternalStrictHandler(b, false)
}

func BenchmarkInternal_StrictHandlerRequestPool(b *testing.B) {
	benchmarkInternalStrictHandler(b, true)
}

func benchmarkInternalStrictBind(b *testing.B, requestStructPoolEnabled bool) {
	type benchReq struct {
		Name  string `json:"name" v:"required"`
		Email string `json:"email" v:"required|email"`
		Age   int    `json:"age" v:"min:1|max:200"`
	}

	s := newBenchmarkServer("bench-bind")
	s.SetRequestStructPoolEnabled(requestStructPoolEnabled)
	funcInfo, err := s.checkAndCreateFuncInfo(
		func(ctx context.Context, req *benchReq) (res any, err error) {
			return req.Name, nil
		},
		"",
		"",
		"",
	)
	if err != nil {
		b.Fatal(err)
	}
	handlerItem := &HandlerItem{
		Type: HandlerTypeHandler,
		Info: funcInfo,
	}
	payload := `{"name":"john","email":"john@example.com","age":18}`

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		request, _ := newBenchmarkRequest(s, http.MethodPost, "http://127.0.0.1/bench/strict", payload)
		request.Header.Set("Content-Type", "application/json")
		request.serveHandler = &HandlerItemParsed{Handler: handlerItem}
		request.handlers = []*HandlerItemParsed{request.serveHandler}
		funcInfo.Func(request)
		if request.error != nil {
			b.Fatal(request.error)
		}
		request.Response.Flush()
		closeBenchmarkRequest(b, request)
	}
}

func benchmarkInternalStrictHandler(b *testing.B, requestStructPoolEnabled bool) {
	type benchReq struct {
		Name  string `json:"name" v:"required"`
		Email string `json:"email" v:"required|email"`
		Age   int    `json:"age" v:"min:1|max:200"`
	}

	s := newBenchmarkServer("bench-strict-handler")
	s.SetRequestStructPoolEnabled(requestStructPoolEnabled)
	fixedRes := "john"
	funcInfo, err := s.checkAndCreateFuncInfo(
		NewStrictHandler(func(ctx context.Context, req *benchReq) (res *string, err error) {
			return &fixedRes, nil
		}),
		"",
		"",
		"",
	)
	if err != nil {
		b.Fatal(err)
	}
	handlerItem := &HandlerItem{
		Type: HandlerTypeHandler,
		Info: funcInfo,
	}
	payload := `{"name":"john","email":"john@example.com","age":18}`

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		request, _ := newBenchmarkRequest(s, http.MethodPost, "http://127.0.0.1/bench/strict-handler", payload)
		request.Header.Set("Content-Type", "application/json")
		request.serveHandler = &HandlerItemParsed{Handler: handlerItem}
		request.handlers = []*HandlerItemParsed{request.serveHandler}
		funcInfo.Func(request)
		if request.error != nil {
			b.Fatal(request.error)
		}
		request.Response.Flush()
		closeBenchmarkRequest(b, request)
	}
}

func BenchmarkInternal_ResponseWriteBuffer(b *testing.B) {
	s := newBenchmarkServer("bench-response")
	payload := bytes.Repeat([]byte("payload-"), 1024)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		request, _ := newBenchmarkRequest(s, http.MethodGet, "http://127.0.0.1/bench/response", "")
		request.Response.Write(payload)
		request.Response.WriteHeader(http.StatusOK)
		request.Response.Flush()
		closeBenchmarkRequest(b, request)
	}
}
