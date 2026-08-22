package mock_test

import (
	"testing"

	"github.com/wiggs555/orei-kvm/internal/device"
	"github.com/wiggs555/orei-kvm/internal/mock"
	"github.com/wiggs555/orei-kvm/internal/protocol"
)

func TestMockFullCommandSurface(t *testing.T) {
	dev := mock.New()
	cli := mock.NewClient(dev, 1)
	if err := cli.Open("mock://orei"); err != nil {
		t.Fatal(err)
	}
	d := device.New(cli)

	if _, err := d.Help(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Firmware(); err != nil {
		t.Fatal(err)
	}
	st, err := d.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Source != 1 {
		t.Fatalf("source %d", st.Source)
	}

	if _, err := d.SetKey(false); err != nil {
		t.Fatal(err)
	}
	on, _, err := d.GetKey()
	if err != nil || on {
		t.Fatalf("key %v %v", on, err)
	}

	if _, err := d.SetBaudIndex(4); err != nil {
		t.Fatal(err)
	}
	baud, _, err := d.GetBaud()
	if err != nil || baud != 38400 {
		t.Fatalf("baud %d %v", baud, err)
	}

	if _, err := d.SetAutoswitch(false); err != nil {
		t.Fatal(err)
	}
	as, _, err := d.GetAutoswitch()
	if err != nil || as {
		t.Fatalf("autoswitch %v %v", as, err)
	}

	if _, err := d.GetUSB5V(0); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SetTXUSBD(0, protocol.PowerForceOn); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetTXUSBD(0); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SetRXUSBD(3, protocol.PowerForceOff); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetRXUSBD(3); err != nil {
		t.Fatal(err)
	}
	if _, err := d.HDBTUpdate(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Reboot(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Reset(true); err != nil {
		t.Fatal(err)
	}

	// Switch away — mock serial should disappear for host 1.
	if _, err := d.SetInput(2); err != nil {
		t.Fatal(err)
	}
	if cli.Connected() {
		t.Fatal("expected disconnect after switching to host 2")
	}
	if cli.PortPresent() {
		t.Fatal("port should not be present on inactive host")
	}
}

func TestMockReconnectWhenActive(t *testing.T) {
	dev := mock.New()
	dev.SetHost(2)
	cli := mock.NewClient(dev, 1)
	if err := cli.Open(""); err == nil {
		t.Fatal("expected open failure while inactive")
	}
	dev.SetHost(1)
	if err := cli.Open(""); err != nil {
		t.Fatal(err)
	}
	host, _, err := device.New(cli).GetInput()
	if err != nil || host != 1 {
		t.Fatalf("host %d %v", host, err)
	}
}
