package govee

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

const timeMs = time.Millisecond

// requestID extracts the requestId from a Govee request body. Every retry of
// a single client call reuses the same requestId, so the server can track
// "attempts made for this logical call".
func requestID(t *testing.T, r *http.Request) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var env struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatalf("decode requestId: %v", err)
	}
	return env.RequestID
}

// newFaultServer injects failures deterministically: every requests fails
// its first `failFirst` attempts (alternating 429 and 503, with Retry-After
// on some 429s), then succeeds. The client's retry loop must absorb them.
// It also counts requests/failures for assertions.
func newFaultServer(t *testing.T, failFirst int, requests, failures *int) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	remaining := map[string]int{} // requestId -> failures left
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := requestID(t, r)
		mu.Lock()
		*requests++
		if !seen[id] {
			seen[id] = true
			if failFirst > 0 {
				remaining[id] = failFirst
			}
		}
		left := remaining[id]
		if left > 0 {
			remaining[id] = left - 1
			*failures++
			// Alternate failure modes across attempts to exercise
			// the whole retry matrix.
			attempt := failFirst - left + 1
			mu.Unlock()
			switch attempt % 3 {
			case 0:
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprint(w, `{"code":429,"message":"too many request"}`)
			case 1:
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `{"code":503,"message":"temporary unavailable"}`)
			default:
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"code":500,"message":"internal error"}`)
			}
			return
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"requestId":"`+id+`","code":200,"msg":"success"}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSoak500Calls fires 600 concurrent calls through the client against a
// fault-injecting server in which every call fails its first three attempts
// (429/503/500). All 600 must succeed after retries. This is the reproducible
// evidence for the "low error rate under sustained use (500+ calls/day)"
// target; run with `go test -run TestSoak500Calls -v`.
func TestSoak500Calls(t *testing.T) {
	if testing.Short() {
		t.Skip("soak test skipped in -short mode")
	}
	var requests, failures int
	srv := newFaultServer(t, 3, &requests, &failures)
	c := NewClient("soak-key",
		WithBaseURL(srv.URL),
		WithNoRateLimit(),
		WithRetryPolicy(6, 5*timeMs),
	)

	const calls = 600
	errs := make(chan error, calls)
	for i := 0; i < calls; i++ {
		go func() {
			errs <- c.SetBrightness(context.Background(), "H6601", "9D:FA:85:EB:D3:00:8B:FF", 42)
		}()
	}
	failed := 0
	for i := 0; i < calls; i++ {
		if err := <-errs; err != nil {
			failed++
			t.Logf("call failed: %v", err)
		}
	}
	if failed != 0 {
		t.Fatalf("%d of %d calls failed after retries (server saw %d requests, %d injected failures)",
			failed, calls, requests, failures)
	}
	// Every call burned exactly its 3 injected failures; success came on
	// attempt 4, so the server must have seen 4 x calls requests.
	if want := calls * 4; requests != want {
		t.Errorf("server saw %d requests, want %d (3 injected failures + 1 success per call)", requests, want)
	}
	if failures != calls*3 {
		t.Errorf("injected %d failures, want %d", failures, calls*3)
	}
	t.Logf("soak: %d/600 calls succeeded; %d requests, %d injected failures", calls-failed, requests, failures)
}
