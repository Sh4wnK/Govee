package govee

import (
	"encoding/json"
	"fmt"
)

// Device is a Govee device discovered via the account API. It carries the
// capability descriptions the API advertises for the device, which the
// client uses for validation and discovery (Supports, Range, Options).
type Device struct {
	SKU          string       `json:"sku"`
	ID           string       `json:"device"`
	Name         string       `json:"deviceName"`
	Type         string       `json:"type"`
	Capabilities []Capability `json:"capabilities"`
}

// Capability describes a single capability of a device, exactly as the API
// reports it (see GET /router/api/v1/user/devices).
type Capability struct {
	Type       string           `json:"type"`
	Instance   string           `json:"instance"`
	AlarmType  int              `json:"alarmType,omitempty"`
	Parameters json.RawMessage  `json:"parameters"`
	EventState *StateDescriptor `json:"eventState,omitempty"`
}

// CapabilityOption is a named choice a capability offers (e.g. on/off, a
// scene, a mode).
type CapabilityOption struct {
	Name    string             `json:"name"`
	Value   any                `json:"value"`
	Options []CapabilityOption `json:"options,omitempty"`
	// Message is present on event options (e.g. "Lack of Water").
	Message string `json:"message,omitempty"`
}

// ValueString renders the option value as a string, descending into
// sub-options when the value is nested (e.g. scene {paramId, id} objects
// render as their id).
func (o CapabilityOption) ValueString() string {
	if f, ok := num(o.Value); ok {
		return fmt.Sprintf("%g", f)
	}
	if s, ok := o.Value.(string); ok {
		return s
	}
	if len(o.Options) > 0 {
		return o.Options[0].ValueString()
	}
	return ""
}

// CapabilityParameters are the parsed parameters of a capability. Not all
// fields are meaningful for all capabilities; the decoder fills what the
// server sent. For STRUCT capabilities, Fields describes the struct; for
// RANGE capabilities, Range/Precision apply.
type CapabilityParameters struct {
	DataType     string             `json:"dataType"`
	Unit         string             `json:"unit"`
	Range        *ValueRange        `json:"range,omitempty"`
	ElementRange *ValueRange        `json:"elementRange,omitempty"`
	ElementType  string             `json:"elementType,omitempty"`
	Size         *ValueRange        `json:"size,omitempty"`
	Options      []CapabilityOption `json:"options"`
	Fields       []ParameterField   `json:"fields"`
	// Raw keeps the unparsed JSON for forward compatibility.
	Raw json.RawMessage `json:"-"`
}

// ValueRange is a numeric range with optional precision/step.
type ValueRange struct {
	Min       float64 `json:"min"`
	Max       float64 `json:"max"`
	Precision float64 `json:"precision"`
}

// ParameterField describes one field of a STRUCT-typed capability parameter.
type ParameterField struct {
	FieldName    string             `json:"fieldName"`
	DataType     string             `json:"dataType"`
	Required     bool               `json:"required"`
	Options      []CapabilityOption `json:"options"`
	Range        *ValueRange        `json:"range,omitempty"`
	ElementRange *ValueRange        `json:"elementRange,omitempty"`
	Size         *ValueRange        `json:"size,omitempty"`
	DefaultValue any                `json:"defaultValue,omitempty"`
}

// StateDescriptor describes a reported event/property state (used for
// events, which have alarmType + eventState).
type StateDescriptor struct {
	Options []CapabilityOption `json:"options"`
}

// Supports reports whether the device advertises a capability of the given
// type (e.g. CapOnOff), optionally filtering by instance.
func (d *Device) Supports(typeName, instance string) bool {
	_, ok := d.Capability(typeName, instance)
	return ok
}

// Capability returns the device's capability of the given type/instance.
func (d *Device) Capability(typeName, instance string) (Capability, bool) {
	for _, c := range d.Capabilities {
		if c.Type == typeName && (instance == "" || c.Instance == instance) {
			return c, true
		}
	}
	return Capability{}, false
}

// Range returns the numeric range (min/max/precision) of a range-typed
// capability (e.g. brightness) on this device. The second return value is
// false when the device has no such capability or it has no range.
func (d *Device) Range(typeName, instance string) (ValueRange, bool) {
	c, ok := d.Capability(typeName, instance)
	if !ok {
		return ValueRange{}, false
	}
	var p CapabilityParameters
	if err := json.Unmarshal(c.Parameters, &p); err != nil || p.Range == nil {
		return ValueRange{}, false
	}
	return *p.Range, true
}

// Options returns the enum options of a capability (e.g. scenes, modes,
// toggles, on/off). The second return value is false when the capability
// does not exist or has no options.
func (d *Device) Options(typeName, instance string) ([]CapabilityOption, bool) {
	c, ok := d.Capability(typeName, instance)
	if !ok {
		return nil, false
	}
	var p CapabilityParameters
	if err := json.Unmarshal(c.Parameters, &p); err != nil || len(p.Options) == 0 {
		return nil, false
	}
	return p.Options, true
}

// IsOnlineInCapabilities reports whether the device advertises an "online"
// capability (most devices do).
func (d *Device) IsOnlineInCapabilities() bool {
	return d.Supports(CapOnline, InstanceOnline)
}

// UnmarshalCapabilityParameters parses a capability's parameters into a
// typed struct.
func UnmarshalCapabilityParameters(c Capability) (CapabilityParameters, error) {
	var p CapabilityParameters
	if len(c.Parameters) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(c.Parameters, &p); err != nil {
		return p, err
	}
	p.Raw = c.Parameters
	return p, nil
}

// num reports whether v is a JSON number (float64 from decoding) and
// returns it.
func num(v any) (float64, bool) {
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
