package mock

import (
	"fmt"
	"strings"
	"sync"
)

// Device simulates the OREI USB3-EX2H330R-K RS-232 ASCII command interface.
type Device struct {
	mu         sync.Mutex
	host       int
	keyOn      bool
	baud       int
	autoswitch bool
	txPower    [3]int
	rxPower    [5]int
}

func New() *Device {
	d := &Device{
		host:       1,
		keyOn:      true,
		baud:       115200,
		autoswitch: true,
	}
	for i := range d.txPower {
		d.txPower[i] = 1
	}
	for i := range d.rxPower {
		d.rxPower[i] = 1
	}
	return d
}

func (d *Device) Host() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.host
}

func (d *Device) SetHost(h int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.host = h
}

// Handle processes one command line (without CRLF) and returns the device feedback.
func (d *Device) Handle(line string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	cmd := strings.ToLower(strings.TrimSpace(line))
	switch {
	case cmd == "?" || cmd == "help":
		return "help\nget fw version\nset reboot\nset reset\nget status\nset key on/off\nget key\nset baud x\nget baud\nset input x\nget input\nget usb5v x\nset autoswitch x\nget autoswitch\nset tx usbd x power y\nget tx usbd x power\nset rx usbd x power y\nget rx usbd x power\nset hdbt update"
	case cmd == "get fw version":
		return "TX FW 1.0.0\nRX FW 1.0.0"
	case cmd == "set reboot":
		return "Reboot...\nSystem Initializing...\nInitialization Finished!\nTX FW 1.0.0"
	case cmd == "set reset":
		return "Sure to RESET to default settings?\nType \"Yes\" after next prompt to confirm..."
	case cmd == "yes":
		d.host = 1
		d.keyOn = true
		d.baud = 115200
		d.autoswitch = true
		return "Reset done"
	case cmd == "get status":
		key := "Off"
		if d.keyOn {
			key = "On"
		}
		as := "Off"
		if d.autoswitch {
			as = "On"
		}
		return fmt.Sprintf("Status Info 2-Port USB 3.2 Gen 1 Extender\nTX FW 1.0.0 RX FW 1.0.0\nSource    Key     Baud      AutoSwitch\n0%d           %s      %d    %s\nIn/Out     Host_1               Host_2           HDMI_Out1      HDMI_Out2\nCable      Connected    Connected     Connected       Connected\nOutput    USB_Power\nTX_01      Follow_Input\nTX_02      Follow_Input\nRX_01      Follow_Input\nRX_02      Follow_Input\nRX_03      Follow_Input\nRX_04      Follow_Input",
			d.host, key, d.baud, as)
	case cmd == "set key on":
		d.keyOn = true
		return "Set key on"
	case cmd == "set key off":
		d.keyOn = false
		return "Set key off"
	case cmd == "get key":
		if d.keyOn {
			return "Key on"
		}
		return "Key off"
	case strings.HasPrefix(cmd, "set baud "):
		var idx int
		fmt.Sscanf(cmd, "set baud %d", &idx)
		rates := map[int]int{1: 4800, 2: 9600, 3: 19200, 4: 38400, 5: 57600, 6: 115200}
		if rate, ok := rates[idx]; ok {
			d.baud = rate
			return fmt.Sprintf("Set baud rate %d", rate)
		}
		return "Invalid baud"
	case cmd == "get baud":
		return fmt.Sprintf("Baud rate %d", d.baud)
	case strings.HasPrefix(cmd, "set input "):
		var h int
		fmt.Sscanf(cmd, "set input %d", &h)
		if h == 1 || h == 2 {
			d.host = h
			return fmt.Sprintf("Set input USB host %d", h)
		}
		return "Invalid input"
	case cmd == "get input":
		return fmt.Sprintf("Input USB host %d", d.host)
	case strings.HasPrefix(cmd, "get usb5v "):
		var x int
		fmt.Sscanf(cmd, "get usb5v %d", &x)
		if x == 0 {
			return "USB host 1: 5V\nUSB host 2: none"
		}
		if x == 1 {
			return "USB host 1: 5V"
		}
		return "USB host 2: none"
	case cmd == "set autoswitch on":
		d.autoswitch = true
		return "Set autoswitch on"
	case cmd == "set autoswitch off":
		d.autoswitch = false
		return "Set autoswitch off"
	case cmd == "get autoswitch":
		if d.autoswitch {
			return "Autoswitch on"
		}
		return "Autoswitch off"
	case strings.HasPrefix(cmd, "set tx usbd "):
		var port, power int
		fmt.Sscanf(cmd, "set tx usbd %d power %d", &port, &power)
		if port == 0 {
			for i := range d.txPower {
				d.txPower[i] = power
			}
			return "Set TX all USB device ports power follow USB host power"
		}
		if port >= 1 && port <= 2 {
			d.txPower[port] = power
			return fmt.Sprintf("Set TX USB device %d power", port)
		}
		return "Invalid"
	case strings.HasPrefix(cmd, "get tx usbd "):
		var port int
		fmt.Sscanf(cmd, "get tx usbd %d power", &port)
		mode := func(p int) string {
			switch d.txPower[p] {
			case 0:
				return "force off"
			case 2:
				return "force on"
			default:
				return "follow USB host power"
			}
		}
		if port == 0 {
			return "TX all USB device ports power " + mode(1)
		}
		return fmt.Sprintf("TX USB device %d power %s", port, mode(port))
	case strings.HasPrefix(cmd, "set rx usbd "):
		var port, power int
		fmt.Sscanf(cmd, "set rx usbd %d power %d", &port, &power)
		if port == 0 {
			for i := range d.rxPower {
				d.rxPower[i] = power
			}
			return "Set RX all USB device ports power follow USB host power"
		}
		if port >= 1 && port <= 4 {
			d.rxPower[port] = power
			return fmt.Sprintf("Set RX USB device %d power", port)
		}
		return "Invalid"
	case strings.HasPrefix(cmd, "get rx usbd "):
		var port int
		fmt.Sscanf(cmd, "get rx usbd %d power", &port)
		if port == 0 {
			return "RX all USB device ports power follow USB host power"
		}
		return fmt.Sprintf("RX USB device %d power follow USB host power", port)
	case cmd == "set hdbt update":
		return "Hdbt update"
	default:
		return "Unknown command"
	}
}

// Client implements the same surface as serial.Client for in-process mocking.
type Client struct {
	mu        sync.Mutex
	dev       *Device
	connected bool
	path      string
	// AbsentHost simulates the serial adapter disappearing when this
	// machine is not the active host. When set (>0), Connect succeeds
	// only while Device.host == AbsentHost... wait: MyHost.
	MyHost int
}

func NewClient(dev *Device, myHost int) *Client {
	if myHost != 1 && myHost != 2 {
		myHost = 1
	}
	return &Client{dev: dev, path: "mock://orei", MyHost: myHost}
}

func (c *Client) Path() string {
	return c.path
}

func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected && c.portPresentLocked()
}

func (c *Client) portPresentLocked() bool {
	// Serial USB is only present on the active host.
	return c.dev.Host() == c.MyHost
}

func (c *Client) Open(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if path != "" {
		c.path = path
	}
	if !c.portPresentLocked() {
		c.connected = false
		return fmt.Errorf("mock port not available (active host is %d, this machine is host %d)", c.dev.Host(), c.MyHost)
	}
	c.connected = true
	return nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
	return nil
}

func (c *Client) SetBaud(int) {}

func (c *Client) Command(cmd string) (string, error) {
	return c.CommandWithConfirm(cmd, "")
}

func (c *Client) CommandWithConfirm(cmd, confirm string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || !c.portPresentLocked() {
		c.connected = false
		return "", fmt.Errorf("serial port not connected")
	}
	resp := c.dev.Handle(cmd)
	if confirm != "" {
		resp = resp + "\n" + c.dev.Handle(confirm)
	}
	// Switching away disconnects this mock client.
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(cmd)), "set input ") {
		if !c.portPresentLocked() {
			c.connected = false
		}
	}
	return resp, nil
}

// PortPresent reports whether the mock serial adapter is visible to this host.
func (c *Client) PortPresent() bool {
	return c.dev.Host() == c.MyHost
}
