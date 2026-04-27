package ghttp

import (
	"net/http"
	"testing"
)

func closeTestRequest(t *testing.T, r *Request) {
	t.Helper()
	if err := r.Session.Close(); err != nil {
		t.Fatal(err)
	}
	r.Response.BufferWriter.Close()
	_ = r.Body.Close()
	releaseRequest(r)
}

func TestInternal_RequestParseWithoutServeHandler(t *testing.T) {
	type requestStruct struct {
		Id   int    `json:"id" d:"9"`
		Name string `json:"name"`
	}

	s := newBenchmarkServer("test-request-parse-without-serve-handler")
	s.BindMiddleware("/*", func(r *Request) {})

	for _, tc := range []struct {
		name        string
		method      string
		target      string
		body        string
		contentType string
		parse       func(r *Request, req *requestStruct) error
	}{
		{
			name:   "Parse",
			method: http.MethodGet,
			target: "http://127.0.0.1/middleware-only?name=john",
			parse:  func(r *Request, req *requestStruct) error { return r.Parse(req) },
		},
		{
			name:   "GetRequestStruct",
			method: http.MethodGet,
			target: "http://127.0.0.1/middleware-only?name=john",
			parse:  func(r *Request, req *requestStruct) error { return r.GetRequestStruct(req) },
		},
		{
			name:   "ParseQuery",
			method: http.MethodGet,
			target: "http://127.0.0.1/middleware-only?name=john",
			parse:  func(r *Request, req *requestStruct) error { return r.ParseQuery(req) },
		},
		{
			name:   "GetQueryStruct",
			method: http.MethodGet,
			target: "http://127.0.0.1/middleware-only?name=john",
			parse:  func(r *Request, req *requestStruct) error { return r.GetQueryStruct(req) },
		},
		{
			name:        "ParseForm",
			method:      http.MethodPost,
			target:      "http://127.0.0.1/middleware-only",
			body:        "name=john",
			contentType: "application/x-www-form-urlencoded",
			parse:       func(r *Request, req *requestStruct) error { return r.ParseForm(req) },
		},
		{
			name:        "GetFormStruct",
			method:      http.MethodPost,
			target:      "http://127.0.0.1/middleware-only",
			body:        "name=john",
			contentType: "application/x-www-form-urlencoded",
			parse:       func(r *Request, req *requestStruct) error { return r.GetFormStruct(req) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newBenchmarkRequest(s, tc.method, tc.target, tc.body)
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			defer closeTestRequest(t, r)

			r.handlers, r.serveHandler, r.hasHookHandler, r.hasServeHandler = s.getHandlersWithCache(r)
			if len(r.handlers) == 0 {
				t.Fatal("expected matched middleware handlers")
			}
			if r.serveHandler != nil || r.hasServeHandler {
				t.Fatalf("expected middleware-only request without serve handler, got %#v", r.serveHandler)
			}

			var req requestStruct
			if err := tc.parse(r, &req); err != nil {
				t.Fatal(err)
			}
			if req.Id != 9 || req.Name != "john" {
				t.Fatalf("unexpected parsed request: %#v", req)
			}
		})
	}
}
