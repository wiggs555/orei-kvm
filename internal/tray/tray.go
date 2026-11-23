//go:build !notray

package tray

import (
	_ "embed"
	"fmt"
	"time"

	"github.com/energye/systray"
	"github.com/wiggs555/orei-kvm/internal/ipc"
)

//go:embed icons/host1.png
var iconHost1 []byte

//go:embed icons/host2.png
var iconHost2 []byte

//go:embed icons/inactive.png
var iconInactive []byte

//go:embed icons/unknown.png
var iconUnknown []byte

type iconKind int

const (
	iconKindUnknown iconKind = iota
	iconKindHost1
	iconKindHost2
	iconKindInactive
)

// viewState is the subset of daemon state that drives tray chrome.
// Comparing this avoids re-issuing SetIcon/SetTitle/menu mutations every poll
// when nothing visible changed — those native calls can retain memory in
// AppKit / StatusNotifier over long uptimes.
type viewState struct {
	connected  bool
	activeHost int
	iAmActive  bool
	lastError  string
	daemonDown bool
	daemonErr  string
}

func viewFromState(st ipc.State) viewState {
	return viewState{
		connected:  st.Connected,
		activeHost: st.ActiveHost,
		iAmActive:  st.IAmActive,
		lastError:  st.LastError,
	}
}

func iconForView(v viewState) iconKind {
	if v.daemonDown {
		return iconKindInactive
	}
	switch v.activeHost {
	case 1:
		return iconKindHost1
	case 2:
		return iconKindHost2
	default:
		if v.connected {
			return iconKindUnknown
		}
		return iconKindInactive
	}
}

func setTrayIcon(kind iconKind) {
	switch kind {
	case iconKindHost1:
		systray.SetIcon(iconHost1)
	case iconKindHost2:
		systray.SetIcon(iconHost2)
	case iconKindInactive:
		systray.SetIcon(iconInactive)
	default:
		systray.SetIcon(iconUnknown)
	}
}

// Run starts the system tray UI, talking to an already-running daemon via socket.
// On macOS this must be called from the main goroutine: AppKit traps (SIGTRAP)
// if [NSApp run] is entered from any other thread.
func Run(socketPath string, myHost int) error {
	onReady := func() {
		systray.SetTitle("OREI …")
		systray.SetTooltip("OREI KVM")
		systray.SetIcon(iconUnknown)

		// Do not install click handlers. On macOS those detach the status menu
		// (ShowMenu sets it, then clears it), so the polled host never stays visible.
		mStatus := systray.AddMenuItem("Status: …", "Current KVM status")
		mStatus.Disable()
		systray.AddSeparator()
		mHost1 := systray.AddMenuItemCheckbox("Host 1", "Switch to host 1", false)
		mHost2 := systray.AddMenuItemCheckbox("Host 2", "Switch to host 2", false)
		systray.AddSeparator()
		mRefresh := systray.AddMenuItem("Refresh", "Re-query daemon / serial")
		mQuit := systray.AddMenuItem("Quit", "Quit the tray")
		systray.CreateMenu()

		var (
			prev     viewState
			havePrev bool
			prevIcon iconKind = iconKindUnknown
		)

		apply := func(v viewState) {
			if havePrev && prev == v {
				return
			}

			var label, title, tip string
			if v.daemonDown {
				label = "Daemon: " + v.daemonErr
				title = "OREI ?"
				tip = "OREI KVM — " + v.daemonErr
			} else {
				st := ipc.State{
					Connected:  v.connected,
					ActiveHost: v.activeHost,
					IAmActive:  v.iAmActive,
					LastError:  v.lastError,
				}
				label = statusLabel(st)
				title = barTitle(st)
				tip = "OREI KVM — " + label
			}
			mStatus.SetTitle(label)
			systray.SetTitle(title)
			systray.SetTooltip(tip)

			icon := iconForView(v)
			if !havePrev || icon != prevIcon {
				setTrayIcon(icon)
				prevIcon = icon
			}

			if !havePrev || prev.activeHost != v.activeHost || prev.daemonDown != v.daemonDown {
				mHost1.Uncheck()
				mHost2.Uncheck()
				switch v.activeHost {
				case 1:
					mHost1.Check()
				case 2:
					mHost2.Check()
				}
			}

			wantEnabled := v.connected && !v.daemonDown
			wasEnabled := havePrev && prev.connected && !prev.daemonDown
			if !havePrev || wantEnabled != wasEnabled {
				if wantEnabled {
					mHost1.Enable()
					mHost2.Enable()
				} else {
					mHost1.Disable()
					mHost2.Disable()
				}
			}

			prev = v
			havePrev = true
		}

		refresh := func() {
			resp, err := ipc.Call(socketPath, ipc.Request{Op: ipc.OpState})
			if err != nil {
				apply(viewState{daemonDown: true, daemonErr: err.Error()})
				return
			}
			if resp.State != nil {
				apply(viewFromState(*resp.State))
				return
			}
			if resp.Error != "" {
				apply(viewState{daemonDown: true, daemonErr: resp.Error})
			}
		}

		mHost1.Click(func() {
			_, _ = ipc.Call(socketPath, ipc.Request{Op: ipc.OpSetHost, Host: 1})
			refresh()
		})
		mHost2.Click(func() {
			_, _ = ipc.Call(socketPath, ipc.Request{Op: ipc.OpSetHost, Host: 2})
			refresh()
		})
		mRefresh.Click(func() {
			_, _ = ipc.Call(socketPath, ipc.Request{Op: ipc.OpReconnect})
			refresh()
		})
		mQuit.Click(func() {
			systray.Quit()
		})

		go func() {
			refresh()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				refresh()
			}
		}()
		_ = myHost
	}

	systray.Run(onReady, func() {})
	return nil
}

// statusLabel is the menu line for a daemon state snapshot.
func statusLabel(st ipc.State) string {
	switch {
	case st.Connected && st.ActiveHost > 0:
		label := fmt.Sprintf("Active: Host %d", st.ActiveHost)
		if st.IAmActive {
			label += " (this machine)"
		}
		return label
	case !st.Connected && st.LastError != "":
		return st.LastError
	case !st.Connected && st.ActiveHost > 0:
		return fmt.Sprintf("Inactive — serial gone (likely Host %d)", st.ActiveHost)
	case !st.Connected:
		return "Inactive — serial not present"
	default:
		return "Status unknown"
	}
}

// barTitle is the menu-bar text, short enough to read without opening the menu.
func barTitle(st ipc.State) string {
	if st.Connected && st.ActiveHost > 0 {
		if st.IAmActive {
			return fmt.Sprintf("OREI H%d", st.ActiveHost)
		}
		return fmt.Sprintf("H%d", st.ActiveHost)
	}
	if !st.Connected {
		return "OREI off"
	}
	return "OREI …"
}
