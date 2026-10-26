package lan

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// LAN command names understood by Govee devices (sent to port 4003).
const (
	CmdTurn       = "turn"
	CmdBrightness = "brightness"
	CmdColorWC    = "colorwc" // white + color temperature (Kelvin)
	CmdColor      = "color"
	CmdSetColor   = "setColor"
	CmdDevStatus  = "devStatus"
	CmdScene      = "scene"
	CmdMusicMode  = "musicMode"
)

// Client talks to a single Govee device over the LAN on port 4003.
// It is safe for concurrent use: every command is an independent UDP
// round trip on its own socket.
type Client struct {
	// Addr is the device's unicast "ip:4003" address.
	Addr string
	// Conn is the packet socket used for commands. When nil, a fresh
	// UDP socket is opened per command. Tests inject a loopback conn
	// that answers directly.
	Conn net.PacketConn
	// Timeout bounds each command round trip (default 2s).
	Timeout time.Duration
	// ReadFunc customizes reading replies (tests only; nil = default).
	ReadFunc func(conn net.PacketConn, buf []byte) (int, net.Addr, error)
}

// NewClient returns a Client for the given device IP, defaulting the
// command port to 4003.
func NewClient(deviceIP string) *Client {
	addr := deviceIP
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, fmt.Sprintf("%d", CommandPort))
	}
	return &Client{Addr: addr, Timeout: 2 * time.Second}
}

// send writes msg to the device and returns the reply payload.
func (c *Client) send(ctx context.Context, msg any) ([]byte, error) {
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("lan: marshal command: %w", err)
	}
	conn := c.Conn
	own := false
	if conn == nil {
		udp, err := net.ListenUDP("udp", nil)
		if err != nil {
			return nil, fmt.Errorf("lan: open socket: %w", err)
		}
		conn = udp
		own = true
		defer udp.Close()
	}
	raddr, err := net.ResolveUDPAddr("udp", c.Addr)
	if err != nil {
		return nil, fmt.Errorf("lan: resolve %q: %w", c.Addr, err)
	}
	if _, err := conn.WriteTo(b, raddr); err != nil {
		return nil, fmt.Errorf("lan: send to %s: %w", c.Addr, err)
	}

	buf := make([]byte, 4096)
	readFn := c.ReadFunc
	if readFn == nil {
		readFn = func(pc net.PacketConn, buf []byte) (int, net.Addr, error) {
			if sd, ok := pc.(interface{ SetReadDeadline(time.Time) error }); ok {
				if err := sd.SetReadDeadline(time.Now().Add(c.Timeout)); err != nil {
					return 0, nil, err
				}
			}
			return pc.ReadFrom(buf)
		}
	}
	n, _, err := readFn(conn, buf)
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return nil, fmt.Errorf("%w: %s did not reply within %s", ErrNoResponse, c.Addr, c.Timeout)
		}
		return nil, fmt.Errorf("lan: read reply from %s: %w", c.Addr, err)
	}
	_ = own
	return buf[:n], nil
}

// DevStatus queries the device's current status. The returned map contains
// the decoded "data" object of the reply (fields vary by model, e.g.
// {"on":true,"brightness":50,"color":{"r":..,"g":..,"b":..}}).
func (c *Client) DevStatus(ctx context.Context) (map[string]any, error) {
	raw, err := c.send(ctx, msgWithCmd(CmdDevStatus, nil))
	if err != nil {
		return nil, err
	}
	var m struct {
		Msg struct {
			Cmd  string `json:"cmd"`
			Data any    `json:"data"`
		} `json:"msg"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("lan: parse devStatus reply: %w", err)
	}
	out := map[string]any{}
	if d, ok := m.Msg.Data.(map[string]any); ok {
		for k, v := range d {
			out[k] = v
		}
	}
	return out, nil
}

// Turn sends a turn command (data {"on": bool}).
func (c *Client) Turn(ctx context.Context, on bool) error {
	_, err := c.send(ctx, msgWithCmd(CmdTurn, map[string]any{"on": on}))
	return err
}

// Brightness sets brightness in percent (0-100).
func (c *Client) Brightness(ctx context.Context, percent int) error {
	_, err := c.send(ctx, msgWithCmd(CmdBrightness, map[string]any{"value": percent}))
	return err
}

// ColorWC sets the white-mode color temperature in Kelvin (2000-9000).
func (c *Client) ColorWC(ctx context.Context, kelvin int) error {
	_, err := c.send(ctx, msgWithCmd(CmdColorWC, map[string]any{"value": kelvin}))
	return err
}

// Color sets an RGB color (data {"color":{"r":r,"g":g,"b":b}}).
func (c *Client) Color(ctx context.Context, r, g, b uint8) error {
	_, err := c.send(ctx, msgWithCmd(CmdColor, map[string]any{"color": rgb(r, g, b)}))
	return err
}

// SetColor is Color with the newer "setColor" command name.
func (c *Client) SetColor(ctx context.Context, r, g, b uint8) error {
	_, err := c.send(ctx, msgWithCmd(CmdSetColor, map[string]any{"color": rgb(r, g, b)}))
	return err
}

// Scene applies a named scene.
func (c *Client) Scene(ctx context.Context, name string) error {
	_, err := c.send(ctx, msgWithCmd(CmdScene, map[string]any{"scene": name}))
	return err
}

// MusicMode toggles music mode (data {"on": bool}) on supporting devices.
func (c *Client) MusicMode(ctx context.Context, on bool) error {
	_, err := c.send(ctx, msgWithCmd(CmdMusicMode, map[string]any{"on": on}))
	return err
}

func rgb(r, g, b uint8) map[string]uint8 {
	return map[string]uint8{"r": r, "g": g, "b": b}
}

// msgWithCmd builds the {"msg":{"cmd":..,"data":..}} LAN envelope.
func msgWithCmd(cmd string, data any) map[string]any {
	return map[string]any{"msg": map[string]any{"cmd": cmd, "data": data}}
}
