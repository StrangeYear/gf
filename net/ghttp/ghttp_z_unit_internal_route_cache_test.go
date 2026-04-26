package ghttp

import (
	"context"
	"net/http"
	"testing"
)

func TestInternal_RouteCacheSkipsDynamicServePaths(t *testing.T) {
	handler := func(r *Request) {
		r.Response.Write("ok")
	}
	s := newBenchmarkServer("test-route-cache-dynamic")
	s.BindHandler("/user/:id", handler)
	s.BindHandler("/static", handler)

	dynamicRequest, _ := newBenchmarkRequest(s, http.MethodGet, "http://127.0.0.1/user/1", "")
	s.getHandlersWithCache(dynamicRequest)
	dynamicKey := s.serveHandlerKey(http.MethodGet, "/user/1", "")
	dynamicCache, err := s.serveCache.Get(context.TODO(), dynamicKey)
	if err != nil {
		t.Fatal(err)
	}
	if dynamicCache != nil {
		t.Fatal("dynamic serving path should not be cached by concrete request path")
	}
	closeRouteCacheTestRequest(t, dynamicRequest)

	staticRequest, _ := newBenchmarkRequest(s, http.MethodGet, "http://127.0.0.1/static", "")
	s.getHandlersWithCache(staticRequest)
	staticKey := s.serveHandlerKey(http.MethodGet, "/static", "")
	staticCache, err := s.serveCache.Get(context.TODO(), staticKey)
	if err != nil {
		t.Fatal(err)
	}
	if staticCache == nil {
		t.Fatal("static serving path should be cached")
	}
	closeRouteCacheTestRequest(t, staticRequest)
}

func closeRouteCacheTestRequest(t *testing.T, r *Request) {
	t.Helper()
	if err := r.Session.Close(); err != nil {
		t.Fatal(err)
	}
	r.Response.BufferWriter.Close()
	_ = r.Body.Close()
	releaseRequest(r)
}
