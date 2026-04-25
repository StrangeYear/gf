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
		serveCache:       gcache.New(),
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

	b.Run("search_dynamic", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			handlers, serveItem, hasHook, hasServe := s.searchHandlers(http.MethodGet, fmt.Sprintf("/api/user/%d/profile", i), "")
			if len(handlers) == 0 || serveItem == nil || hasHook || !hasServe {
				b.Fatal("route search failed")
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
