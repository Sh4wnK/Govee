package govee

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Color is an RGB color (each channel 0-255).
type Color struct {
	R, G, B uint8
}

// RGB returns the 24-bit 0xRRGGBB integer the Govee API expects
// (range 0-16777215).
func (c Color) RGB() int {
	return int(c.R)<<16 | int(c.G)<<8 | int(c.B)
}

// ColorToRGBInt converts an RGB color to the API's 0xRRGGBB integer.
func ColorToRGBInt(c Color) int { return c.RGB() }

// RGBColor converts a 0xRRGGBB integer (0-16777215) to a Color.
func RGBColor(rgb int) Color {
	return Color{
		R: uint8(rgb >> 16 & 0xff),
		G: uint8(rgb >> 8 & 0xff),
		B: uint8(rgb & 0xff),
	}
}

// SetPower sends an on/off command to the device: on=true turns it on.
func (c *Client) SetPower(ctx context.Context, sku, deviceID string, on bool) error {
	return c.Control(ctx, sku, deviceID, OnCommand(on))
}

// SetBrightness sets the brightness of a device in percent (1-100).
// The value is validated against the device's advertised range when the
// optional device is non-nil and has the range capability.
func (c *Client) SetBrightness(ctx context.Context, sku, deviceID string, percent int) error {
	return c.Control(ctx, sku, deviceID, RangeCommand(InstanceBrightness, percent))
}

// SetColorRGB sets the color of a light to col.
func (c *Client) SetColorRGB(ctx context.Context, sku, deviceID string, col Color) error {
	return c.Control(ctx, sku, deviceID, ColorCommand(col))
}

// SetColorTemperature sets the color temperature in Kelvin
// (typically 2000-9000 K).
func (c *Client) SetColorTemperature(ctx context.Context, sku, deviceID string, kelvin int) error {
	return c.Control(ctx, sku, deviceID, ColorTemperatureCommand(kelvin))
}

// SetSegmentColor sets an RGB color on the given segments of a light strip
// (devices.capabilities.segment_color_setting, segmentedColorRgb).
func (c *Client) SetSegmentColor(ctx context.Context, sku, deviceID string, segments []int, col Color) error {
	return c.Control(ctx, sku, deviceID, SegmentColorCommand(segments, col))
}

// SetSegmentBrightness sets brightness on the given segments of a light
// strip (segmentedBrightness).
func (c *Client) SetSegmentBrightness(ctx context.Context, sku, deviceID string, segments []int, brightness int) error {
	return c.Control(ctx, sku, deviceID, SegmentBrightnessCommand(segments, brightness))
}

// SetScene applies a dynamic light scene by name (from ListScenes or the
// device's advertised lightScene options).
func (c *Client) SetScene(ctx context.Context, sku, deviceID, sceneName string) error {
	return c.Control(ctx, sku, deviceID, SceneCommand(sceneName))
}

// SetSceneID applies a dynamic light scene by numeric ID (from ListScenes).
func (c *Client) SetSceneID(ctx context.Context, sku, deviceID string, sceneID int) error {
	return c.Control(ctx, sku, deviceID, SceneCommandID(sceneID))
}

// SetWorkMode sets the work mode of a device (devices.capabilities.work_mode):
// e.g. SetWorkMode(ctx, sku, id, 1, 1) selects gear mode "Low" on a purifier.
func (c *Client) SetWorkMode(ctx context.Context, sku, deviceID string, workMode, modeValue int) error {
	return c.Control(ctx, sku, deviceID, WorkModeCommand(workMode, modeValue))
}

// SetTargetTemperature sets the target temperature of a heater or
// thermostat (devices.capabilities.temperature_setting). unit is "Celsius"
// or "Fahrenheit"; autoStop is 0 (maintain), 1 (auto stop), or -1 (omit).
func (c *Client) SetTargetTemperature(ctx context.Context, sku, deviceID string, temp int, unit string, autoStop int) error {
	return c.Control(ctx, sku, deviceID, TemperatureCommand(temp, unit, autoStop))
}

// Toggle toggles a toggle-type instance (e.g. oscillationToggle,
// nightlightToggle) on or off.
func (c *Client) Toggle(ctx context.Context, sku, deviceID, instance string, on bool) error {
	return c.Control(ctx, sku, deviceID, ToggleCommand(instance, on))
}

// SetMode sets an enum mode (devices.capabilities.mode, e.g. nightlightScene).
func (c *Client) SetMode(ctx context.Context, sku, deviceID, instance string, value int) error {
	return c.Control(ctx, sku, deviceID, EnumCommand(CapMode, instance, value))
}

// SetMusicMode toggles the music mode of a device
// (devices.capabilities.music_setting).
func (c *Client) SetMusicMode(ctx context.Context, sku, deviceID string, on bool) error {
	return c.Control(ctx, sku, deviceID, MusicSettingCommand(on))
}

// SetHumidity sets the target humidity of a humidifier (range capability,
// instance "humidity").
func (c *Client) SetHumidity(ctx context.Context, sku, deviceID string, percent int) error {
	return c.Control(ctx, sku, deviceID, RangeCommand(InstanceHumidity, percent))
}

// Device is the per-device convenience wrapper returned by
// Client.Device(sku, id). It carries the device's advertised capabilities
// (fetched via Devices) so helpers can validate values before sending, and
// it enforces Govee's per-device control limit (2 req/s sustained, burst 6)
// with its own token bucket.
type DeviceHandle struct {
	client   *Client
	sku      string
	deviceID string
	Device   *Device

	mu      sync.Mutex
	limiter *tokenBucket
}

// Device returns a per-device handle. If dev is non-nil (e.g. from
// Devices()), the handle validates commands against its advertised
// capabilities and derives sku/id from it; otherwise the sku and deviceID
// parameters are used as-is. The handle enforces Govee's per-device
// control limit of 2 req/s (burst 6) locally.
func (c *Client) Device(sku, deviceID string, dev *Device) *DeviceHandle {
	h := &DeviceHandle{
		client:   c,
		sku:      sku,
		deviceID: deviceID,
		Device:   dev,
		limiter:  newTokenBucket(6, 3*time.Second), // 2 req/s, burst 6
	}
	if dev != nil {
		h.sku = dev.SKU
		h.deviceID = dev.ID
	}
	return h
}

// Control sends a command through the client, after validating it against
// the device's advertised capabilities when available.
func (h *DeviceHandle) Control(ctx context.Context, cmd CapabilityCommand) error {
	if h.Device != nil {
		if err := ValidateCommand(h.Device, cmd); err != nil {
			return err
		}
	}
	h.mu.Lock()
	lim := h.limiter
	h.mu.Unlock()
	if lim != nil {
		if err := lim.Wait(ctx); err != nil {
			return fmt.Errorf("%w: per-device rate limit: %v", ErrRateLimited, err)
		}
	}
	return h.client.Control(ctx, h.sku, h.deviceID, cmd)
}

// SetPower turns the device on or off.
func (h *DeviceHandle) SetPower(ctx context.Context, on bool) error {
	if h.Device == nil {
		return fmt.Errorf("%w: device handle without Device data", ErrInvalidRequest)
	}
	return h.Control(ctx, OnCommand(on))
}

// SetBrightness sets the device brightness in percent.
func (h *DeviceHandle) SetBrightness(ctx context.Context, percent int) error {
	return h.Control(ctx, RangeCommand(InstanceBrightness, percent))
}

// SetColorRGB sets the device color.
func (h *DeviceHandle) SetColorRGB(ctx context.Context, col Color) error {
	return h.Control(ctx, ColorCommand(col))
}

// SetColorTemperature sets the color temperature in Kelvin.
func (h *DeviceHandle) SetColorTemperature(ctx context.Context, kelvin int) error {
	return h.Control(ctx, ColorTemperatureCommand(kelvin))
}

// SetSegmentColor sets an RGB color on the given segments.
func (h *DeviceHandle) SetSegmentColor(ctx context.Context, segments []int, col Color) error {
	return h.Control(ctx, SegmentColorCommand(segments, col))
}

// SetScene applies a scene by name.
func (h *DeviceHandle) SetScene(ctx context.Context, sceneName string) error {
	return h.Control(ctx, SceneCommand(sceneName))
}

// SetWorkMode sets the work mode.
func (h *DeviceHandle) SetWorkMode(ctx context.Context, workMode, modeValue int) error {
	return h.Control(ctx, WorkModeCommand(workMode, modeValue))
}

// Toggle toggles a toggle instance.
func (h *DeviceHandle) Toggle(ctx context.Context, instance string, on bool) error {
	return h.Control(ctx, ToggleCommand(instance, on))
}

// State fetches the device state.
func (h *DeviceHandle) State(ctx context.Context) (*State, error) {
	if h.Device == nil {
		return nil, fmt.Errorf("%w: device handle without Device data", ErrInvalidRequest)
	}
	return h.client.State(ctx, h.Device.SKU, h.Device.ID)
}

// Supports reports whether the device advertises the given capability.
func (h *DeviceHandle) Supports(typeName, instance string) bool {
	return h.Device != nil && h.Device.Supports(typeName, instance)
}

// BrightnessRange returns the advertised brightness range of the device.
func (h *DeviceHandle) BrightnessRange() (ValueRange, bool) {
	if h.Device == nil {
		return ValueRange{}, false
	}
	return h.Device.Range(CapRange, InstanceBrightness)
}
