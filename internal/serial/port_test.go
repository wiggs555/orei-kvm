package serial

import "testing"

func TestPickPortPrefersCalloutDevice(t *testing.T) {
	ports := []PortInfo{
		{Name: "/dev/tty.usbserial-10", Description: "USB Serial"},
		{Name: "/dev/cu.usbserial-10", Description: "USB Serial"},
	}
	got, err := pickPort(ports, "", []string{"usbserial"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "/dev/cu.usbserial-10" {
		t.Fatalf("got %s", got)
	}
}

func TestPickPortMatchesConfiguredBasename(t *testing.T) {
	ports := []PortInfo{{Name: "/dev/cu.usbserial-10"}}
	got, err := pickPort(ports, "cu.usbserial-10", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/dev/cu.usbserial-10" {
		t.Fatalf("got %s", got)
	}
}
