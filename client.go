package govee

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Default base URL of the Govee Developer Platform OpenAPI.
const DefaultBaseURL = "https://openapi.api.govee.com"

// Default timeout for individual HTTP requests.
const DefaultTimeout = 30 * time.Second

// API paths, as documented at https://developer.govee.com/reference.
const (
	pathDevices   = "/router/api/v1/user/devices"
	pathState     = "/router/api/v1/device/state"
	pathControl   = "/router/api/v1/device/control"
	pathScenes    = "/router/api/v1/device/scenes"
	pathDIYScenes = "/router/api/v1/device/diy-scenes"
)

// apiKeyHeader is the request header carrying the account API key.
const apiKeyHeader = "Govee-API-Key"

// Capability type constants, as documented under
// "capabilities" at https://developer.govee.com/reference/get-you-devices.
const (
	CapOnOff          = "devices.capabilities.on_off"
	CapToggle         = "devices.capabilities.toggle"
	CapRange          = "devices.capabilities.range"
	CapMode           = "devices.capabilities.mode"
	CapColorSetting   = "devices.capabilities.color_setting"
	CapSegmentColor   = "devices.capabilities.segment_color_setting"
	CapMusicSetting   = "devices.capabilities.music_setting"
	CapDynamicScene   = "devices.capabilities.dynamic_scene"
	CapWorkMode       = "devices.capabilities.work_mode"
	CapTemperatureSet = "devices.capabilities.temperature_setting"
	CapOnline         = "devices.capabilities.online"
	CapProperty       = "devices.capabilities.property"
	CapEvent          = "devices.capabilities.event"
)

// Device type constants returned in Device.Type. The list covers every
// device family the official API documents; Govee adds more over time, so
// treat unknown values as opaque strings.
const (
	DeviceTypeLight             = "devices.types.light"
	DeviceTypeAirPurifier       = "devices.types.air_purifier"
	DeviceTypeThermometer       = "devices.types.thermometer"
	DeviceTypeSocket            = "devices.types.socket"
	DeviceTypeSensor            = "devices.types.sensor"
	DeviceTypeHeater            = "devices.types.heater"
	DeviceTypeHumidifier        = "devices.types.humidifier"
	DeviceTypeDehumidifier      = "devices.types.dehumidifier"
	DeviceTypeIceMaker          = "devices.types.ice_maker"
	DeviceTypeAromaDiffuser     = "devices.types.aroma_diffuser"
	DeviceTypeBox               = "devices.types.box"
	DeviceTypeFan               = "devices.types.fan"
	DeviceTypeKettle            = "devices.types.kettle"
	DeviceTypeFrozenTreatMaker  = "devices.types.frozen_treat_maker"
	DeviceTypeAirQualityMonitor = "devices.types.air_quality_monitor"
	DeviceTypeKitchenComposter  = "devices.types.kitchen_composter"
	DeviceTypeTimer             = "devices.types.timer"
)

// Standard instance names for the most common capabilities.
const (
	InstancePowerSwitch         = "powerSwitch"
	InstanceBrightness          = "brightness"
	InstanceHumidity            = "humidity"
	InstanceColorRgb            = "colorRgb"
	InstanceColorTemperatureK   = "colorTemperatureK"
	InstanceSegmentedColorRgb   = "segmentedColorRgb"
	InstanceSegmentedBrightness = "segmentedBrightness"
	InstanceWorkMode            = "workMode"
	InstanceTargetTemperature   = "targetTemperature"
	InstanceSliderTemperature   = "sliderTemperature"
	InstanceLightScene          = "lightScene"
	InstanceDIYScene            = "diyScene"
	InstanceOnline              = "online"
	InstanceVolume              = "volume"
	InstanceTemperature         = "temperature"
)

// Option configures a Client. All options are applied in order; invalid
// values are ignored or clamped rather than panicking.
type Option func(*Client)

// WithHTTPClient replaces the default http.Client. Callers that supply their
// own client take responsibility for its Transport; the default client uses a
// connection-reusing Transport with sane timeouts.
func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) {
		if c != nil {
			cl.http = c
		}
	}
}

// WithBaseURL overrides the API base URL. Useful for the Govee test
// environment (https://test-openapi.api.govee.com) or local testing.
func WithBaseURL(base string) Option {
	return func(cl *Client) { cl.baseURL = strings.TrimRight(base, "/") }
}

// WithTimeout sets the per-request HTTP timeout.
func WithTimeout(d time.Duration) Option {
	return func(cl *Client) {
		if d > 0 && cl.http != nil {
			cl.http.Timeout = d
		}
	}
}

// WithRateLimit overrides the client-side token bucket. allow is the number
// of requests permitted per window of time. See Govee's published limits:
//
//	GET  /router/api/v1/user/devices:    30 req/min/account
//	POST /router/api/v1/device/control:   12 req/s/account (720/min), burst 80
//	POST /router/api/v1/device/control:   2 req/s/device  (120/min), burst 6
//	POST /router/api/v1/device/state:     30 req/min/device
//	POST /router/api/v1/device/scenes:    30 req/min/device
func WithRateLimit(allow int, window time.Duration) Option {
	return func(cl *Client) {
		if allow > 0 && window > 0 {
			cl.limiter = newTokenBucket(allow, window)
		}
	}
}

// WithNoRateLimit disables client-side rate limiting. Only use when your
// application enforces Govee's published limits itself.
func WithNoRateLimit() Option {
	return func(cl *Client) { cl.limiter = nil }
}

// WithRetryPolicy overrides retry behavior for transient failures (HTTP 429
// and 5xx). maxAttempts counts the first attempt (must be >= 1); baseDelay is
// the initial backoff, doubled on each retry with jitter added.
func WithRetryPolicy(maxAttempts int, baseDelay time.Duration) Option {
	return func(cl *Client) {
		if maxAttempts >= 1 {
			cl.maxAttempts = maxAttempts
		}
		if baseDelay > 0 {
			cl.baseDelay = baseDelay
		}
	}
}

// Client is a Govee OpenAPI client. It is safe for concurrent use by
// multiple goroutines: both the underlying http.Client and the token bucket
// are concurrency-safe.
type Client struct {
	apiKey string

	baseURL string
	http    *http.Client

	maxAttempts int
	baseDelay   time.Duration

	limiter *tokenBucket
}

// NewClient returns a Client for the given API key. It performs no network
// I/O and never panics; an empty or malformed key surfaces as an API error
// from the first request.
func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:  apiKey,
		baseURL: DefaultBaseURL,
		http:    &http.Client{Timeout: DefaultTimeout},
		// Account-level control limit: 720 requests/minute sustained
		// (12/s), burst 80; per-device limits are additionally enforced
		// by the typed Device helpers via Device.rateLimit.
		maxAttempts: 5,
		baseDelay:   200 * time.Millisecond,
	}
	c.http.Transport = &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     90 * time.Second,
	}
	c.limiter = newTokenBucket(720, time.Minute)
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

// WithAPIKey returns a client sharing this client's configuration and HTTP
// machinery but bound to a different API key. The receiver is not mutated.
func (c *Client) WithAPIKey(apiKey string) *Client {
	nc := &Client{
		apiKey:      apiKey,
		baseURL:     c.baseURL,
		http:        c.http,
		maxAttempts: c.maxAttempts,
		baseDelay:   c.baseDelay,
		limiter:     c.limiter, // tokenBucket is concurrency-safe
	}
	return nc
}

// doRoundTrip performs a single HTTP request, applying the client-level
// token bucket, and decodes the response into out (which may be nil). It
// returns a *APIError for non-2xx responses or API-level failures.
func (c *Client) doRoundTrip(ctx context.Context, method, path string, body any, out any) (retryable bool, apiErr *APIError, err error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return false, nil, fmt.Errorf("%w: marshal request body: %v", ErrInvalidRequest, err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return false, nil, fmt.Errorf("%w: build request: %v", ErrInvalidRequest, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set(apiKeyHeader, c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return true, nil, fmt.Errorf("%w: HTTP request failed: %v", ErrInvalidRequest, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, &APIError{Status: resp.StatusCode, Message: "read response body: " + err.Error()}, err
	}

	ra := retryAfterSeconds(resp.Header.Get("Retry-After"))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		e := apiErrorFromBody(resp.StatusCode, ra, b)
		return e.Status == 429 || e.Status >= 500, e, e
	}
	if out == nil {
		return false, nil, nil
	}
	// The API returns HTTP 200 even for rejected requests; the envelope
	// code field is authoritative.
	var env struct {
		Code    int             `json:"code"`
		Msg     string          `json:"msg"`
		Message string          `json:"message"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return false, nil, fmt.Errorf("%w: invalid JSON response: %v", ErrInvalidRequest, err)
	}
	if env.Code != 0 && env.Code != 200 {
		e := &APIError{Status: resp.StatusCode, Code: env.Code, Message: firstNonEmpty(env.Msg, env.Message), RetryAfter: ra}
		return env.Code == 429 || env.Code >= 500, e, e
	}
	if err := json.Unmarshal(b, out); err != nil {
		return false, nil, fmt.Errorf("%w: decode response body: %v", ErrInvalidRequest, err)
	}
	return false, nil, nil
}

// do performs a request with retries: transient failures (HTTP 429 and 5xx,
// or network errors) are retried up to maxAttempts times with exponential
// backoff, jitter, and optional Retry-After honoring.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	if c.apiKey == "" {
		return fmt.Errorf("%w: API key is empty", ErrUnauthorized)
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			return fmt.Errorf("%w: waiting for rate-limit slot: %v", ErrRateLimited, err)
		}
	}
	delay := c.baseDelay
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		retryable, apiErr, err := c.doRoundTrip(ctx, method, path, body, out)
		if err == nil {
			return nil
		}
		if apiErr != nil {
			apiErr.Endpoint = path
		}
		if !retryable || attempt == c.maxAttempts {
			if err != nil {
				return err
			}
			return apiErr
		}
		// Back off before the next attempt, honoring Retry-After when
		// the server provided it; jitter (±baseDelay/2) spreads bursts.
		wait := delay
		if apiErr != nil && apiErr.RetryAfter > 0 {
			wait = time.Duration(apiErr.RetryAfter) * time.Second
		}
		wait += jitterDuration(c.baseDelay / 2)
		if sle := sleepCtx(ctx, wait); sle != nil {
			return sle
		}
		delay = nextDelay(delay, c.baseDelay)
	}
	return nil
}

// retryAfterSeconds parses an HTTP Retry-After header value (seconds or an
// HTTP date) into seconds; returns -1 when absent.
func retryAfterSeconds(v string) int {
	if v == "" {
		return -1
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return secs
	}
	if t, err := http.ParseTime(v); err == nil {
		s := int(time.Until(t).Seconds())
		if s < 0 {
			s = 0
		}
		return s
	}
	return -1
}

func apiErrorFromBody(status, retryAfter int, b []byte) *APIError {
	var eb struct {
		Code      int    `json:"code"`
		Message   string `json:"message"`
		Msg       string `json:"msg"`
		RequestID string `json:"requestId"`
	}
	_ = json.Unmarshal(b, &eb)
	msg := firstNonEmpty(eb.Message, eb.Msg, strings.TrimSpace(string(b)))
	return &APIError{Status: status, Code: eb.Code, Message: msg, RequestID: eb.RequestID, RetryAfter: retryAfter}
}

// maxBackoff caps the exponential backoff delay between retries.
const maxBackoff = 5 * time.Second

func nextDelay(d, _ time.Duration) time.Duration {
	d *= 2
	if d > maxBackoff {
		d = maxBackoff
	}
	return d
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// jitterDuration returns a random duration in [0, base) using the process
// RNG (math/rand/v2 is concurrency-safe).
func jitterDuration(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(base)))
}
