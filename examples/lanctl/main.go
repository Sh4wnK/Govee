// Command lanctl shows LAN discovery and direct device control without any
// API key or cloud round trip:
//
//	go run ./examples/lanctl scan
//	go run ./examples/lanctl on 192.168.1.50
//	go run ./examples/lanctl color 192.168.1.50 255 0 0
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/Sh4wnK/Govee/lan"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Println(`usage:
  lanctl scan
  lanctl on|off <ip>
  lanctl brightness <ip> <percent>
  lanctl color <ip> <r> <g> <b>
  lanctl status <ip>`)
		os.Exit(2)
	}
	ctx := context.Background()
	switch args[0] {
	case "scan":
		devices, err := lan.Scan(ctx)
		if err != nil {
			log.Fatalf("scan: %v", err)
		}
		for _, d := range devices {
			fmt.Printf("%-15s %-6s %-18s %-20s %v\n", d.IP, d.SKU, d.MAC, d.Name, d.Capabilities)
		}
		if len(devices) == 0 {
			fmt.Println("no Govee devices found on the LAN")
		}
	case "on", "off":
		need(args, 2)
		c := lan.NewClient(args[1])
		if err := c.Turn(ctx, args[0] == "on"); err != nil {
			log.Fatalf("turn: %v", err)
		}
		fmt.Printf("device %s turned %s\n", args[1], args[0])
	case "brightness":
		need(args, 3)
		pct, err := strconv.Atoi(args[2])
		if err != nil {
			log.Fatalf("percent must be an integer: %v", err)
		}
		c := lan.NewClient(args[1])
		if err := c.Brightness(ctx, pct); err != nil {
			log.Fatalf("brightness: %v", err)
		}
		fmt.Printf("device %s brightness set to %d\n", args[1], pct)
	case "color":
		need(args, 5)
		r := atoi("r", args[2])
		g := atoi("g", args[3])
		b := atoi("b", args[4])
		c := lan.NewClient(args[1])
		if err := c.Color(ctx, uint8(r), uint8(g), uint8(b)); err != nil {
			log.Fatalf("color: %v", err)
		}
		fmt.Printf("device %s color set to rgb(%d,%d,%d)\n", args[1], r, g, b)
	case "status", "devstatus":
		need(args, 2)
		c := lan.NewClient(args[1])
		st, err := c.DevStatus(ctx)
		if err != nil {
			log.Fatalf("devStatus: %v", err)
		}
		for k, v := range st {
			fmt.Printf("%s: %v\n", k, v)
		}
	default:
		log.Fatalf("unknown command %q", args[0])
	}
}

func need(args []string, n int) {
	if len(args) < n {
		fmt.Fprintf(os.Stderr, "lanctl: expected %d arguments\n", n)
		os.Exit(2)
	}
}

func atoi(name, s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		log.Fatalf("%s must be an integer: %v", name, err)
	}
	return n
}
