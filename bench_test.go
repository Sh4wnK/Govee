package govee

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const timeMillisecond = time.Millisecond

// BenchmarkControl measures the steady-state latency of a single control
// call against a local fake API (no network). Run with:
//
//	go test -bench . -benchmem -run '^$'
//
// This is the number behind the README's per-call latency claim; the
// machine it was measured on is noted there too.
func BenchmarkControl(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"requestId":"bench","code":200,"msg":"success"}`)
	}))
	defer srv.Close()

	c := NewClient("bench-key", WithBaseURL(srv.URL), WithNoRateLimit(), WithRetryPolicy(1, timeMillisecond))
	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := c.SetPower(ctx, "H6601", "9D:FA:85:EB:D3:00:8B:FF", true); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkDeviceHandleControl measures the same path through the
// per-device handle (validation + per-device rate limiting disabled here;
// the token bucket in the dev handle would pace the benchmark to 2 req/s).
func BenchmarkDeviceHandleControl(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"requestId":"bench","code":200,"msg":"success"}`)
	}))
	defer srv.Close()

	dev := &Device{
		SKU: "H6601", ID: "9D:FA:85:EB:D3:00:8B:FF",
		Capabilities: []Capability{{
			Type: CapOnOff, Instance: InstancePowerSwitch,
			Parameters: []byte(`{"dataType":"ENUM","options":[{"name":"on","value":1},{"name":"off","value":0}]}`),
		}},
	}
	c := NewClient("bench-key", WithBaseURL(srv.URL), WithNoRateLimit(), WithRetryPolicy(1, timeMillisecond))
	h := c.Device(dev.SKU, dev.ID, dev)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := h.SetPower(context.Background(), true); err != nil {
			b.Fatal(err)
		}
	}
}
