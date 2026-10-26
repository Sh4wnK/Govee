package govee

import (
	"encoding/json"
	"errors"
	"fmt"
)

// This file holds the typed capability command constructors and the
// client-side validation helpers that check control values against the
// capability parameters advertised by a device.

// OnCommand builds a CapabilityCommand that turns a device on or off
// (devices.capabilities.on_off, instance powerSwitch).
func OnCommand(on bool) CapabilityCommand {
	return EnumCommand(CapOnOff, InstancePowerSwitch, boolToInt(on))
}

// ToggleCommand builds a CapabilityCommand for a toggle instance (e.g.
// oscillationToggle, nightlightToggle, gradientToggle).
func ToggleCommand(instance string, on bool) CapabilityCommand {
	return EnumCommand(CapToggle, instance, boolToInt(on))
}

// EnumCommand builds a CapabilityCommand for an enum-valued capability
// (mode, on_off, toggle) with an int value.
func EnumCommand(capType, instance string, value int) CapabilityCommand {
	return CapabilityCommand{Type: capType, Instance: instance, Value: value}
}

// RangeCommand builds a CapabilityCommand for a range capability (e.g.
// brightness with instance "brightness").
func RangeCommand(instance string, value int) CapabilityCommand {
	return CapabilityCommand{Type: CapRange, Instance: instance, Value: value}
}

// ColorCommand builds a CapabilityCommand that sets an RGB color:
// devices.capabilities.color_setting, instance colorRgb, value as 0xRRGGBB.
func ColorCommand(col Color) CapabilityCommand {
	return CapabilityCommand{
		Type:     CapColorSetting,
		Instance: InstanceColorRgb,
		Value:    ColorToRGBInt(col),
	}
}

// ColorTemperatureCommand builds a CapabilityCommand for colorTemperatureK
// (Kelvin, typically 2000-9000).
func ColorTemperatureCommand(kelvin int) CapabilityCommand {
	return CapabilityCommand{
		Type:     CapColorSetting,
		Instance: InstanceColorTemperatureK,
		Value:    kelvin,
	}
}

// SegmentColorCommand builds a CapabilityCommand setting colors on a range
// of segments (devices.capabilities.segment_color_setting, segmentedColorRgb).
func SegmentColorCommand(segments []int, col Color) CapabilityCommand {
	return CapabilityCommand{
		Type:     CapSegmentColor,
		Instance: InstanceSegmentedColorRgb,
		Value:    map[string]any{"segment": segments, "rgb": ColorToRGBInt(col)},
	}
}

// SegmentBrightnessCommand builds a CapabilityCommand setting brightness on
// a range of segments (segmentedBrightness).
func SegmentBrightnessCommand(segments []int, brightness int) CapabilityCommand {
	return CapabilityCommand{
		Type:     CapSegmentColor,
		Instance: InstanceSegmentedBrightness,
		Value:    map[string]any{"segment": segments, "brightness": brightness},
	}
}

// SceneCommand builds a CapabilityCommand that applies a scene by name.
// The scene must be one of the device's advertised options (see
// Device.Options(CapDynamicScene, ...) or ListScenes).
func SceneCommand(sceneName string) CapabilityCommand {
	return CapabilityCommand{Type: CapDynamicScene, Instance: InstanceLightScene, Value: sceneName}
}

// SceneCommandID builds a CapabilityCommand that applies a scene by its
// numeric ID (as returned by ListScenes).
func SceneCommandID(id int) CapabilityCommand {
	return CapabilityCommand{Type: CapDynamicScene, Instance: InstanceLightScene, Value: id}
}

// WorkModeCommand builds a CapabilityCommand setting a work mode and its
// value (devices.capabilities.work_mode): e.g. WorkModeCommand(1, 1) for
// gear mode Low on a purifier.
func WorkModeCommand(workMode, modeValue int) CapabilityCommand {
	return CapabilityCommand{
		Type:     CapWorkMode,
		Instance: InstanceWorkMode,
		Value:    map[string]any{"workMode": workMode, "modeValue": modeValue},
	}
}

// TemperatureCommand builds a CapabilityCommand for targetTemperature
// (devices.capabilities.temperature_setting). unit is "Celsius" or
// "Fahrenheit". autoStop (0/1) optionally stops the device when the target
// is reached; pass -1 to omit it (server default).
func TemperatureCommand(kelvinOrDegrees int, unit string, autoStop int) CapabilityCommand {
	v := map[string]any{"temperature": kelvinOrDegrees, "unit": unit}
	if autoStop == 0 || autoStop == 1 {
		v["autoStop"] = autoStop
	}
	return CapabilityCommand{Type: CapTemperatureSet, Instance: InstanceTargetTemperature, Value: v}
}

// MusicSettingCommand builds a CapabilityCommand for the music mode
// (devices.capabilities.music_setting). value is typically 0 (off) or 1 (on).
func MusicSettingCommand(on bool) CapabilityCommand {
	return CapabilityCommand{Type: CapMusicSetting, Instance: "musicMode", Value: boolToInt(on)}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ValidateCommand checks a CapabilityCommand against the capability
// parameters advertised by the device. It enforces:
//
//   - the device advertises the capability (type+instance),
//   - enum values are among the advertised options,
//   - numeric values fall within the advertised range and honor precision,
//   - struct values include every required field with a valid value.
//
// It returns ErrNotSupported when the capability is missing, ErrValidation
// when a value violates the advertised parameters, or nil when the command
// is valid. Devices() must have been called first so Capabilities is
// populated.
func ValidateCommand(d *Device, cmd CapabilityCommand) error {
	if d == nil {
		return errors.New("govee: nil device")
	}
	if cmd.Type == "" || cmd.Instance == "" {
		return fmt.Errorf("%w: command type and instance are required", ErrInvalidRequest)
	}
	cap, ok := d.Capability(cmd.Type, cmd.Instance)
	if !ok {
		return fmt.Errorf("%w: %s/%s", ErrNotSupported, cmd.Type, cmd.Instance)
	}
	p, err := UnmarshalCapabilityParameters(cap)
	if err != nil {
		return nil // unparseable parameters: let the server decide
	}
	switch p.DataType {
	case "ENUM":
		if err := validateEnum(cmd.Value, p.Options); err != nil {
			return err
		}
	case "INTEGER", "NUMBER":
		if err := validateNumber(cmd.Value, p.Range); err != nil {
			return err
		}
	case "STRUCT":
		if err := validateStruct(cmd.Value, p.Fields); err != nil {
			return err
		}
	}
	return nil
}

func validateEnum(v any, opts []CapabilityOption) error {
	if len(opts) == 0 {
		return nil // no options advertised: server decides
	}
	_, ok := findOption(opts, v)
	if ok {
		return nil
	}
	// Scene and mode commands send the option name (e.g. "Party" for a
	// dynamic scene); match names as well as numeric values.
	if name, isStr := v.(string); isStr {
		for _, o := range opts {
			if o.Name == name {
				return nil
			}
			if containsNamedOption(o.Options, name) {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: value %v is not among the advertised options", ErrValidation, v)
}

// containsNamedOption searches nested option trees by name.
func containsNamedOption(opts []CapabilityOption, name string) bool {
	for _, o := range opts {
		if o.Name == name {
			return true
		}
		if containsNamedOption(o.Options, name) {
			return true
		}
	}
	return false
}

// findOption searches for a matching option value (int/float/string) or
// nested option object ({paramId, id}).
func findOption(opts []CapabilityOption, v any) (CapabilityOption, bool) {
	for _, o := range opts {
		if optionValueEquals(o.Value, v) {
			return o, true
		}
		if len(o.Options) > 0 {
			if fo, ok := findOption(o.Options, v); ok {
				return fo, true
			}
		}
	}
	return CapabilityOption{}, false
}

func optionValueEquals(a, b any) bool {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		return af == bf
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func validateNumber(v any, r *ValueRange) error {
	f, ok := toFloat(v)
	if !ok {
		return fmt.Errorf("%w: expected a number, got %T", ErrValidation, v)
	}
	if r == nil {
		return nil
	}
	if f < r.Min || f > r.Max {
		return fmt.Errorf("%w: value %v out of range [%v, %v]", ErrValidation, f, r.Min, r.Max)
	}
	if r.Precision > 0 {
		step := int(r.Precision)
		base := int(f) / step * step
		if int(f) != base {
			return fmt.Errorf("%w: value %v must be a multiple of %v", ErrValidation, f, r.Precision)
		}
	}
	return nil
}

// validateStruct checks each required field, recursing for nested enums,
// ranges, and arrays.
func validateStruct(v any, fields []ParameterField) error {
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: expected a struct value, got %T", ErrValidation, v)
	}
	for _, f := range fields {
		fv, present := m[f.FieldName]
		if !present || isZeroValue(fv) {
			if f.Required {
				return fmt.Errorf("%w: required field %q is missing", ErrValidation, f.FieldName)
			}
			continue
		}
		switch f.DataType {
		case "ENUM":
			if err := validateEnum(fv, f.Options); err != nil {
				return fmt.Errorf("%w: field %s: %v", ErrValidation, f.FieldName, err)
			}
		case "INTEGER", "NUMBER":
			if err := validateNumber(fv, f.Range); err != nil {
				return fmt.Errorf("%w: field %s: %v", ErrValidation, f.FieldName, err)
			}
		case "Array":
			a, err := asSlice(fv)
			if err != nil {
				return fmt.Errorf("%w: field %s: %v", ErrValidation, f.FieldName, err)
			}
			if f.Size != nil && (len(a) < int(f.Size.Min) || len(a) > int(f.Size.Max)) {
				return fmt.Errorf("%w: field %s has %d elements, want [%v,%v]", ErrValidation, f.FieldName, len(a), f.Size.Min, f.Size.Max)
			}
			if f.ElementRange != nil {
				for _, e := range a {
					if err := validateNumber(e, f.ElementRange); err != nil {
						return fmt.Errorf("%w: field %s: %v", ErrValidation, f.FieldName, err)
					}
				}
			}
		}
	}
	return nil
}

func isZeroValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case float64:
		return t == 0
	case int:
		return t == 0
	}
	return false
}

// asSlice converts any slice type ([]any from JSON, or a []int constructed
// directly) to []any for uniform field validation.
func asSlice(v any) ([]any, error) {
	switch s := v.(type) {
	case []any:
		return s, nil
	case []int:
		out := make([]any, len(s))
		for i, e := range s {
			out[i] = e
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected an array, got %T", v)
	}
}
