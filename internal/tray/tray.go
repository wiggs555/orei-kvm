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

		apply := func(st ipc.State) {
			label := statusLabel(st)
			mStatus.SetTitle(label)
			systray.SetTitle(barTitle(st))
			systray.SetTooltip("OREI KVM — " + label)

			mHost1.Uncheck()
			mHost2.Uncheck()
			switch st.ActiveHost {
			case 1:
				mHost1.Check()
				systray.SetIcon(iconHost1)
			case 2:
				mHost2.Check()
				systray.SetIcon(iconHost2)
			default:
				if st.Connected {
					systray.SetIcon(iconUnknown)
				} else {
					systray.SetIcon(iconInactive)
				}
			}

			if st.Connected {
				mHost1.Enable()
				mHost2.Enable()
			} else {
				mHost1.Disable()
				mHost2.Disable()
			}
		}

		refresh := func() {
			resp, err := ipc.Call(socketPath, ipc.Request{Op: ipc.OpState})
			if err != nil {
				mStatus.SetTitle("Daemon: " + err.Error())
				systray.SetTitle("OREI ?")
				systray.SetTooltip("OREI KVM — " + err.Error())
				systray.SetIcon(iconInactive)
				mHost1.Disable()
				mHost2.Disable()
				return
			}
			if resp.State != nil {
				apply(*resp.State)
				return
			}
			if resp.Error != "" {
				mStatus.SetTitle(resp.Error)
				systray.SetTitle("OREI ?")
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
