package Govee

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"strings"
)

type DeviceCommnd struct {
	RequestID string  `json:"requestId"`
	Payload   Payload `json:"payload"`
}

type Payload struct {
	SKU        string      `json:"sku"`
	Device     string      `json:"device"`
	Capability interface{} `json:"capability"`
}

type SingleValuePayload struct {
	Type     string `json:"type"`
	Instance string `json:"instance"`
	Value    int    `json:"value"`
}

type Capability struct {
	Type       string
	Instance   string
	Parameters interface{}
}
type BGR struct {
	B int
	G int
	R int
}

type DeviceInfoStruct struct {
	Powerstate bool
	Brightness int
	Color      BGR
}

type Device struct {
	controller   *Govee
	DeviceSku    string
	DeviceID     string
	Type         string
	Capabilities []Capability
	DeviceInfo   DeviceInfoStruct
}

func (d *Device) TogglePower() error {
	deviceCommand := DeviceCommnd{
		RequestID: uuid.New().String(),
		Payload: Payload{
			SKU:    d.DeviceSku,
			Device: d.DeviceID,
			Capability: SingleValuePayload{
				Type:     "devices.capabilities.on_off",
				Instance: "powerSwitch",
				Value:    1 - map[bool]int{true: 1, false: 0}[d.DeviceInfo.Powerstate],
			},
		},
	}
	jsonData, err := json.Marshal(deviceCommand)
	if err != nil {
		fmt.Println("Error marshaling JSON:", err)
		return err
	}

	request, _ := http.NewRequest("POST", "https://openapi.api.govee.com/router/api/v1/device/control", bytes.NewReader(jsonData))
	request.Header.Add("Govee-API-Key", d.controller.apikey)
	request.Header.Add("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Govee (Go Wrapper V1.0)")
	response, err := d.controller.client.Do(request)
	if err != nil {
		return err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			return
		}
	}(response.Body)

	rawBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if strings.Contains(string(rawBytes), "{\"status\":\"success\"}") {
		d.DeviceInfo.Powerstate = !d.DeviceInfo.Powerstate
		return nil
	}

	return errors.New(string(rawBytes))

}

func (d *Device) ChangeColor(bgr BGR) error {
	deviceCommand := DeviceCommnd{
		RequestID: uuid.New().String(),
		Payload: Payload{
			SKU:    d.DeviceSku,
			Device: d.DeviceID,
			Capability: SingleValuePayload{
				Type:     "devices.capabilities.color_setting",
				Instance: "colorRgb",
				Value:    ((bgr.R & 0xFF) << 16) | ((bgr.G & 0xFF) << 8) | ((bgr.B & 0xFF) << 0),
			},
		},
	}
	jsonData, err := json.Marshal(deviceCommand)
	if err != nil {
		fmt.Println("Error marshaling JSON:", err)
		return err
	}

	request, _ := http.NewRequest("POST", "https://openapi.api.govee.com/router/api/v1/device/control", bytes.NewReader(jsonData))
	request.Header.Add("Govee-API-Key", d.controller.apikey)
	request.Header.Add("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Govee (Go Wrapper V1.0)")
	response, err := d.controller.client.Do(request)
	if err != nil {
		return err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			return
		}
	}(response.Body)

	rawBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if strings.Contains(string(rawBytes), "{\"status\":\"success\"}") {
		d.DeviceInfo.Color = bgr
		return nil
	}

	return errors.New(string(rawBytes))

}
