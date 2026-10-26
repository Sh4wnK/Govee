// Package lan implements Govee's LAN control API: UDP multicast discovery,
// status queries, and direct device commands over the local network.
//
// Govee devices listen for discovery on 239.255.255.250:4001 and reply on
// port 4002, then accept commands on port 4003. This package mirrors the
// client API of the parent govee package (no API key required) for
// low-latency, LAN-only control of devices on the same network.
package lan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// Well-known Govee LAN addresses and ports.
const (
	// MulticastGroup is the discovery group address.
	MulticastGroup = "239.255.255.250"
	// ResponsePort is the local port devices send discovery replies to.
	ResponsePort = 4002
	// CommandPort is where devices accept commands and status requests.
	CommandPort = 4003
)

// MulticastAddr is where devices listen for discovery probes.
var MulticastAddr = net.JoinHostPort(MulticastGroup, "4001")

// DefaultDiscoveryTimeout is how long a scan collects replies after
// sending the probe.
const DefaultDiscoveryTimeout = 3 * time.Second

// ErrNoResponse is returned when a device does not reply in time.
var ErrNoResponse = errors.New("lan: device did not respond")

// Device is a Govee device discovered on the LAN.
type Device struct {
	// IP is the device's unicast address (for commands).
	IP string
	// MAC is the device's MAC address as reported by the device.
	MAC string
	// SKU is the product model, when reported.
	SKU string
	// Name is the device name reported by the device.
	Name string
	// Capabilities are the command options advertised via LAN messages
	// (e.g. "turn", "brightness", "colorwc", "color" for lights).
	Capabilities []string
}

// Supports reports whether the discovered device advertises the given LAN
// command capability (case-insensitive).
func (d *Device) Supports(cap string) bool {
	for _, c := range d.Capabilities {
		if strings.EqualFold(strings.TrimSpace(c), cap) {
			return true
		}
	}
	return false
}

// Scanner discovers Govee devices on the local network. The zero value
// works with production defaults; the fields are injectable for tests.
type Scanner struct {
	// MulticastAddr is where the scan probe is sent
	// (default: 239.255.255.250:4001).
	MulticastAddr string
	// Conn is the packet socket used to send the probe and read replies.
	// When nil, a UDP socket bound to ResponsePort (4002) is opened for
	// the duration of the scan, which is where devices send replies.
	Conn net.PacketConn
	// Timeout bounds the reply-collection window
	// (default: DefaultDiscoveryTimeout).
	Timeout time.Duration
}

// Scan performs discovery and returns the devices that replied. A non-nil
// ctx cancels the scan early.
func (s *Scanner) Scan(ctx context.Context) ([]Device, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultDiscoveryTimeout
	}
	dest := s.MulticastAddr
	if dest == "" {
		dest = MulticastAddr
	}

	conn := s.Conn
	if conn == nil {
		var err error
		conn, err = listenResponse()
		if err != nil {
			return nil, fmt.Errorf("lan: bind response socket: %w", err)
		}
		defer conn.Close()
	}

	probe := []byte(`{"msg":{"cmd":"scan","data":{}}}`)
	raddr, err := net.ResolveUDPAddr("udp", dest)
	if err != nil {
		return nil, fmt.Errorf("lan: resolve %q: %w", dest, err)
	}
	if _, err := conn.WriteTo(probe, raddr); err != nil {
		return nil, fmt.Errorf("lan: send scan probe: %w", err)
	}

	var out []Device
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 4096)
	for {
		n, src, err := readPacket(ctx, conn, buf, deadline)
		if err != nil {
			if isTimeout(err) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				break
			}
			return out, fmt.Errorf("lan: read scan reply: %w", err)
		}
		if d := parseDeviceReply(buf[:n], src); d != nil {
			out = append(out, *d)
		}
	}
	return out, nil
}

// Scan discovers devices using production defaults.
func Scan(ctx context.Context) ([]Device, error) {
	return (&Scanner{}).Scan(ctx)
}

// listenResponse opens a UDP socket bound to the response port so devices
// know where to reply. Falls back to an ephemeral port when 4002 is taken
// (e.g. concurrent scans from the same host; devices that reply to the
// packet's source address still work).
func listenResponse() (*net.UDPConn, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: ResponsePort})
	if err == nil {
		return conn, nil
	}
	return net.ListenUDP("udp", nil)
}

// readPacket reads one datagram from conn into buf, honoring the deadline
// and ctx. It works for any net.PacketConn: deadlines are set when the
// conn supports them, otherwise a goroutine plus select is used.
func readPacket(ctx context.Context, conn net.PacketConn, buf []byte, deadline time.Time) (int, net.Addr, error) {
	if sd, ok := conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		if err := sd.SetReadDeadline(deadline); err != nil {
			return 0, nil, err
		}
		return conn.ReadFrom(buf)
	}
	type res struct {
		n   int
		adr net.Addr
		err error
	}
	ch := make(chan res, 1)
	go func() {
		n, adr, err := conn.ReadFrom(buf)
		ch <- res{n, adr, err}
	}()
	var timer *time.Timer
	var tc <-chan time.Time
	if d := time.Until(deadline); d > 0 {
		timer = time.NewTimer(d)
		tc = timer.C
	} else {
		tc = time.After(0)
	}
	if timer != nil {
		defer timer.Stop()
	}
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case <-tc:
		return 0, nil, timeoutErr{}
	case r := <-ch:
		return r.n, r.adr, r.err
	}
}

// timeoutErr is a net.Error-compatible timeout used when the conn cannot
// set deadlines.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// isTimeout reports whether err is a timeout.
func isTimeout(err error) bool {
	ne, ok := err.(net.Error)
	return ok && ne.Timeout()
}

// parseDeviceReply extracts a Device from a discovery reply. A reply looks
// like:
//
//	{"msg":{"cmd":"scan","data":{"ip":"192.168.1.50","sku":"H6003",
//	"device":"AA:BB:...","deviceName":"Living Room",
//	"capabilities":["turn","brightness","colorwc","color"]}}}
//
// Unknown or malformed payloads return nil so scans tolerate network noise.
func parseDeviceReply(b []byte, src net.Addr) *Device {
	var m struct {
		Msg struct {
			Cmd  string `json:"cmd"`
			Data struct {
				IP           string   `json:"ip"`
				SKU          string   `json:"sku"`
				Device       string   `json:"device"`
				DeviceName   string   `json:"deviceName"`
				Capabilities []string `json:"capabilities"`
			} `json:"data"`
		} `json:"msg"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	if m.Msg.Cmd != "scan" {
		return nil
	}
	d := &Device{
		IP:           m.Msg.Data.IP,
		SKU:          m.Msg.Data.SKU,
		MAC:          m.Msg.Data.Device,
		Name:         m.Msg.Data.DeviceName,
		Capabilities: m.Msg.Data.Capabilities,
	}
	if d.IP == "" && src != nil {
		if ua, ok := src.(*net.UDPAddr); ok {
			d.IP = ua.IP.String()
		}
	}
	if d.IP == "" {
		return nil
	}
	d.MAC = strings.ToUpper(strings.TrimSpace(d.MAC))
	d.SKU = strings.TrimSpace(d.SKU)
	return d
}
