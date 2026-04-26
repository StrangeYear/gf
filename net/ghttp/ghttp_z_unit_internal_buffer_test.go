package ghttp

import (
	"bytes"
	"net/http"
	"testing"
)

func TestInternal_GzipCompressBufferCapacityLimit(t *testing.T) {
	smallBuffer := bytes.NewBuffer(make([]byte, 0, maxPooledGzipBufferCapacity))
	if !putGzipCompressBuffer(smallBuffer) {
		t.Fatal("small gzip buffer should be pooled")
	}

	largeBuffer := bytes.NewBuffer(make([]byte, 0, maxPooledGzipBufferCapacity+1))
	if putGzipCompressBuffer(largeBuffer) {
		t.Fatal("large gzip buffer should not be pooled")
	}
}

func TestInternal_AcceptsGzip(t *testing.T) {
	testCases := []struct {
		name   string
		header string
		want   bool
	}{
		{name: "empty", header: "", want: false},
		{name: "exact", header: "gzip", want: true},
		{name: "with quality", header: "br, gzip;q=0.8", want: true},
		{name: "disabled", header: "gzip;q=0, br", want: false},
		{name: "token substring", header: "xgzip", want: false},
		{name: "case insensitive", header: "GZip", want: true},
	}
	for _, testCase := range testCases {
		request, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Accept-Encoding", testCase.header)
		if got := acceptsGzip(request); got != testCase.want {
			t.Fatalf("%s: got %v, want %v", testCase.name, got, testCase.want)
		}
	}
}
