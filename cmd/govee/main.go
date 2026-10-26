// govee is a command-line client for the Govee Developer Platform and LAN
// APIs. It lets developers manage devices programmatically without the app:
//
//	govee devices                 list devices and their capabilities
//	govee state <sku> <device>    query device state
//	govee power <sku> <device> on|off
//	govee brightness <sku> <device> <0-100>
//	govee color <sku> <device> <R,G,B>
//	govee color-temp <sku> <device> <kelvin>
//	govee scenes <sku> <device>   list dynamic scenes
//	govee set-scene <sku> <device> <scene>   apply a scene
//	govee lan scan                discover LAN devices
//
// The API key is read from the GOVEE_API_KEY environment variable.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/Sh4wnK/Govee"
	"github.com/Sh4wnK/Govee/lan"
)

func main() {
	args := os.Args[1:]
	if len(args) < 1 {
		usage()
		return
	}
	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "lan":
		lanCmd(rest)
	case "devices", "state", "power", "brightness", "color", "color-temp",
		"scenes", "set-scene", "toggle":
		cloudCmd(cmd, rest)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "govee: unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Print(`usage: govee <command> [args]

commands:
  devices                        list devices and their capabilities
  state <sku> <device>           query device state
  power <sku> <device> on|off    set device power
  brightness <sku> <device> <pct>  set brightness (1-100)
  color <sku> <device> <r,g,b>   set color (0-255 each)
  color-temp <sku> <device> <K>  set color temperature in Kelvin
  scenes <sku> <device>          list dynamic scenes
  set-scene <sku> <device> <name>  apply a scene by name
  toggle <sku> <device> <instance> on|off  toggle a toggle capability
  lan scan                       discover LAN devices

environment:
  GOVEE_API_KEY      API key from the Govee Home app (required for cloud commands)
  GOVEE_BASE_URL     optional API base URL override (e.g. the test environment)
`)
}

func apiKey() string {
	if k := os.Getenv("GOVEE_API_KEY"); k != "" {
		return k
	}
	fmt.Fprintln(os.Stderr, "govee: GOVEE_API_KEY is not set (get one from the Govee Home app)")
	os.Exit(2)
	return ""
}

func newClient(key string) *govee.Client {
	opts := []govee.Option{govee.WithHTTPClient(&http.Client{})}
	if base := os.Getenv("GOVEE_BASE_URL"); base != "" {
		opts = append(opts, govee.WithBaseURL(base))
	}
	return govee.NewClient(key, opts...)
}

func cloudCmd(cmd string, rest []string) {
	c := newClient(apiKey())
	ctx := context.Background()

	switch cmd {
	case "devices":
		devices, err := c.Devices(ctx)
		if err != nil {
			fail(err)
		}
		for _, d := range devices {
			fmt.Printf("%s\t%s\t%s\t%s\n", d.SKU, d.ID, d.Name, d.Type)
			for _, cap := range d.Capabilities {
				fmt.Printf("    %s / %s\n", cap.Type, cap.Instance)
			}
		}
		fmt.Printf("%d device(s)\n", len(devices))
	case "state":
		need(rest, 2)
		st, err := c.State(ctx, rest[0], rest[1])
		if err != nil {
			fail(err)
		}
		fmt.Printf("sku:     %s\n", st.SKU)
		fmt.Printf("device:  %s\n", st.Device)
		if st.DeviceName != "" {
			fmt.Printf("name:    %s\n", st.DeviceName)
		}
		for _, cap := range st.Capabilities {
			fmt.Printf("%s / %s = %v\n", cap.Type, cap.Instance, cap.State.Value)
		}
	case "power":
		need(rest, 3)
		on, err := parseBool(rest[2])
		if err != nil {
			fail(err)
		}
		if err := c.SetPower(ctx, rest[0], rest[1], on); err != nil {
			fail(err)
		}
		fmt.Printf("power %s on %s\n", boolWord(on), rest[1])
	case "brightness":
		need(rest, 3)
		pct, err := strconv.Atoi(rest[2])
		if err != nil {
			fail(fmt.Errorf("brightness must be an integer: %v", err))
		}
		if err := c.SetBrightness(ctx, rest[0], rest[1], pct); err != nil {
			fail(err)
		}
		fmt.Printf("brightness set to %d%% on %s\n", pct, rest[1])
	case "color":
		need(rest, 3)
		col, err := parseColor(rest[2])
		if err != nil {
			fail(err)
		}
		if err := c.SetColorRGB(ctx, rest[0], rest[1], col); err != nil {
			fail(err)
		}
		fmt.Printf("color set on %s\n", rest[1])
	case "color-temp":
		need(rest, 3)
		k, err := strconv.Atoi(rest[2])
		if err != nil {
			fail(fmt.Errorf("kelvin must be an integer: %v", err))
		}
		if err := c.SetColorTemperature(ctx, rest[0], rest[1], k); err != nil {
			fail(err)
		}
		fmt.Printf("color temperature set to %d K on %s\n", k, rest[1])
	case "scenes":
		need(rest, 2)
		scenes, err := c.ListScenes(ctx, rest[0], rest[1])
		if err != nil {
			fail(err)
		}
		for _, s := range scenes {
			fmt.Printf("%d\t%s\n", s.ID, s.Name)
		}
	case "set-scene":
		need(rest, 3)
		if err := c.SetScene(ctx, rest[0], rest[1], rest[2]); err != nil {
			fail(err)
		}
		fmt.Printf("scene %q applied to %s\n", rest[2], rest[1])
	case "toggle":
		need(rest, 4)
		on, err := parseBool(rest[3])
		if err != nil {
			fail(err)
		}
		if err := c.Toggle(ctx, rest[0], rest[1], rest[2], on); err != nil {
			fail(err)
		}
		fmt.Printf("%s %s on %s\n", rest[2], boolWord(on), rest[1])
	}
}

func lanCmd(rest []string) {
	if len(rest) == 0 || rest[0] != "scan" {
		fmt.Fprintln(os.Stderr, "usage: govee lan scan")
		os.Exit(2)
	}
	devices, err := lan.Scan(context.Background())
	if err != nil {
		fail(err)
	}
	for _, d := range devices {
		fmt.Printf("%s\t%s\t%s\t%s\n", d.IP, d.SKU, d.MAC, d.Name)
		for _, cap := range d.Capabilities {
			fmt.Printf("    %s\n", cap)
		}
	}
	if len(devices) == 0 {
		fmt.Println("no Govee devices found on the LAN")
	}
}

func need(args []string, n int) {
	if len(args) < n {
		fmt.Fprintf(os.Stderr, "govee: expected %d arguments, got %d\n", n, len(args))
		os.Exit(2)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "govee: %v\n", err)
	os.Exit(1)
}

func parseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "on", "true", "1":
		return true, nil
	case "off", "false", "0":
		return false, nil
	}
	return false, fmt.Errorf("expected on/off, got %q", s)
}

func boolWord(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func parseColor(s string) (govee.Color, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return govee.Color{}, fmt.Errorf("color must be r,g,b")
	}
	var out govee.Color
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 || n > 255 {
			return govee.Color{}, fmt.Errorf("color channel must be 0-255: %q", p)
		}
		switch i {
		case 0:
			out.R = uint8(n)
		case 1:
			out.G = uint8(n)
		case 2:
			out.B = uint8(n)
		}
	}
	return out, nil
}
