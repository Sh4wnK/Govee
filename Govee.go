package Govee

import (
	"net/http"
	"time"
)

type Govee struct {
	client  http.Client
	apikey  string
	devices []Device
}

func New(apiKey string) *Govee {
	govee := Govee{apikey: apiKey}
	govee.client = http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	err := govee.getDevices()
	if err != nil {
		panic(err)
		return nil
	}
	govee.initAllDevices()
	return &govee
}
