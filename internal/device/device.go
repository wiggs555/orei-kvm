package device

import (
	"fmt"
	"sync"

	"github.com/wiggs555/orei-kvm/internal/protocol"
)

// Bus is the serial command transport.
type Bus interface {
	Command(cmd string) (string, error)
	CommandWithConfirm(cmd, confirm string) (string, error)
}

// Device is a high-level wrapper around the full OREI RS-232 command set.
type Device struct {
	mu  sync.Mutex
	Bus Bus
}

func New(bus Bus) *Device {
	return &Device{Bus: bus}
}

func (d *Device) raw(cmd string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.Bus.Command(cmd)
}

func (d *Device) Help() (string, error) {
	return d.raw(protocol.CmdHelp())
}

func (d *Device) Firmware() (string, error) {
	return d.raw(protocol.CmdGetFW())
}

func (d *Device) Reboot() (string, error) {
	return d.raw(protocol.CmdReboot())
}

func (d *Device) Reset(confirm bool) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !confirm {
		return "", fmt.Errorf("factory reset requires confirmation (pass --yes)")
	}
	return d.Bus.CommandWithConfirm(protocol.CmdReset(), protocol.CmdResetConfirm())
}

func (d *Device) Status() (protocol.Status, error) {
	resp, err := d.raw(protocol.CmdGetStatus())
	if err != nil {
		return protocol.Status{}, err
	}
	return protocol.ParseStatus(resp), nil
}

func (d *Device) SetKey(on bool) (string, error) {
	return d.raw(protocol.CmdSetKey(on))
}

func (d *Device) GetKey() (bool, string, error) {
	resp, err := d.raw(protocol.CmdGetKey())
	if err != nil {
		return false, resp, err
	}
	on, err := protocol.ParseKeyOn(resp)
	return on, resp, err
}

func (d *Device) SetBaudIndex(idx int) (string, error) {
	if _, err := protocol.BaudRateFromIndex(idx); err != nil {
		return "", err
	}
	return d.raw(protocol.CmdSetBaud(idx))
}

func (d *Device) GetBaud() (int, string, error) {
	resp, err := d.raw(protocol.CmdGetBaud())
	if err != nil {
		return 0, resp, err
	}
	baud, err := protocol.ParseBaud(resp)
	return baud, resp, err
}

func (d *Device) SetInput(host int) (string, error) {
	if err := protocol.ValidateHost(host); err != nil {
		return "", err
	}
	return d.raw(protocol.CmdSetInput(host))
}

func (d *Device) GetInput() (int, string, error) {
	resp, err := d.raw(protocol.CmdGetInput())
	if err != nil {
		return 0, resp, err
	}
	host, err := protocol.ParseHost(resp)
	return host, resp, err
}

func (d *Device) GetUSB5V(x int) (string, error) {
	if err := protocol.ValidateUSB5V(x); err != nil {
		return "", err
	}
	return d.raw(protocol.CmdGetUSB5V(x))
}

func (d *Device) SetAutoswitch(on bool) (string, error) {
	return d.raw(protocol.CmdSetAutoswitch(on))
}

func (d *Device) GetAutoswitch() (bool, string, error) {
	resp, err := d.raw(protocol.CmdGetAutoswitch())
	if err != nil {
		return false, resp, err
	}
	on, err := protocol.ParseAutoswitchOn(resp)
	return on, resp, err
}

func (d *Device) SetTXUSBD(port int, power protocol.PowerMode) (string, error) {
	if err := protocol.ValidateTXPort(port); err != nil {
		return "", err
	}
	return d.raw(protocol.CmdSetTXUSBD(port, int(power)))
}

func (d *Device) GetTXUSBD(port int) (string, error) {
	if err := protocol.ValidateTXPort(port); err != nil {
		return "", err
	}
	return d.raw(protocol.CmdGetTXUSBD(port))
}

func (d *Device) SetRXUSBD(port int, power protocol.PowerMode) (string, error) {
	if err := protocol.ValidateRXPort(port); err != nil {
		return "", err
	}
	return d.raw(protocol.CmdSetRXUSBD(port, int(power)))
}

func (d *Device) GetRXUSBD(port int) (string, error) {
	if err := protocol.ValidateRXPort(port); err != nil {
		return "", err
	}
	return d.raw(protocol.CmdGetRXUSBD(port))
}

func (d *Device) HDBTUpdate() (string, error) {
	return d.raw(protocol.CmdHDBTUpdate())
}

func (d *Device) Raw(cmd string) (string, error) {
	return d.raw(cmd)
}
