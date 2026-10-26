package govee

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// newTestServer returns an httptest server that records requests and
// serves the given handler. Recording is race-safe for concurrent calls.
func newTestServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *[]requestRecord) {
	t.Helper()
	var mu sync.Mutex
	var recs []requestRecord
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 0, 4096)
		if r.Body != nil {
			buf := make([]byte, 4096)
			n, _ := r.Body.Read(buf)
			b = buf[:n]
		}
		mu.Lock()
		recs = append(recs, requestRecord{method: r.Method, path: r.URL.Path, apiKey: r.Header.Get(apiKeyHeader), ct: r.Header.Get("Content-Type"), body: b})
		mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &recs
}

type requestRecord struct {
	method string
	path   string
	apiKey string
	ct     string
	body   []byte
}

func fixedClient(t *testing.T, srv *httptest.Server) *Client {
	return NewClient("test-key-123",
		WithBaseURL(srv.URL),
		WithNoRateLimit(),
		WithRetryPolicy(1, time.Millisecond),
	)
}

func TestDevicesParsesRealisticFixture(t *testing.T) {
	srv, recs := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != pathDevices {
			t.Errorf("path = %s, want %s", r.URL.Path, pathDevices)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(readFixture(t, "devices.json"))
	})
	c := fixedClient(t, srv)

	devices, err := c.Devices(context.Background())
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(devices) != 3 {
		t.Fatalf("got %d devices, want 3", len(devices))
	}

	strip := devices[0]
	if strip.SKU != "H6601" || strip.ID != "9D:FA:85:EB:D3:00:8B:FF" {
		t.Errorf("unexpected device %+v", strip)
	}
	if strip.Name != "Living Room Strip" || strip.Type != DeviceTypeLight {
		t.Errorf("unexpected name/type: %q %q", strip.Name, strip.Type)
	}
	if !strip.Supports(CapOnOff, InstancePowerSwitch) {
		t.Error("expected on_off/powerSwitch support")
	}
	if strip.Supports(CapWorkMode, InstanceWorkMode) {
		t.Error("light must not advertise work_mode")
	}
	if !strip.Supports(CapDynamicScene, InstanceLightScene) {
		t.Error("expected dynamic_scene/lightScene support")
	}

	r, ok := strip.Range(CapRange, InstanceBrightness)
	if !ok {
		t.Fatal("expected brightness range")
	}
	if r.Min != 1 || r.Max != 100 || r.Precision != 1 {
		t.Errorf("unexpected brightness range %+v", r)
	}

	opts, ok := strip.Options(CapDynamicScene, InstanceLightScene)
	if !ok || len(opts) != 19 {
		t.Fatalf("expected 19 scene options, got %d (ok=%v)", len(opts), ok)
	}
	if opts[0].Name != "Tudum" || opts[0].ValueString() != "3054" {
		t.Errorf("unexpected first scene option %+v", opts[0])
	}

	// Humidifier: work_mode + event capability.
	hum := devices[1]
	if !hum.Supports(CapWorkMode, InstanceWorkMode) {
		t.Error("expected work_mode support on humidifier")
	}
	cap, ok := hum.Capability(CapEvent, "lackWaterEvent")
	if !ok {
		t.Fatal("expected lackWaterEvent capability")
	}
	if cap.AlarmType != 51 {
		t.Errorf("alarmType = %d, want 51", cap.AlarmType)
	}
	if cap.EventState == nil || len(cap.EventState.Options) != 1 {
		t.Errorf("expected eventState options, got %+v", cap.EventState)
	}

	// Monitor device (property capabilities, no parameters).
	mon := devices[2]
	if !mon.Supports(CapProperty, "sensorTemperature") {
		t.Error("expected property support")
	}

	// Request shape.
	rec := (*recs)[0]
	if rec.method != "GET" {
		t.Errorf("method = %s, want GET", rec.method)
	}
	if rec.apiKey != "test-key-123" {
		t.Errorf("api key header = %q", rec.apiKey)
	}
	if rec.ct != "application/json" {
		t.Errorf("content type = %q", rec.ct)
	}
}

func TestControlRequestShape(t *testing.T) {
	srv, recs := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != pathControl {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"requestId":"echo","code":200,"msg":"success"}`))
	})
	c := fixedClient(t, srv)

	// Contract methods.
	if err := c.SetPower(context.Background(), "H6601", "9D:FA:85:EB:D3:00:8B:FF", true); err != nil {
		t.Fatalf("SetPower: %v", err)
	}
	if err := c.SetBrightness(context.Background(), "H6601", "9D:FA:85:EB:D3:00:8B:FF", 42); err != nil {
		t.Fatalf("SetBrightness: %v", err)
	}
	if err := c.SetColorRGB(context.Background(), "H6601", "9D:FA:85:EB:D3:00:8B:FF", Color{R: 255, G: 128, B: 0}); err != nil {
		t.Fatalf("SetColorRGB: %v", err)
	}

	if len(*recs) != 3 {
		t.Fatalf("got %d requests, want 3", len(*recs))
	}

	// --- power on ---
	var powerEnv struct {
		RequestID string `json:"requestId"`
		Payload   struct {
			SKU    string `json:"sku"`
			Device string `json:"device"`
			Cap    struct {
				Type     string `json:"type"`
				Instance string `json:"instance"`
				Value    int    `json:"value"`
			} `json:"capability"`
		} `json:"payload"`
	}
	if err := json.Unmarshal((*recs)[0].body, &powerEnv); err != nil {
		t.Fatalf("decode power request: %v", err)
	}
	if powerEnv.RequestID == "" {
		t.Error("requestId is empty")
	}
	if powerEnv.Payload.SKU != "H6601" || powerEnv.Payload.Device != "9D:FA:85:EB:D3:00:8B:FF" {
		t.Errorf("unexpected payload %+v", powerEnv.Payload)
	}
	if powerEnv.Payload.Cap.Type != CapOnOff || powerEnv.Payload.Cap.Instance != "powerSwitch" || powerEnv.Payload.Cap.Value != 1 {
		t.Errorf("unexpected capability %+v", powerEnv.Payload.Cap)
	}

	// --- brightness ---
	var bEnv struct {
		Payload struct {
			Cap struct {
				Type     string `json:"type"`
				Instance string `json:"instance"`
				Value    int    `json:"value"`
			} `json:"capability"`
		} `json:"payload"`
	}
	if err := json.Unmarshal((*recs)[1].body, &bEnv); err != nil {
		t.Fatalf("decode brightness request: %v", err)
	}
	if bEnv.Payload.Cap.Type != CapRange || bEnv.Payload.Cap.Instance != "brightness" || bEnv.Payload.Cap.Value != 42 {
		t.Errorf("unexpected brightness capability %+v", bEnv.Payload.Cap)
	}

	// --- color ---
	var cEnv struct {
		Payload struct {
			Cap struct {
				Type     string `json:"type"`
				Instance string `json:"instance"`
				Value    int    `json:"value"`
			} `json:"capability"`
		} `json:"payload"`
	}
	if err := json.Unmarshal((*recs)[2].body, &cEnv); err != nil {
		t.Fatalf("decode color request: %v", err)
	}
	if cEnv.Payload.Cap.Type != CapColorSetting || cEnv.Payload.Cap.Instance != "colorRgb" {
		t.Errorf("unexpected color capability %+v", cEnv.Payload.Cap)
	}
	if want := 0xFF8000; cEnv.Payload.Cap.Value != want {
		t.Errorf("color value = %#x, want %#x", cEnv.Payload.Cap.Value, want)
	}
}

func TestStateParsing(t *testing.T) {
	srv, recs := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != pathState {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(readFixture(t, "state.json"))
	})
	c := fixedClient(t, srv)

	st, err := c.State(context.Background(), "H7143", "52:8B:D4:AD:FC:45:5D:FE")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if st.SKU != "H7143" || st.Device != "52:8B:D4:AD:FC:45:5D:FE" {
		t.Errorf("unexpected state identity %+v", st)
	}
	on, ok := st.Bool(InstanceOnline)
	if !ok || !on {
		t.Errorf("online = %v (%v), want true", on, ok)
	}
	pow, ok := st.Bool(InstancePowerSwitch)
	if !ok || pow {
		t.Errorf("power = %v (%v), want false", pow, ok)
	}
	b, ok := st.Number(InstanceBrightness)
	if !ok || b != 5 {
		t.Errorf("brightness = %v (%v), want 5", b, ok)
	}
	// The humidity capability reports an empty value => unavailable.
	if _, ok := st.Number("humidity"); ok {
		t.Error("humidity should be unset (empty string value)")
	}
	// workMode is a struct value.
	v, ok := st.StateValue(InstanceWorkMode)
	if !ok {
		t.Fatal("workMode state missing")
	}
	_, isMap := v.(map[string]any)
	if !isMap {
		t.Errorf("workMode value = %T, want map", v)
	}

	// Request body shape: requestId + sku/device payload.
	var env struct {
		RequestID string `json:"requestId"`
		Payload   struct {
			SKU    string `json:"sku"`
			Device string `json:"device"`
		} `json:"payload"`
	}
	if err := json.Unmarshal((*recs)[0].body, &env); err != nil {
		t.Fatalf("decode state request: %v", err)
	}
	if env.RequestID == "" || env.Payload.SKU != "H7143" || env.Payload.Device != "52:8B:D4:AD:FC:45:5D:FE" {
		t.Errorf("unexpected state request %+v", env)
	}
}

func TestStateSensorParsing(t *testing.T) {
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(readFixture(t, "state_sensor.json"))
	})
	c := fixedClient(t, srv)
	st, err := c.State(context.Background(), "H5140", "FD:C8:DC:1E:D5:51:65:24")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	co2, ok := st.Number("carbonDioxideConcentration")
	if !ok || co2 != 1590 {
		t.Errorf("co2 = %v (%v), want 1590", co2, ok)
	}
	temp, ok := st.Number("sensorTemperature")
	if !ok || temp != 79.52 {
		t.Errorf("temp = %v (%v), want 79.52", temp, ok)
	}
	if on, ok := st.Online(); !ok || !on {
		t.Errorf("online = %v (%v), want true", on, ok)
	}
}

func TestScenesParsing(t *testing.T) {
	srv, recs := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathScenes {
			t.Errorf("path = %s, want %s", r.URL.Path, pathScenes)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(readFixture(t, "scenes.json"))
	})
	c := fixedClient(t, srv)
	scenes, err := c.ListScenes(context.Background(), "H6057", "2F:56:C1:9A:7C:E8:5B:19")
	if err != nil {
		t.Fatalf("ListScenes: %v", err)
	}
	if len(scenes) != 12 {
		t.Fatalf("got %d scenes, want 12", len(scenes))
	}
	if scenes[0].Name != "Sunrise" || scenes[0].ID != 3853 || scenes[0].ParamID != 4280 {
		t.Errorf("unexpected first scene %+v", scenes[0])
	}
	if scenes[11].Name != "Gradient" || scenes[11].ID != 3883 {
		t.Errorf("unexpected last scene %+v", scenes[11])
	}
	// Ensure it was a POST with the right body shape.
	var env struct {
		Payload struct {
			SKU    string `json:"sku"`
			Device string `json:"device"`
		} `json:"payload"`
	}
	if err := json.Unmarshal((*recs)[0].body, &env); err != nil {
		t.Fatalf("decode scenes request: %v", err)
	}
	if env.Payload.SKU != "H6057" {
		t.Errorf("scenes request sku = %q", env.Payload.SKU)
	}
}

func TestControlSuccessResponse(t *testing.T) {
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"requestId":"abc","code":200,"msg":"success"}`))
	})
	c := fixedClient(t, srv)
	if err := c.SetPower(context.Background(), "H6601", "9D:FA:85:EB:D3:00:8B:FF", false); err != nil {
		t.Fatalf("SetPower: %v", err)
	}
}

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantSent   error
		wantStatus int
		wantCode   int
	}{
		{
			name:     "404 device not found",
			status:   http.StatusNotFound,
			body:     `{"code":404,"message":"device not found","requestId":"r1"}`,
			wantSent: ErrDeviceNotFound, wantStatus: 404,
		},
		{
			name:     "429 rate limited",
			status:   http.StatusTooManyRequests,
			body:     `{"code":429,"message":"too many request","requestId":"r2"}`,
			wantSent: ErrRateLimited, wantStatus: 429,
		},
		{
			name:     "401 unauthorized",
			status:   http.StatusUnauthorized,
			body:     `{"code":401,"message":"Invalid API key","requestId":"r3"}`,
			wantSent: ErrUnauthorized, wantStatus: 401,
		},
		{
			name:       "405 method not allowed",
			status:     http.StatusMethodNotAllowed,
			body:       `{"code":405,"message":"method not allowed"}`,
			wantStatus: 405,
		},
		{
			name:     "API error inside 200 envelope",
			status:   http.StatusOK,
			body:     `{"requestId":"r4","code":400,"msg":"Parameter value out of range"}`,
			wantCode: 400, wantStatus: 200,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			})
			c := fixedClient(t, srv)
			err := c.SetPower(context.Background(), "H6601", "9D:FA:85:EB:D3:00:8B:FF", true)
			if err == nil {
				t.Fatal("expected error")
			}
			if tt.wantSent != nil && !errors.Is(err, tt.wantSent) {
				t.Errorf("errors.Is(%v, %v) = false", err, tt.wantSent)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error is %T, want *APIError", err)
			}
			if tt.wantStatus != 0 && apiErr.Status != tt.wantStatus {
				t.Errorf("Status = %d, want %d", apiErr.Status, tt.wantStatus)
			}
			if tt.wantCode != 0 && apiErr.Code != tt.wantCode {
				t.Errorf("Code = %d, want %d", apiErr.Code, tt.wantCode)
			}
			if tt.name == "404 device not found" && apiErr.Endpoint != pathControl {
				t.Errorf("Endpoint = %q, want %q", apiErr.Endpoint, pathControl)
			}
		})
	}
}

func TestRetryOn429HonorsRetryAfter(t *testing.T) {
	attempts := 0
	start := time.Now()
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"code":429,"message":"too many request"}`))
			return
		}
		w.Write([]byte(`{"requestId":"x","code":200,"msg":"success"}`))
	})
	c := NewClient("k", WithBaseURL(srv.URL), WithNoRateLimit(), WithRetryPolicy(3, 50*time.Millisecond))
	if err := c.SetPower(context.Background(), "H6601", "9D:FA:85:EB:D3:00:8B:FF", true); err != nil {
		t.Fatalf("SetPower: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if elapsed := time.Since(start); elapsed < 2*time.Second {
		t.Errorf("retries returned too fast (%v); Retry-After=1s honored twice", elapsed)
	}
}

func TestRetryOn5xxWithBackoff(t *testing.T) {
	attempts := 0
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"code":503,"message":"temporary"}`))
			return
		}
		w.Write([]byte(`{"requestId":"x","code":200,"msg":"success"}`))
	})
	c := NewClient("k", WithBaseURL(srv.URL), WithNoRateLimit(), WithRetryPolicy(4, time.Millisecond))
	if _, err := c.State(context.Background(), "H6601", "9D:FA:85:EB:D3:00:8B:FF"); err != nil {
		t.Fatalf("State: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestRetryExhaustedReturnsError(t *testing.T) {
	attempts := 0
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"code":429,"message":"still limited"}`))
	})
	c := NewClient("k", WithBaseURL(srv.URL), WithNoRateLimit(), WithRetryPolicy(2, time.Millisecond))
	err := c.SetPower(context.Background(), "H6601", "9D:FA:85:EB:D3:00:8B:FF", true)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("errors.Is(err, ErrRateLimited) = false, err = %v", err)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2 (maxAttempts)", attempts)
	}
}

func TestRetryRespectsContextCancel(t *testing.T) {
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{}`))
	})
	c := NewClient("k", WithBaseURL(srv.URL), WithNoRateLimit(), WithRetryPolicy(5, time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := c.SetPower(ctx, "H6601", "9D:FA:85:EB:D3:00:8B:FF", true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestEmptyAPIKeyReturnsUnauthorized(t *testing.T) {
	c := NewClient("", WithNoRateLimit())
	err := c.SetPower(context.Background(), "H6601", "x", true)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestErrValidationOnBadAPIError(t *testing.T) {
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html>gateway hiccup</html>`))
	})
	c := fixedClient(t, srv)
	_, err := c.State(context.Background(), "H6601", "x")
	if err == nil {
		t.Fatal("expected error for non-JSON success body")
	}
	if strings.Contains(err.Error(), "invalid JSON") == false {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConcurrentCallsAreSafe(t *testing.T) {
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"requestId":"x","code":200,"msg":"success"}`))
	})
	c := fixedClient(t, srv)
	const n = 50
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			errs <- c.SetBrightness(context.Background(), "H6601", "9D:FA:85:EB:D3:00:8B:FF", 50)
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent call: %v", err)
		}
	}
}
