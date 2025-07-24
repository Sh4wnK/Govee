# Govee  
**Govee smart device controller in Go**

A lightweight Go library to interface with Govee smart devices (LED strips, bulbs, plugs, etc.) via LAN/BLE/API for local control and automation.  

---

## 🚀 Features
- Discover and manage Govee devices using Go structs and methods  
- Send commands to toggle power, set brightness or RGB color  
- Modular handlers for different device types  
- Easy to integrate into larger Go applications or CLI tools  

---

## 📦 Usage

### Installation

```bash
go get github.com/Sh4wnK/Govee
````

### Import and initialize

```go
import "github.com/Sh4wnK/Govee"

func main() {
    g := govee.NewClient()

    // Discover devices
    devices, err := g.ScanDevices()
    if err != nil {
        log.Fatalf("Scan failed: %v", err)
    }

    for _, dev := range devices {
        fmt.Printf("Found device: %s (%s)\n", dev.Name, dev.DeviceID)
    }

    // Example: Turn on a device and set color
    if len(devices) > 0 {
        d := devices[0]
        err = g.TurnOn(d)
        if err != nil {
            log.Printf("Turn-on error: %v", err)
        }

        // Set color to purple
        err = g.SetColor(d, 128, 0, 128)
        if err != nil {
            log.Printf("Color error: %v", err)
        }
    }
}
```

---

## 📐 API Overview

| Function                                         | Description                            |
| ------------------------------------------------ | -------------------------------------- |
| `NewClient()`                                    | Creates a new client instance          |
| `ScanDevices()`                                  | Discovers available devices on LAN/BLE |
| `TurnOn/TurnOff`                                 | Power controls                         |
| `SetBrightness`                                  | Adjusts brightness level               |
| `SetColor(r,g,b)`                                | Sets RGB color value                   |
| Additional handlers for device-specific commands |                                        |

Check method docs inside `DeviceHandler.go` and `Device.go`.

---

## 🧪 Examples

* Use `ScanDevices()` to detect and list devices
* Chain commands like `TurnOn`, `SetBrightness`, and `SetColor`
* Build scenarios or scheduled automations

---

## ⚙️ Configuration

* No external config required
* Adjust BLE timeout and LAN discovery parameters in code as needed

---

## 🧩 Contribution

Contributions welcome!

1. Fork the repo
2. Create a feature branch
3. Open a pull request

Areas for improvement:

* Support for more Govee models
* Add scenes, music or DIY modes
* Better error handling and retries

---

## 📝 License

MIT License – see [LICENSE](./LICENSE)

---

## 🔗 See Also

* [wez/govee-py](https://github.com/wez/govee-py): Python Govee control library
* [LaggAt/python-govee-api](https://github.com/LaggAt/python-govee-api): Govee API wrapper in Python
* Home Assistant integrations like [`hacs-govee`](https://github.com/LaggAt/hacs-govee) for API-based control

---

## 🙌 Acknowledgements

Inspired by community projects in Python and Go for Govee control:

* **wez/govee-py**
* **LaggAt/python-govee-api**

