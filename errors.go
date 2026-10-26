// Package govee is a complete, strongly typed Go client for the Govee
// Developer Platform OpenAPI (https://openapi.api.govee.com).
//
// It covers the full device catalogue exposed by the official API - lights,
// air purifiers, thermometers, sockets, sensors, heaters, humidifiers,
// dehumidifiers, ice makers, aroma diffusers and more - via a small set of
// type-safe endpoints, plus a LAN control sub-package for low-latency
// discovery and control on the local network.
//
// The client is concurrency-safe and designed for sustained use: requests are
// rate limited with a token bucket honoring Govee's per-account and per-device
// limits, and transient failures (HTTP 429/5xx) are retried with exponential
// backoff and jitter.
//
// See https://developer.govee.com for the official API documentation.
package govee

import (
	"errors"
	"fmt"
	"net/http"
)

// Sentinel errors returned by the client. Use errors.Is to test for them.
var (
	// ErrRateLimited is returned when the local rate limiter refuses a
	// request or when the API returns HTTP 429 and retries are exhausted.
	ErrRateLimited = errors.New("govee: rate limit exceeded")

	// ErrUnauthorized is returned when the API rejects the API key
	// (HTTP 401 or 403).
	ErrUnauthorized = errors.New("govee: unauthorized (invalid or missing API key)")

	// ErrDeviceNotFound is returned when the API reports an unknown
	// device or SKU (HTTP 404).
	ErrDeviceNotFound = errors.New("govee: device not found")

	// ErrValidation is returned by client-side validation helpers when a
	// control value violates the capability parameters advertised by the
	// device (e.g. brightness outside the advertised range).
	ErrValidation = errors.New("govee: validation failed")

	// ErrNotSupported is returned when the requested capability is not
	// advertised by the device.
	ErrNotSupported = errors.New("govee: capability not supported by device")

	// ErrInvalidRequest is returned for malformed client-side requests,
	// capability values of the wrong shape, and empty required fields.
	ErrInvalidRequest = errors.New("govee: invalid request")
)

// APIError is the error type returned for non-2xx HTTP responses and for
// API-level failures reported inside a 200 response body (code != 200).
type APIError struct {
	// HTTP status code of the response. Zero for API-level errors that
	// came inside an HTTP 200 body.
	Status int
	// Code is the Govee API code from the response body, when present.
	Code int
	// Message is the human-readable error message, when present.
	Message string
	// RequestID echoes the requestId sent with the request, when the
	// API returned it. Useful for support conversations.
	RequestID string
	// Endpoint is the API path the error came from (e.g. "/router/api/v1/device/control").
	Endpoint string
	// RetryAfter is the number of seconds the API asked the client to
	// wait before retrying (from the Retry-After header on 429/503), or
	// -1 when absent.
	RetryAfter int
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = "unknown API error"
	}
	switch {
	case e.Status == http.StatusTooManyRequests:
		return fmt.Sprintf("govee: rate limited (HTTP 429): %s", msg)
	case e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden:
		return fmt.Sprintf("govee: unauthorized (HTTP %d): %s", e.Status, msg)
	case e.Status == http.StatusNotFound:
		return fmt.Sprintf("govee: not found (HTTP 404): %s", msg)
	case e.Status != 0 && e.Code != 0:
		return fmt.Sprintf("govee: HTTP %d, API code %d: %s", e.Status, e.Code, msg)
	case e.Code != 0:
		return fmt.Sprintf("govee: API code %d: %s", e.Code, msg)
	case e.Status != 0:
		return fmt.Sprintf("govee: HTTP %d: %s", e.Status, msg)
	default:
		return "govee: " + msg
	}
}

// Unwrap makes APIError compatible with errors.Unwrap/errors.Is chains so
// callers can still match on the sentinel errors above.
func (e *APIError) Unwrap() error {
	switch {
	case e.Status == http.StatusTooManyRequests:
		return ErrRateLimited
	case e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden:
		return ErrUnauthorized
	case e.Status == http.StatusNotFound:
		return ErrDeviceNotFound
	default:
		return nil
	}
}

// Is reports whether target matches one of the sentinel errors, without
// requiring the exact error value. This makes sentinel matching work with
// APIError values that were created without going through Unwrap.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrRateLimited:
		return e.Status == http.StatusTooManyRequests
	case ErrUnauthorized:
		return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
	case ErrDeviceNotFound:
		return e.Status == http.StatusNotFound
	default:
		return false
	}
}

// AsError converts an error returned by this package to *APIError, when
// possible. It returns nil when the error is not an APIError.
func AsError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}
