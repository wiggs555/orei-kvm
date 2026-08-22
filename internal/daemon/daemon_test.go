package daemon_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/wiggs555/orei-kvm/internal/config"
	"github.com/wiggs555/orei-kvm/internal/daemon"
	"github.com/wiggs555/orei-kvm/internal/ipc"
)

func TestDaemonMockHostSwitch(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.MyHost = 1
	cfg.SocketPath = filepath.Join(dir, "orei.sock")
	cfg.PollInterval = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := daemon.New(cfg, true)
	errCh := make(chan error, 1)
	go func() { errCh <- d.Start(ctx) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ipc.IsDaemonUp(cfg.SocketPath) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ipc.IsDaemonUp(cfg.SocketPath) {
		t.Fatal("daemon failed to start")
	}

	st, err := ipc.Call(cfg.SocketPath, ipc.Request{Op: ipc.OpState})
	if err != nil || !st.OK || st.State == nil || !st.State.Connected {
		t.Fatalf("initial state: %+v err=%v", st, err)
	}
	if st.State.ActiveHost != 1 {
		t.Fatalf("want host 1, got %d", st.State.ActiveHost)
	}

	resp, err := ipc.Call(cfg.SocketPath, ipc.Request{Op: ipc.OpSetHost, Host: 2})
	if err != nil || !resp.OK {
		t.Fatalf("set host: %+v %v", resp, err)
	}
	time.Sleep(200 * time.Millisecond)
	st, err = ipc.Call(cfg.SocketPath, ipc.Request{Op: ipc.OpState})
	if err != nil {
		t.Fatal(err)
	}
	if st.State.Connected {
		t.Fatal("expected disconnected after switching away")
	}
	if st.State.ActiveHost != 2 {
		t.Fatalf("active host=%d", st.State.ActiveHost)
	}

	// Cannot switch back without serial.
	resp, err = ipc.Call(cfg.SocketPath, ipc.Request{Op: ipc.OpSetHost, Host: 1})
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK {
		t.Fatal("expected switch-back failure while inactive")
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
	}
}
