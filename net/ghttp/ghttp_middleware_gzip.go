// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package ghttp

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

var gzipCompressBufferPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(nil)
	},
}

var gzipWriterPool = sync.Pool{
	New: func() any {
		return gzip.NewWriter(io.Discard)
	},
}

const maxPooledGzipBufferCapacity = 256 * 1024

// MiddlewareGzip is a middleware that compresses HTTP response using gzip compression.
// Note that it does not compress responses if:
// 1. The response is already compressed (Content-Encoding header is set)
// 2. The client does not accept gzip compression
// 3. The response body length is too small (less than 1KB)
//
// To disable compression for specific routes, you can use the group middleware:
//
//	group.Group("/api", func(group *ghttp.RouterGroup) {
//	    group.Middleware(ghttp.MiddlewareGzip) // Enable GZIP for /api routes
//	})
func MiddlewareGzip(r *Request) {
	// Skip compression if client doesn't accept gzip
	if !acceptsGzip(r.Request) {
		r.Middleware.Next()
		return
	}

	// Execute the next handlers first
	r.Middleware.Next()

	// Skip if already compressed or empty response
	if r.Response.Header().Get("Content-Encoding") != "" {
		return
	}

	// Get the response buffer and check its length
	buffer := r.Response.Buffer()
	if len(buffer) < 1024 {
		return
	}

	// Try to compress the response
	var (
		compressed = gzipCompressBufferPool.Get().(*bytes.Buffer)
		gzipWriter = gzipWriterPool.Get().(*gzip.Writer)
		logger     = r.Server.Logger()
		ctx        = r.Context()
	)
	compressed.Reset()
	gzipWriter.Reset(compressed)
	defer func() {
		gzipWriter.Reset(io.Discard)
		gzipWriterPool.Put(gzipWriter)
		putGzipCompressBuffer(compressed)
	}()
	if _, err := gzipWriter.Write(buffer); err != nil {
		logger.Warningf(ctx, "gzip compression failed: %+v", err)
		return
	}
	if err := gzipWriter.Close(); err != nil {
		logger.Warningf(ctx, "gzip writer close failed: %+v", err)
		return
	}

	// Clear the original buffer and set headers
	r.Response.ClearBuffer()
	r.Response.Header().Set("Content-Encoding", "gzip")
	r.Response.Header().Del("Content-Length")

	// Write the compressed data
	r.Response.Write(compressed.Bytes())
}

func putGzipCompressBuffer(buffer *bytes.Buffer) bool {
	if buffer.Cap() > maxPooledGzipBufferCapacity {
		return false
	}
	buffer.Reset()
	gzipCompressBufferPool.Put(buffer)
	return true
}

// acceptsGzip returns true if the client accepts gzip compression.
func acceptsGzip(r *http.Request) bool {
	for _, headerValue := range r.Header.Values("Accept-Encoding") {
		for len(headerValue) > 0 {
			var encoding string
			encoding, headerValue, _ = strings.Cut(headerValue, ",")
			if acceptsGzipEncoding(encoding) {
				return true
			}
		}
	}
	return false
}

func acceptsGzipEncoding(encoding string) bool {
	token, params, _ := strings.Cut(strings.TrimSpace(encoding), ";")
	if !strings.EqualFold(strings.TrimSpace(token), "gzip") {
		return false
	}
	for len(params) > 0 {
		var param string
		param, params, _ = strings.Cut(params, ";")
		key, value, found := strings.Cut(strings.TrimSpace(param), "=")
		if !found || !strings.EqualFold(strings.TrimSpace(key), "q") {
			continue
		}
		q, err := strconv.ParseFloat(strings.Trim(strings.TrimSpace(value), `"`), 64)
		if err == nil && q <= 0 {
			return false
		}
	}
	return true
}
