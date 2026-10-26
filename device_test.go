package govee

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

func loadTestDevice(t *testing.T, sku string) *Device {
	t.Helper()
	var resp devicesResponse
	if err := json.Unmarshal(readFixture(t, "devices.json"), &resp); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	for i := range resp.Data {
		if resp.Data[i].SKU == sku {
			return &resp.Data[i]
		}
	}
	t.Fatalf("device %s not in fixture", sku)
	return nil
}

func TestValidateCommandEnum(t *testing.T) {
	strip := loadTestDevice(t, "H6601")

	// on_off accepts 0/1.
	if err := ValidateCommand(strip, OnCommand(true)); err != nil {
		t.Errorf("OnCommand(true): %v", err)
	}
	if err := ValidateCommand(strip, OnCommand(false)); err != nil {
		t.Errorf("OnCommand(false): %v", err)
	}
	// Invalid enum value must be rejected.
	if err := ValidateCommand(strip, EnumCommand(CapOnOff, InstancePowerSwitch, 7)); !errors.Is(err, ErrValidation) {
		t.Errorf("bad enum value: err = %v, want ErrValidation", err)
	}
	// Unadvertised capability must be ErrNotSupported.
	err := ValidateCommand(strip, EnumCommand(CapMode, "hdmiSource", 1))
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("unadvertised capability: err = %v, want ErrNotSupported", err)
	}
}

func TestValidateCommandRange(t *testing.T) {
	strip := loadTestDevice(t, "H6601")
	// Brightness 1-100.
	if err := ValidateCommand(strip, RangeCommand(InstanceBrightness, 100)); err != nil {
		t.Errorf("brightness 100: %v", err)
	}
	err := ValidateCommand(strip, RangeCommand(InstanceBrightness, 101))
	if !errors.Is(err, ErrValidation) {
		t.Errorf("brightness 101: err = %v, want ErrValidation", err)
	}
	err = ValidateCommand(strip, RangeCommand(InstanceBrightness, 0))
	if !errors.Is(err, ErrValidation) {
		t.Errorf("brightness 0: err = %v, want ErrValidation", err)
	}
	// colorTemperatureK 2000-9000.
	if err := ValidateCommand(strip, ColorTemperatureCommand(2700)); err != nil {
		t.Errorf("kelvin 2700: %v", err)
	}
	if err := ValidateCommand(strip, ColorTemperatureCommand(1000)); !errors.Is(err, ErrValidation) {
		t.Errorf("kelvin 1000: err = %v, want ErrValidation", err)
	}
}

func TestValidateCommandStruct(t *testing.T) {
	strip := loadTestDevice(t, "H6601")
	// Segment command requires segment array 1..15 elements with values
	// 0..14 plus rgb 0..16777215.
	cmd := SegmentColorCommand([]int{0, 1, 2, 3, 4}, Color{R: 255, G: 0, B: 255})
	if err := ValidateCommand(strip, cmd); err != nil {
		t.Errorf("valid segment command: %v", err)
	}
	// Out-of-range segment index.
	cmd.Value.(map[string]any)["segment"] = []int{0, 99}
	if err := ValidateCommand(strip, cmd); !errors.Is(err, ErrValidation) {
		t.Errorf("bad segment index: err = %v, want ErrValidation", err)
	}
	// Too many segments.
	tooMany := make([]int, 16)
	for i := range tooMany {
		tooMany[i] = i
	}
	cmd.Value.(map[string]any)["segment"] = tooMany
	if err := ValidateCommand(strip, cmd); !errors.Is(err, ErrValidation) {
		t.Errorf("16 segments: err = %v, want ErrValidation", err)
	}
	// Missing required field.
	cmd = SegmentColorCommand(nil, Color{R: 1, G: 2, B: 3})
	cmd.Value = map[string]any{"rgb": 0x010203}
	if err := ValidateCommand(strip, cmd); !errors.Is(err, ErrValidation) {
		t.Errorf("missing segment: err = %v, want ErrValidation", err)
	}
}

func TestValidateCommandWorkMode(t *testing.T) {
	hum := loadTestDevice(t, "H7143")
	// Manual mode value 1 with modeValue among 1..9.
	cmd := WorkModeCommand(1, 5)
	if err := ValidateCommand(hum, cmd); err != nil {
		t.Errorf("valid work mode: %v", err)
	}
	// modeValue 99 is not advertised.
	cmd = WorkModeCommand(1, 99)
	if err := ValidateCommand(hum, cmd); !errors.Is(err, ErrValidation) {
		t.Errorf("bad modeValue: err = %v, want ErrValidation", err)
	}
	// Auto mode (3) omits modeValue (not required).
	cmd = WorkModeCommand(3, 0)
	// modeValue=0 for Auto: field has defaultValue 0 and is not required.
	cmd.Value = map[string]any{"workMode": 3}
	if err := ValidateCommand(hum, cmd); err != nil {
		t.Errorf("auto work mode: %v", err)
	}
}

func TestValidateScene(t *testing.T) {
	strip := loadTestDevice(t, "H6601")
	if err := ValidateCommand(strip, SceneCommand("Party")); err != nil {
		t.Errorf("scene Party: %v", err)
	}
	if err := ValidateCommand(strip, SceneCommand("Not A Scene")); !errors.Is(err, ErrValidation) {
		t.Errorf("unknown scene: err = %v, want ErrValidation", err)
	}
}

func TestValidateCommandNilDevice(t *testing.T) {
	if err := ValidateCommand(nil, OnCommand(true)); err == nil {
		t.Error("nil device must error")
	}
}

func TestDeviceHandleValidatesAndControls(t *testing.T) {
	strip := loadTestDevice(t, "H6601")
	srv, recs := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"requestId":"x","code":200,"msg":"success"}`))
	})
	c := fixedClient(t, srv)
	h := c.Device(strip.SKU, strip.ID, strip)

	// Out-of-range value rejected before any HTTP round trip.
	err := h.SetBrightness(context.Background(), 500)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if len(*recs) != 0 {
		t.Fatalf("server saw %d requests, want 0 (validation must be local)", len(*recs))
	}

	if err := h.SetBrightness(context.Background(), 42); err != nil {
		t.Fatalf("SetBrightness: %v", err)
	}
	if err := h.SetPower(context.Background(), true); err != nil {
		t.Fatalf("SetPower: %v", err)
	}
	if err := h.SetColorRGB(context.Background(), Color{R: 1, G: 2, B: 3}); err != nil {
		t.Fatalf("SetColorRGB: %v", err)
	}
	if !h.Supports(CapOnOff, InstancePowerSwitch) {
		t.Error("Supports(on_off) = false")
	}
	if _, ok := h.BrightnessRange(); !ok {
		t.Error("BrightnessRange missing")
	}
	if len(*recs) != 3 {
		t.Errorf("server saw %d requests, want 3", len(*recs))
	}
}

func TestDeviceHandleRateLimitsPerDevice(t *testing.T) {
	strip := loadTestDevice(t, "H6601")
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"requestId":"x","code":200,"msg":"success"}`))
	})
	c := fixedClient(t, srv)
	h := c.Device(strip.SKU, strip.ID, strip)
	// Burst 6 then throttled: 8 quick calls must succeed (~2/s pace).
	start := time.Now()
	for i := 0; i < 8; i++ {
		if err := h.SetPower(context.Background(), true); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("8 per-device calls completed in %v, want >= ~1s (2 req/s limit)", elapsed)
	}
}

func TestColorConversions(t *testing.T) {
	if c := (Color{R: 255, G: 0, B: 255}).RGB(); c != 0xFF00FF {
		t.Errorf("magenta RGB() = %#x, want 0xff00ff", c)
	}
	if c := RGBColor(0x123456); c.R != 0x12 || c.G != 0x34 || c.B != 0x56 {
		t.Errorf("RGBColor(0x123456) = %+v", c)
	}
	if c := RGBColor(0); c != (Color{}) {
		t.Errorf("RGBColor(0) = %+v", c)
	}
}

func TestBucketWaitBlockingBehavior(t *testing.T) {
	// Each device handle gets its own token bucket (Govee's 2 req/s per
	// device with burst 6), independent of other devices.
	strip := loadTestDevice(t, "H6601")
	hum := loadTestDevice(t, "H7143")
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"requestId":"x","code":200,"msg":"success"}`))
	})
	c := fixedClient(t, srv)
	h1 := c.Device(strip.SKU, strip.ID, strip)
	h2 := c.Device(hum.SKU, hum.ID, hum)
	start := time.Now()
	// Interleave two devices with 8 calls each: each device exceeds its
	// burst of 6, so both must be paced to their own 2 req/s. The two
	// buckets run independently (no cross-device serialization).
	for i := 0; i < 8; i++ {
		if err := h1.SetPower(context.Background(), true); err != nil {
			t.Fatalf("h1 call %d: %v", i, err)
		}
		if err := h2.SetPower(context.Background(), true); err != nil {
			t.Fatalf("h2 call %d: %v", i, err)
		}
	}
	elapsed := time.Since(start)
	// Burst 6 per device is consumed instantly; the 7th and 8th call on
	// each device then pace at 2 req/s (0.5s apart) and the two devices
	// wait concurrently, so the whole run lands near ~1s. A shared bucket
	// would serialize both devices and take ~4x longer.
	if elapsed < 750*time.Millisecond {
		t.Errorf("16 calls across 2 devices in %v, want >= ~0.75s (per-device 2 req/s pacing)", elapsed)
	}
	if elapsed > 6*time.Second {
		t.Errorf("16 calls across 2 devices took %v, want <= ~6s (no starvation)", elapsed)
	}
}
