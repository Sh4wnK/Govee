package Govee

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
)

func (g *Govee) getDevices() error {
	request, _ := http.NewRequest("GET", "https://openapi.api.govee.com/router/api/v1/user/devices", nil)
	request.Header.Add("Govee-API-Key", g.apikey)
	response, err := g.client.Do(request)
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

	var result map[string]interface{}
	json.Unmarshal(rawBytes, &result)
	for _, device := range result["data"].([]interface{}) {
		Capabilities := []Capability{}
		var DeviceSku = device.(map[string]interface{})["sku"].(string)
		var DeviceName = device.(map[string]interface{})["device"].(string)
		var DeviceType = device.(map[string]interface{})["type"].(string)
		for _, capability := range device.(map[string]interface{})["capabilities"].([]interface{}) {
			var CapabilityType = capability.(map[string]interface{})["type"].(string)
			var CapabilityInstance = capability.(map[string]interface{})["instance"].(string)
			var CapabilityParameters = capability.(map[string]interface{})["parameters"].(interface{})
			Capabilities = append(Capabilities, Capability{CapabilityType, CapabilityInstance, CapabilityParameters})
		}
		g.devices = append(g.devices, Device{g, DeviceSku, DeviceName, DeviceType, Capabilities, DeviceInfoStruct{}})
	}
	return nil

}
func (g *Govee) getDeviceStatus(device *Device) error {
	request, _ := http.NewRequest("GET", "https://developer-api.govee.com/v1/devices/state?device="+device.DeviceID+"&model="+device.DeviceSku, nil)
	request.Header.Add("Govee-API-Key", g.apikey)
	response, err := g.client.Do(request)
	if err != nil {
		return err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			return
		}
	}(response.Body)
	rawBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	json.Unmarshal(rawBytes, &result)
	Brightness := result["data"].(map[string]interface{})["properties"].([]interface{})[2].(map[string]interface{})["brightness"].(float64)
	PowerState := result["data"].(map[string]interface{})["properties"].([]interface{})[1].(map[string]interface{})["powerState"].(string)
	ColorVals := result["data"].(map[string]interface{})["properties"].([]interface{})[3].(map[string]interface{})["color"].(map[string]interface{})
	ColorBGR := BGR{int(ColorVals["b"].(float64)), int(ColorVals["g"].(float64)), int(ColorVals["r"].(float64))}
	DeviceInfo := DeviceInfoStruct{PowerState == "on", int(Brightness), ColorBGR}
	device.DeviceInfo = DeviceInfo
	return nil
}

func (g *Govee) initAllDevices() {
	for i := range g.devices {
		err := g.getDeviceStatus(&g.devices[i])
		if err != nil {
			continue
		}

	}

}

func (g *Govee) GetDevices() []Device {
	return g.devices
}
