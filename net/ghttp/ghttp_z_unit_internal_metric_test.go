package ghttp

import (
	"context"
	"net/http"
	"testing"

	"github.com/gogf/gf/v2/os/gmetric"
)

type testMetricProvider struct{}

func (p testMetricProvider) SetAsGlobal() {}

func (p testMetricProvider) MeterPerformer(gmetric.MeterOption) gmetric.MeterPerformer {
	return nil
}

func (p testMetricProvider) ForceFlush(context.Context) error {
	return nil
}

func (p testMetricProvider) Shutdown(context.Context) error {
	return nil
}

func TestInternal_HandleAfterRequestDone_WithMetricsAndPooledResponse(t *testing.T) {
	gmetric.SetGlobalProvider(testMetricProvider{})
	defer gmetric.SetGlobalProvider(nil)

	s := newBenchmarkServer("test-handle-after-request-done-with-metrics")
	request, _ := newBenchmarkRequest(s, http.MethodGet, "http://127.0.0.1/metrics", "")
	request.Response.Write("ok")
	request.Response.Flush()

	defer func() {
		if exception := recover(); exception != nil {
			t.Fatalf("handleAfterRequestDone should not panic, got: %+v", exception)
		}
	}()

	s.handleAfterRequestDone(request)
}
