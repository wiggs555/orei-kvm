package device

import (
	"strings"
	"testing"
	"time"

	"github.com/wiggs555/orei-kvm/internal/mock"
	"github.com/wiggs555/orei-kvm/internal/protocol"
)

func TestCycleRXUSBDRestoresOn(t *testing.T) {
	md := mock.New()
	cli := mock.NewClient(md, 1)
	if err := cli.Open("mock://orei"); err != nil {
		t.Fatal(err)
	}
	d := New(cli)

	resp, err := d.CycleRXUSBD(3, 0, protocol.PowerForceOn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(resp), "power") {
		t.Fatalf("unexpected cycle response: %q", resp)
	}

	got, err := d.GetRXUSBD(3)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(got), "force on") {
		t.Fatalf("want force on after cycle, got %q", got)
	}
}

func TestCycleRXUSBDRejectsRestoreOff(t *testing.T) {
	md := mock.New()
	cli := mock.NewClient(md, 1)
	if err := cli.Open("mock://orei"); err != nil {
		t.Fatal(err)
	}
	_, err := New(cli).CycleRXUSBD(0, time.Millisecond, protocol.PowerForceOff)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCycleTXUSBD(t *testing.T) {
	md := mock.New()
	cli := mock.NewClient(md, 1)
	if err := cli.Open("mock://orei"); err != nil {
		t.Fatal(err)
	}
	d := New(cli)
	if _, err := d.CycleTXUSBD(1, 0, protocol.PowerFollow); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetTXUSBD(1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(got), "follow") {
		t.Fatalf("want follow after cycle, got %q", got)
	}
}
