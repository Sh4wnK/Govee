package lan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

// startFakeDevice binds a loopback UDP socket on an ephemeral port and
// spawns a goroutine that records every datagram it receives and (optionally)
// replies. Tests use it as a stand-in for a real Govee device.
type fakeDevice struct {
	conn *net.UDPConn
	port int

	mu       sync.Mutex
	received []map[string]any
	reply    []byte
	done     chan struct{}
}

func startFakeDevice(t *testing.T) *fakeDevice {
	t.Helper()
	f := &fakeDevice{mu: sync.Mutex{}}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f.conn = conn
	if la := conn.LocalAddr(); la != nil {
		if ua, ok := la.(*net.UDPAddr); ok {
			f.port = ua.Port
		}
	}
	f.done = make(chan struct{}, 1)
	go func() {
		defer func() { f.done <- struct{}{} }()
		buf := make([]byte, 4096)
		for {
			n, src, err := conn.ReadFrom(buf)
			if err != nil {
				return // socket closed
			}
			var m map[string]any
			_ = json.Unmarshal(buf[:n], &m)
			f.mu.Lock()
			f.received = append(f.received, m)
			reply := f.reply
			f.mu.Unlock()
			if len(reply) > 0 {
				conn.WriteTo(reply, src)
			}
		}
	}()
	t.Cleanup(func() {
		f.conn.Close()
		select {
		case <-f.done:
		case <-time.After(2 * time.Second):
		}
	})
	return f
}

// setReply makes the fake device echo these bytes for each datagram.
func (f *fakeDevice) setReply(b []byte) {
	f.mu.Lock()
	f.reply = b
	f.mu.Unlock()
}

// waitFor blocks until at least n datagrams were received (or ~3s) and
// returns the ones collected so far.
func (f *fakeDevice) waitFor(n int) []map[string]any {
	deadline := time.Now().Add(3 * time.Second)
	for {
		f.mu.Lock()
		out := f.received
		f.mu.Unlock()
		if len(out) >= n {
			return out
		}
		if time.Now().After(deadline) {
			return out
		}
		time.Sleep(time.Millisecond)
	}
}

func cmdOf(m map[string]any) string {
	var msg struct {
		Msg struct {
			Cmd string `json:"cmd"`
		} `json:"msg"`
	}
	if err := json.Unmarshal(mapToJSON(m), &msg); err != nil {
		return ""
	}
	return msg.Msg.Cmd
}

func dataOf(m map[string]any) map[string]any {
	var msg struct {
		Msg struct {
			Data map[string]any `json:"data"`
		} `json:"msg"`
	}
	if err := json.Unmarshal(mapToJSON(m), &msg); err != nil {
		return nil
	}
	return msg.Msg.Data
}

func mapToJSON(m map[string]any) []byte {
	b, err := json.Marshal(m)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}

func fakeAddr(port int) string {
	return fmt.Sprintf("127.0.0.1:%d", port)
}

func TestLANClientCommands(t *testing.T) {
	f := startFakeDevice(t)
	c := NewClient(fakeAddr(f.port))
	c.Timeout = time.Second

	f.setReply([]byte(`{"msg":{"cmd":"turn","data":{"on":true}}}`))
	if err := c.Turn(context.Background(), true); err != nil {
		t.Fatalf("Turn: %v", err)
	}
	f.setReply([]byte(`{"msg":{"cmd":"brightness","data":{"value":50}}}`))
	if err := c.Brightness(context.Background(), 50); err != nil {
		t.Fatalf("Brightness: %v", err)
	}
	f.setReply([]byte(`{"msg":{"cmd":"color","data":{"color":{"r":255,"g":0,"b":0}}}}`))
	if err := c.Color(context.Background(), 255, 0, 0); err != nil {
		t.Fatalf("Color: %v", err)
	}
	// CVSA: receive-window replies must not corrupt the next command.
	if err := c.SetColor(context.Background(), 1, 2, 3); err != nil {
		t.Fatalf("SetColor: %v", err)
	}

	msgs := f.waitFor(4)
	if len(msgs) != 4 {
		t.Fatalf("got %d messages, want 4", len(msgs))
	}
	want := []string{"turn", "brightness", "color", "setColor"}
	for i := range want {
		if cmdOf(msgs[i]) != want[i] {
			t.Errorf("cmd %d = %q, want %q", i, cmdOf(msgs[i]), want[i])
		}
	}
	if v := dataOf(msgs[0])["on"]; v != true {
		t.Errorf("turn data.on = %v, want true", v)
	}
	if v := dataOf(msgs[1])["value"]; v != float64(50) {
		t.Errorf("brightness data.value = %v, want 50", v)
	}
	col := dataOf(msgs[2])["color"]
	if col == nil {
		t.Fatal("color missing")
	}
}

func TestLANDevStatusParsesReply(t *testing.T) {
	f := startFakeDevice(t)
	c := NewClient(fakeAddr(f.port))
	c.Timeout = time.Second
	f.setReply([]byte(`{"msg":{"cmd":"devStatus","data":{"on":true,"brightness":67,"color":{"r":1,"g":2,"b":3}}}}`))

	st, err := c.DevStatus(context.Background())
	if err != nil {
		t.Fatalf("DevStatus: %v", err)
	}
	if st["on"] != true {
		t.Errorf("on = %v, want true", st["on"])
	}
	if st["brightness"] != float64(67) {
		t.Errorf("brightness = %v, want 67", st["brightness"])
	}
	col, ok := st["color"].(map[string]any)
	if !ok {
		t.Fatalf("color = %T, want map", st["color"])
	}
	if col["r"] != float64(1) || col["g"] != float64(2) || col["b"] != float64(3) {
		t.Errorf("color = %+v", col)
	}
	msgs := f.waitFor(1)
	if len(msgs) != 1 || cmdOf(msgs[0]) != CmdDevStatus {
		got := ""
		if len(msgs) == 1 {
			got = cmdOf(msgs[0])
		}
		t.Errorf("cmd = %q, want devStatus", got)
	}
}

func TestLANNoResponseTimeout(t *testing.T) {
	f := startFakeDevice(t)
	c := NewClient(fakeAddr(f.port))
	c.Timeout = 100 * time.Millisecond
	start := time.Now()
	_, err := c.DevStatus(context.Background())
	if !errors.Is(err, ErrNoResponse) {
		t.Fatalf("err = %v, want ErrNoResponse", err)
	}
	if elapsed := time.Since(start); elapsed < 90*time.Millisecond || elapsed > time.Second {
		t.Errorf("timeout took %v, want ~100ms", elapsed)
	}
}

func TestLANSendEnvelope(t *testing.T) {
	f := startFakeDevice(t)
	c := NewClient(fakeAddr(f.port))
	c.Timeout = time.Second
	f.setReply([]byte(`{"msg":{"cmd":"scene","data":{"ok":true}}}`))
	if err := c.Scene(context.Background(), "Aurora"); err != nil {
		t.Fatalf("Scene: %v", err)
	}
	f.setReply([]byte(`{"msg":{"cmd":"musicMode","data":{"ok":true}}}`))
	if err := c.MusicMode(context.Background(), true); err != nil {
		t.Fatalf("MusicMode: %v", err)
	}
	msgs := f.waitFor(2)
	if len(msgs) != 2 {
		t.Fatal("no message")
	}
	if cmdOf(msgs[0]) != CmdScene || dataOf(msgs[0])["scene"] != "Aurora" {
		t.Errorf("scene message = %+v", msgs[0])
	}
	if cmdOf(msgs[1]) != CmdMusicMode || dataOf(msgs[1])["on"] != true {
		t.Errorf("music message = %+v", msgs[1])
	}
}

func TestScanParsesReplies(t *testing.T) {
	// A fake "device" that answers a scan probe with the documented
	// reply shape, and a Scanner pointed at it.
	f := startFakeDevice(t)
	reply := []byte(`{"msg":{"cmd":"scan","data":{"ip":"192.168.1.50","sku":"H6003","device":"AA:BB:CC:DD:EE:FF","deviceName":"Living Room","capabilities":["turn","brightness","colorwc","color"]}}}`)
	f.setReply(reply)

	// The scan probe goes to the multicast address; the fake device is
	// not on the multicast group, so drive the parser directly instead:
	// a real integration test of Scanner requires a multicast-capable
	// host. The parser test below covers reply decoding.
	d := parseDeviceReply(reply, &net.UDPAddr{IP: net.ParseIP("192.168.1.50"), Port: ResponsePort})
	if d == nil {
		t.Fatal("parseDeviceReply returned nil")
	}
	if d.IP != "192.168.1.50" || d.SKU != "H6003" || d.MAC != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("unexpected device %+v", d)
	}
	if d.Name != "Living Room" || len(d.Capabilities) != 4 {
		t.Errorf("unexpected name/caps %+v", d)
	}
	if !d.Supports("TURN") || !d.Supports("brightness") || d.Supports("color") == false {
		t.Errorf("Supports misbehaving: %+v", d)
	}
}

func TestScanIgnoresNoise(t *testing.T) {
	// Random network noise and non-scan replies must be ignored.
	if d := parseDeviceReply([]byte(`garbage`), nil); d != nil {
		t.Errorf("garbage parsed as device: %+v", d)
	}
	if d := parseDeviceReply([]byte(`{"msg":{"cmd":"turn","data":{}}}`), nil); d != nil {
		t.Errorf("turn reply parsed as device: %+v", d)
	}
	if d := parseDeviceReply([]byte(`{}`), nil); d != nil {
		t.Errorf("empty parsed as device: %+v", d)
	}
	if d := parseDeviceReply([]byte(`{"msg":{"cmd":"scan","data":{"sku":"H1"}}}`), nil); d != nil {
		t.Errorf("scan without ip parsed as device: %+v", d)
	}
}

func TestDeviceSupportsCaseInsensitive(t *testing.T) {
	d := &Device{Capabilities: []string{"turn", "BRIGHTNESS", "colorwc"}}
	if !d.Supports("Turn") || !d.Supports("brightness") || !d.Supports("COLORWC") {
		t.Error("case-insensitive Supports failed")
	}
	if d.Supports("color") {
		t.Error("Supports(color) should be false")
	}
}
func TestScannerSendsProbeAndCollectsReplies(t *testing.T) {
	// Point the Scanner at a loopback fake "device": the probe address
	// is overridden so no real multicast is used, and the fake replies
	// with a scan message on the same socket the probe came from.
	f := startFakeDevice(t)
	reply := []byte(`{"msg":{"cmd":"scan","data":{"ip":"127.0.0.1","sku":"H6003","device":"AA:BB:CC:DD:EE:FF","deviceName":"Kitchen","capabilities":["turn","brightness"]}}}`)
	f.setReply(reply)

	s := &Scanner{
		MulticastAddr: fakeAddr(f.port),
		Conn:          dialUDP(t),
		Timeout:       500 * time.Millisecond,
	}
	// The Scanner's conn is a *net.UDPConn bound to an ephemeral port;
	// WriteTo carries the source address so the fake can reply directly.
	devices, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("got %d devices, want 1", len(devices))
	}
	d := devices[0]
	if d.SKU != "H6003" || d.MAC != "AA:BB:CC:DD:EE:FF" || d.Name != "Kitchen" {
		t.Errorf("unexpected device %+v", d)
	}
	if !d.Supports("turn") {
		t.Error("expected turn capability")
	}
	// The fake saw the probe with the scan command.
	msgs := f.waitFor(1)
	if len(msgs) != 1 || cmdOf(msgs[0]) != "scan" {
		t.Errorf("probe = %+v, want scan cmd", msgs)
	}
}

// dialUDP opens a loopback UDP socket for Scanner tests.
func dialUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("dial udp: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}
