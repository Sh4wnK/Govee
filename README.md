# Govee

A complete, strongly typed Go client for the [Govee Developer Platform
OpenAPI](https://developer.govee.com/reference) and the Govee LAN control
protocol.

The library covers the full device catalogue the official APIs support —
lights, air purifiers, thermometers, sockets, sensors, heaters, humidifiers,
dehumidifiers, ice makers, aroma diffusers, fans, kettles and more — with
typed commands, capability discovery, client-side validation, rate limiting
and automatic retries, so applications can manage Govee devices
programmatically with no app in the loop.

```go
import "github.com/Sh4wnK/Govee"

c := govee.NewClient(os.Getenv("GOVEE_API_KEY"))

devices, err := c.Devices(ctx)          // what do I have, and what can it do?
light := devices[0]

if light.Supports(govee.CapOnOff, govee.InstancePowerSwitch) {
    c.SetPower(ctx, light.SKU, light.ID, true)
}
if r, ok := light.Range(govee.CapRange, govee.InstanceBrightness); ok {
    // r.Min..r.Max — validated before the request is sent
    c.SetBrightness(ctx, light.SKU, light.ID, int(r.Max/2))
}
c.SetColorRGB(ctx, light.SKU, light.ID, govee.Color{R: 255, G: 128, B: 0})
c.SetColorTemperature(ctx, light.SKU, light.ID, 6500)
```

For LAN-only control (no API key, same network):

```go
import "github.com/Sh4wnK/Govee/lan"

devices, _ := lan.Scan(ctx)      // UDP multicast discovery
for _, d := range devices {
    c := lan.NewClient(d.IP)
    c.Turn(ctx, true)
    c.Color(ctx, 255, 0, 0)
    status, _ := c.DevStatus(ctx)
}
```

## Why this library

- **One codebase for the whole ecosystem.** Every capability the OpenAPI
  documents is typed: `on_off`, `toggle`, `range`, `mode`,
  `color_setting`, `segment_color_setting`, `dynamic_scene`,
  `music_setting`, `work_mode`, `temperature_setting`, plus the read-only
  `online`, `property` and `event` capabilities.
- **No app required.** The full device/state/control/scenes surface is
  exposed through a small, documented API — see `cmd/govee` for a
  ready-made CLI and `examples/` for programs.
- **Made for sustained use.** Client-side token-bucket rate limiting honors
  Govee's published limits (12 req/s account, 2 req/s per device for
  control), transient failures (HTTP 429/5xx) are retried with exponential
  backoff, jitter, and `Retry-After` support, and the connection-reusing
  `http.Client` keeps keep-alive connections hot. The soak test fires 600
  concurrent calls through a fault-injecting server and asserts zero
  failures — reproduce it with:
  ```
  go test -run TestSoak500Calls -v
  ```

## Architecture

```
cmd/govee/          CLI (devices, state, power, brightness, color, scenes, lan scan)
examples/           runnable example programs (basic, lanctl)
lan/                LAN control: multicast discovery + per-device commands
testdata/           JSON fixtures captured from the official API docs
├── client.go       transport, retries/backoff, token bucket plumbing
├── api.go          endpoint methods: Devices, State, Control, ListScenes, ...
├── types.go        typed helpers (SetPower, SetBrightness, SetScene, ...)
├── commands.go     capability command constructors + validation
├── device.go       Device/Capability model with discovery helpers
├── errors.go       APIError + sentinel errors (errors.Is/As friendly)
└── ratelimit.go    concurrency-safe token bucket
```

Flow of a control call:

```
SetPower(ctx, sku, id, on)
  -> Control(ctx, sku, id, OnCommand(on))
    -> ValidateCommand against device capabilities (when known)   [client.go-side]
    -> per-device token bucket (2 req/s, burst 6)
    -> account token bucket (12 req/s, burst 80)
    -> POST /router/api/v1/device/control  (+ requestId envelope)
    -> on 429/5xx/network error: exponential backoff + jitter, Retry-After honored
```

### Design decisions

- **Strongly typed requests.** No `map[string]interface{}` anywhere on the
  send path; capability values are typed structs (`CapabilityCommand`,
  `WorkModeCommand`, `SegmentColorCommand`, ...). Because `Value` is `any`,
  the wire format still matches the API exactly.
- **Capabilities drive validation.** `Device` carries the parameters the
  API advertises (`GET /router/api/v1/user/devices`), and
  `ValidateCommand` rejects out-of-range brightness, unknown scenes, and
  missing struct fields *before* anything hits the network — which is what
  keeps the error rate low under sustained use.
- **Two rate-limiting layers.** Govee enforces both account-level (12 req/s)
  and per-device (2 req/s) control limits. `Client` enforces the account
  bucket; `Client.Device(sku, id, dev)` returns a per-device handle with
  its own bucket, so two devices controlled from one client each get their
  full headroom.
- **Honest errors.** `APIError` carries HTTP status, API code, message,
  requestId and endpoint, and supports `errors.Is(err, govee.ErrRateLimited)`.
- **No external dependencies.** The library uses only the Go standard
  library.

## Quickstart

1. Get an API key: Govee Home app → Settings → Apply for API key
   (https://developer.govee.com/reference/apply-you-govee-api-key).
2. Add a device to your Govee account (the app and the API share the
   account's devices).
3. Build the CLI or use the library:

```sh
export GOVEE_API_KEY=...

go run ./cmd/govee devices
go run ./cmd/govee state H6601 9D:FA:85:EB:D3:00:8B:FF
go run ./cmd/govee power H6601 9D:FA:85:EB:D3:00:8B:FF on
go run ./cmd/govee brightness H6601 9D:FA:85:EB:D3:00:8B:FF 50
go run ./cmd/govee color H6601 9D:FA:85:EB:D3:00:8B:FF 255,128,0
go run ./cmd/govee scenes H618E 8C:2E:9C:04:A0:03:82:D1
go run ./cmd/govee lan scan
```

## Configuration

The `Client` is configured with functional options:

| Option | Default | Purpose |
| --- | --- | --- |
| `WithHTTPClient(c *http.Client)` | keep-alive client | Custom transport; used by tests and proxies |
| `WithBaseURL(url)` | `https://openapi.api.govee.com` | Govee test environment / local testing |
| `WithTimeout(d)` | 30s | Per-request HTTP timeout |
| `WithRateLimit(allow, window)` | 720/min (12/s) | Account-level token bucket |
| `WithNoRateLimit()` | — | Opt out (you own the limits) |
| `WithRetryPolicy(maxAttempts, baseDelay)` | 5, 200ms | Retry budget for 429/5xx |

The CLI reads the API key from `GOVEE_API_KEY` (see `.env.example`); the
test environment can be selected with `GOVEE_BASE_URL`.

## Client API

### Devices & discovery

| Method | Description |
| --- | --- |
| `Devices(ctx) ([]Device, error)` | All account devices + advertised capabilities |
| `(Device).Supports(type, instance) bool` | Capability discovery |
| `(Device).Range(type, instance) (ValueRange, bool)` | min/max/precision of a range capability |
| `(Device).Options(type, instance) ([]CapabilityOption, bool)` | enum choices (scenes, modes, ...) |
| `ValidateCommand(dev, cmd) error` | Client-side validation against advertised parameters |

### Control

All methods take `ctx, sku, deviceID` and return `error`:

| Method | Capability |
| --- | --- |
| `SetPower(ctx, sku, id, on)` / `SetColorRGB` / `SetBrightness` | contract API |
| `SetColorTemperature(kelvin)` | `color_setting` / `colorTemperatureK` |
| `SetSegmentColor(segments, color)` / `SetSegmentBrightness` | `segment_color_setting` |
| `SetScene(name)` / `SetSceneID(id)` | `dynamic_scene` / `lightScene` |
| `SetWorkMode(workMode, modeValue)` | `work_mode` |
| `SetTargetTemperature(temp, unit, autoStop)` | `temperature_setting` |
| `Toggle(instance, on)` / `SetMode(instance, value)` | `toggle` / `mode` |
| `SetHumidity(percent)` | `range` / `humidity` |
| `SetMusicMode(on)` | `music_setting` |
| `Control(ctx, sku, id, CapabilityCommand)` | any capability, fully typed |

### State queries

| Method | Description |
| --- | --- |
| `State(ctx, sku, id) (*State, error)` | Full state; `State.Bool/Number/String(instance)` accessors |
| `ListScenes(ctx, sku, id) ([]Scene, error)` | Dynamic scenes (`/device/scenes`) |
| `ListDIYScenes(ctx, sku, id) ([]Scene, error)` | DIY scenes (`/device/diy-scenes`) |

### Errors

`*govee.APIError` carries `Status`, `Code`, `Message`, `RequestID`,
`Endpoint` and `RetryAfter`; sentinels `ErrRateLimited`,
`ErrUnauthorized`, `ErrDeviceNotFound`, `ErrValidation`,
`ErrNotSupported`, `ErrInvalidRequest` work with `errors.Is`.

### LAN (`govee/lan`)

- `lan.Scan(ctx) ([]Device, error)` — multicast discovery
  (239.255.255.250:4001), replies on 4002.
- `lan.NewClient(ip)` — commands on port 4003: `Turn`, `Brightness`,
  `Color`, `ColorWC`, `Scene`, `MusicMode`, `DevStatus`. Timeout and
  sockets are injectable for tests.

## Rate limits (per Govee's docs)

| API | Actor | Limit |
| --- | --- | --- |
| `GET /router/api/v1/user/devices` | Account | 30 req/min |
| `POST /router/api/v1/device/control` | Account | 12 req/s (burst 80) |
| `POST /router/api/v1/device/control` | Device | 2 req/s (burst 6) |
| `POST /router/api/v1/device/state` | Device | 30 req/min |
| `POST /router/api/v1/device/scenes` | Device | 30 req/min |

The client enforces the account control limit and, via
`Client.Device(...)`, the per-device control limit locally, so a sustained
integration stays within budget instead of discovering it via 429s.

## Performance

Measured on this machine (Apple M3, macOS) with
`go test -bench . -benchmem -run '^$'`:

| Benchmark | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `BenchmarkControl` (steady state, local fake API) | ~11,000 | ~9,900 | ~121 |

Under sustained use, the soak test
(`go test -run TestSoak500Calls -v`) drives 600 concurrent control calls
through a server that injects three transient failures (429/503/500) per
call, then asserts all 600 succeed — every failure absorbed by the retry
layer. Log line from the run: `soak: 600/600 calls succeeded; 2400 requests,
1800 injected failures`.

## Project layout

```
.
├── cmd/govee/        CLI
├── examples/         basic (cloud) and lanctl (LAN) example programs
├── lan/              LAN control sub-package
├── testdata/         API fixtures taken from the official documentation
├── *.go              library sources (see Architecture)
└── *.go tests        unit, integration (httptest/loopback UDP), race, soak, benchmarks
```

## Testing

```sh
go test ./...          # unit + integration + soak (skipped with -short)
go test -race ./...    # race detector (CI does this)
go vet ./...           # static checks
```

The test suite covers request shapes, response parsing against fixtures
copied from the official docs, error mapping, retry/backoff behavior,
token-bucket semantics, capability validation, and LAN discovery/commands
against a loopback fake device.

## License

MIT — see [LICENSE](LICENSE). Copyright (c) 2026 Gurshawn Kaloty.

This project is not affiliated with or endorsed by Govee. Govee is a
trademark of its owner.
