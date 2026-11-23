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

func TestViewStateIgnoresUpdatedAt(t *testing.T) {
	a := viewFromState(ipc.State{Connected: true, ActiveHost: 1, IAmActive: true, LastError: ""})
	b := viewFromState(ipc.State{Connected: true, ActiveHost: 1, IAmActive: true, LastError: ""})
	if a != b {
		t.Fatalf("view states should compare equal when only timestamps differ")
	}
	b.activeHost = 2
	if a == b {
		t.Fatal("expected inequality after host change")
	}
}

func TestIconForView(t *testing.T) {
	if iconForView(viewState{daemonDown: true}) != iconKindInactive {
		t.Fatal("daemon down")
	}
	if iconForView(viewState{activeHost: 1}) != iconKindHost1 {
		t.Fatal("host1")
	}
	if iconForView(viewState{activeHost: 2}) != iconKindHost2 {
		t.Fatal("host2")
	}
	if iconForView(viewState{connected: true}) != iconKindUnknown {
		t.Fatal("unknown")
	}
	if iconForView(viewState{}) != iconKindInactive {
		t.Fatal("inactive")
	}
}
