// Command basic shows the core cloud API: listing devices, querying state,
// and sending typed commands. Run it with an API key from the Govee Home
// app:
//
//	GOVEE_API_KEY=xxxx go run ./examples/basic
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	govee "github.com/Sh4wnK/Govee"
)

func main() {
	key := os.Getenv("GOVEE_API_KEY")
	if key == "" {
		log.Fatal("GOVEE_API_KEY is not set")
	}
	ctx := context.Background()
	c := govee.NewClient(key)

	devices, err := c.Devices(ctx)
	if err != nil {
		log.Fatalf("list devices: %v", err)
	}
	fmt.Printf("found %d device(s):\n", len(devices))
	for _, d := range devices {
		fmt.Printf("  %-5s %-20s %-24s %s\n", d.SKU, d.Name, d.ID, d.Type)
	}
	if len(devices) == 0 {
		fmt.Println("nothing to control; add a device to your Govee account first")
		return
	}

	d := devices[0]
	fmt.Printf("\ncontrolling %s (%s %s)\n", d.Name, d.SKU, d.ID)

	// Query device state.
	st, err := c.State(ctx, d.SKU, d.ID)
	if err != nil {
		log.Fatalf("query state: %v", err)
	}
	if on, ok := st.Bool(govee.InstanceOnline); ok {
		fmt.Printf("online: %v\n", on)
	}
	if b, ok := st.Number(govee.InstanceBrightness); ok {
		fmt.Printf("brightness: %v\n", b)
	}

	// Typed commands. Validation helpers keep out-of-range values from
	// reaching the API.
	if d.Supports(govee.CapOnOff, govee.InstancePowerSwitch) {
		if err := c.SetPower(ctx, d.SKU, d.ID, true); err != nil {
			log.Fatalf("power on: %v", err)
		}
		fmt.Println("powered on")
	}
	if r, ok := d.Range(govee.CapRange, govee.InstanceBrightness); ok {
		fmt.Printf("brightness range advertised: [%v..%v]\n", r.Min, r.Max)
		if err := c.SetBrightness(ctx, d.SKU, d.ID, 50); err != nil {
			log.Fatalf("set brightness: %v", err)
		}
		fmt.Println("brightness set to 50%")
	}
	if d.Supports(govee.CapColorSetting, govee.InstanceColorRgb) {
		if err := c.SetColorRGB(ctx, d.SKU, d.ID, govee.Color{R: 255, G: 128, B: 0}); err != nil {
			log.Fatalf("set color: %v", err)
		}
		fmt.Println("color set to orange")
	}
}

// InstanceOnline etc. are unexported constants in the library; define local
// aliases so this example compiles against the public API only.
