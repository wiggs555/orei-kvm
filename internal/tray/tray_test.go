//go:build !notray

package tray

import (
	"strings"
	"testing"

	"github.com/wiggs555/orei-kvm/internal/ipc"
)

func TestStatusLabelConnected(t *testing.T) {
	label := statusLabel(ipc.State{Connected: true, ActiveHost: 2, IAmActive: true})
	if !strings.Contains(label, "Host 2") || !strings.Contains(label, "this machine") {
		t.Fatalf("label=%q", label)
	}
	if barTitle(ipc.State{Connected: true, ActiveHost: 2, IAmActive: true}) != "OREI H2" {
		t.Fatal(barTitle(ipc.State{Connected: true, ActiveHost: 2, IAmActive: true}))
	}
}

func TestStatusLabelShowsDaemonError(t *testing.T) {
	label := statusLabel(ipc.State{Connected: false, LastError: "open /dev/cu.usbserial: busy"})
	if label != "open /dev/cu.usbserial: busy" {
		t.Fatalf("label=%q", label)
	}
	if barTitle(ipc.State{Connected: false}) != "OREI off" {
		t.Fatal("bar title")
	}
}
