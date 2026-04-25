package ghttp

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/net/goai"
	"github.com/gogf/gf/v2/net/gsvc"
	"github.com/gogf/gf/v2/os/gcache"
	"github.com/gogf/gf/v2/os/gsession"
)

func newBenchmarkServer(name string) *Server {
	s := &Server{
		instance:         name,
		plugins:          make([]Plugin, 0),
		servers:          nil,
		closeChan:        make(chan struct{}, 16),
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

func BenchmarkInternal_NewRequest(b *testing.B) {
	s := newBenchmarkServer("bench-request")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, _ := newBenchmarkRequest(s, http.MethodGet, "http://127.0.0.1/user/profile?id=1", "")
		_ = r.Body.Close()
		if err := r.Session.Close(); err != nil {
			b.Fatal(err)
		}
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
}

func TestInternal_RouteFastPath_MixedFallback(t *testing.T) {
	handler := func(r *Request) {
		r.Response.Write("ok")
	}
	s := newBenchmarkServer("test-router-fast")
	s.BindHandler("/api/user/:id/profile", handler)
	s.BindHandler("/api/object/{id}", handler)
	s.BindHandler("/api/files/*path", handler)
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
	if handlers != nil || serveItem != nil || hasHook || hasServe {
		t.Fatal("complex route should fall back to compatibility matcher")
	}
	handlers, serveItem, hasHook, hasServe = s.searchHandlers(http.MethodGet, "/data.json", "")
	if len(handlers) == 0 || serveItem == nil || hasHook || !hasServe {
		t.Fatal("compatibility matcher should still serve complex route")
	}
	if serveItem.Values["hash"] != "data" || serveItem.Values["type"] != "json" {
		t.Fatalf("unexpected complex route params: %#v", serveItem.Values)
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
		_ = request.Body.Close()
		if err := request.Session.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInternal_StrictBind(b *testing.B) {
	type benchReq struct {
		Name  string `json:"name" v:"required"`
		Email string `json:"email" v:"required|email"`
		Age   int    `json:"age" v:"min:1|max:200"`
	}

	s := newBenchmarkServer("bench-bind")
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
		_ = request.Body.Close()
		if err := request.Session.Close(); err != nil {
			b.Fatal(err)
		}
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
		_ = request.Body.Close()
		if err := request.Session.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
