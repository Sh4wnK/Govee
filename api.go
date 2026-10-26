package govee

import (
	"context"
	"encoding/json"
)

// requestEnvelope wraps every API request body. requestId is echoed back by
// the API in both success and error responses.
type requestEnvelope struct {
	RequestID string `json:"requestId"`
	Payload   any    `json:"payload"`
}

// deviceRef identifies a device by SKU and device ID (MAC).
type deviceRef struct {
	SKU    string `json:"sku"`
	Device string `json:"device"`
}

// controlPayload is the body of POST /router/api/v1/device/control.
type controlPayload struct {
	deviceRef
	Capability capabilityCommand `json:"capability"`
}

// capabilityCommand is a single typed command sent to a device. Value is the
// JSON-encodable value of the capability (int, string, struct, array...).
type capabilityCommand struct {
	Type     string `json:"type"`
	Instance string `json:"instance"`
	Value    any    `json:"value"`
}

// CapabilityCommand is a typed command for a device capability
// (see the typed constructors OnCommand, BrightnessCommand, ...).
type CapabilityCommand = capabilityCommand

// sceneValue is the value object of a dynamic scene option
// ({paramId, id} as documented for GET /router/api/v1/device/scenes).
type sceneValue struct {
	ParamID int `json:"paramId"`
	ID      int `json:"id"`
}

// Scene is a dynamic light scene as returned by ListScenes.
type Scene struct {
	Name    string `json:"name"`
	ParamID int    `json:"paramId"`
	ID      int    `json:"id"`
}

// scenesPayload is the body of POST /router/api/v1/device/scenes and
// POST /router/api/v1/device/diy-scenes.
type scenesPayload struct {
	deviceRef
}

// scenesResponse is the response of the scenes endpoints.
type scenesResponse struct {
	RequestID string `json:"requestId"`
	Msg       string `json:"msg"`
	Code      int    `json:"code"`
	Payload   struct {
		SKU          string       `json:"sku"`
		Device       string       `json:"device"`
		Capabilities []Capability `json:"capabilities"`
	} `json:"payload"`
}

// ControlRequest is the documented JSON body of POST /router/api/v1/device/control.
type ControlRequest struct {
	RequestID string         `json:"requestId"`
	Payload   ControlPayload `json:"payload"`
}

// ControlPayload is the payload of a ControlRequest.
type ControlPayload struct {
	SKU        string            `json:"sku"`
	Device     string            `json:"device"`
	Capability CapabilityCommand `json:"capability"`
}

// StateRequest is the documented JSON body of POST /router/api/v1/device/state.
type StateRequest struct {
	RequestID string       `json:"requestId"`
	Payload   StatePayload `json:"payload"`
}

// StatePayload identifies a device for a state query.
type StatePayload struct {
	SKU    string `json:"sku"`
	Device string `json:"device"`
}

// StateResponse is the JSON response of POST /router/api/v1/device/state.
type StateResponse struct {
	RequestID string `json:"requestId"`
	Msg       string `json:"msg"`
	Code      int    `json:"code"`
	Payload   State  `json:"payload"`
}

// State is the typed state of a device returned by State.
type State struct {
	SKU          string            `json:"sku"`
	Device       string            `json:"device"`
	DeviceName   string            `json:"deviceName"`
	Capabilities []StateCapability `json:"capabilities"`
}

// StateCapability is one capability value of a state report. Value holds the
// parsed value: bool for online, float64 for numeric ranges, string for
// enums, map[string]any for structs (work mode, temperature settings).
type StateCapability struct {
	Type     string `json:"type"`
	Instance string `json:"instance"`
	State    struct {
		Value any `json:"value"`
	} `json:"state"`
}

// StateValue returns the parsed state value of a capability instance.
func (s *State) StateValue(instance string) (any, bool) {
	for _, c := range s.Capabilities {
		if c.Instance == instance {
			return c.State.Value, c.State.Value != nil
		}
	}
	return nil, false
}

// Online reports whether the device state says it is online, when the
// device reports the online capability.
func (s *State) Online() (bool, bool) {
	v, ok := s.StateValue(InstanceOnline)
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// Bool returns the state value of instance as a bool. The API reports
// on_off and toggle states as 0/1 numbers and online as true/false, so both
// shapes are accepted. ok is false when unavailable.
func (s *State) Bool(instance string) (bool, bool) {
	v, ok := s.StateValue(instance)
	if !ok {
		return false, false
	}
	switch b := v.(type) {
	case bool:
		return b, true
	case float64:
		return b != 0, true
	case int:
		return b != 0, true
	}
	return false, false
}

// Number returns the state value of instance as a float64 (for ranges,
// RGB, temperature...). ok is false when unavailable.
func (s *State) Number(instance string) (float64, bool) {
	v, ok := s.StateValue(instance)
	if !ok {
		return 0, false
	}
	f, ok := v.(float64)
	return f, ok
}

// String returns the state value of instance as a string. ok is false when
// unavailable.
func (s *State) String(instance string) (string, bool) {
	v, ok := s.StateValue(instance)
	if !ok {
		return "", false
	}
	str, ok := v.(string)
	return str, ok
}

// devicesResponse is the response of GET /router/api/v1/user/devices.
type devicesResponse struct {
	Code    int      `json:"code"`
	Message string   `json:"message"`
	Msg     string   `json:"msg"`
	Data    []Device `json:"data"`
}

// Devices fetches all devices and their advertised capabilities for the
// account: GET /router/api/v1/user/devices. The returned slice is a fresh
// copy; mutating it does not affect the client.
func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	var resp devicesResponse
	if err := c.do(ctx, "GET", pathDevices, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// Control sends a single capability command to a device:
// POST /router/api/v1/device/control.
func (c *Client) Control(ctx context.Context, sku, deviceID string, cmd CapabilityCommand) error {
	body := requestEnvelope{
		RequestID: newRequestID(),
		Payload: controlPayload{
			deviceRef:  deviceRef{SKU: sku, Device: deviceID},
			Capability: cmd,
		},
	}
	resp := struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}{}
	return c.do(ctx, "POST", pathControl, body, &resp)
}

// State queries the current state of a device:
// POST /router/api/v1/device/state.
func (c *Client) State(ctx context.Context, sku, deviceID string) (*State, error) {
	body := requestEnvelope{
		RequestID: newRequestID(),
		Payload: deviceRef{
			SKU:    sku,
			Device: deviceID,
		},
	}
	var resp StateResponse
	if err := c.do(ctx, "POST", pathState, body, &resp); err != nil {
		return nil, err
	}
	return &resp.Payload, nil
}

// ListScenes queries the dynamic light scenes of a device:
// POST /router/api/v1/device/scenes.
func (c *Client) ListScenes(ctx context.Context, sku, deviceID string) ([]Scene, error) {
	return c.listScenes(ctx, sku, deviceID, pathScenes)
}

// ListDIYScenes queries the DIY light scenes of a device:
// POST /router/api/v1/device/diy-scenes.
func (c *Client) ListDIYScenes(ctx context.Context, sku, deviceID string) ([]Scene, error) {
	return c.listScenes(ctx, sku, deviceID, pathDIYScenes)
}

func (c *Client) listScenes(ctx context.Context, sku, deviceID, path string) ([]Scene, error) {
	body := requestEnvelope{
		RequestID: newRequestID(),
		Payload: scenesPayload{
			deviceRef: deviceRef{SKU: sku, Device: deviceID},
		},
	}
	var resp scenesResponse
	if err := c.do(ctx, "POST", path, body, &resp); err != nil {
		return nil, err
	}
	var scenes []Scene
	for _, cap := range resp.Payload.Capabilities {
		if cap.Type != CapDynamicScene {
			continue
		}
		var p CapabilityParameters
		if err := json.Unmarshal(cap.Parameters, &p); err != nil {
			continue
		}
		for _, opt := range p.Options {
			b, err := json.Marshal(opt.Value)
			if err != nil {
				continue
			}
			var sv sceneValue
			if err := json.Unmarshal(b, &sv); err == nil && (sv.ParamID != 0 || sv.ID != 0) {
				scenes = append(scenes, Scene{Name: opt.Name, ParamID: sv.ParamID, ID: sv.ID})
				continue
			}
			// Some scene options carry a plain integer value.
			var iv int
			if json.Unmarshal(b, &iv) == nil {
				scenes = append(scenes, Scene{Name: opt.Name, ID: iv})
			}
		}
	}
	return scenes, nil
}

// newRequestID returns a fresh RFC 4122 v4 UUID used as requestId.
func newRequestID() string {
	return newUUIDString()
}
