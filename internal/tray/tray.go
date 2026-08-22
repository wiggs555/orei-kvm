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
func Run(socketPath string, myHost int) error {
	onReady := func() {
		systray.SetTitle("OREI")
		systray.SetTooltip("OREI KVM")
		systray.SetIcon(iconUnknown)
		systray.SetOnClick(func(menu systray.IMenu) {
			if menu != nil {
				_ = menu.ShowMenu()
			}
		})
		systray.SetOnRClick(func(menu systray.IMenu) {
			if menu != nil {
				_ = menu.ShowMenu()
			}
		})
		systray.CreateMenu()

		mStatus := systray.AddMenuItem("Status: …", "Current KVM status")
		mStatus.Disable()
		systray.AddSeparator()
		mHost1 := systray.AddMenuItemCheckbox("Host 1", "Switch to host 1", false)
		mHost2 := systray.AddMenuItemCheckbox("Host 2", "Switch to host 2", false)
		systray.AddSeparator()
		mRefresh := systray.AddMenuItem("Refresh", "Re-query daemon / serial")
		mQuit := systray.AddMenuItem("Quit", "Quit tray (daemon keeps running)")

		apply := func(st ipc.State) {
			var label string
			switch {
			case st.Connected && st.ActiveHost > 0:
				label = fmt.Sprintf("Active: Host %d", st.ActiveHost)
				if st.IAmActive {
					label += " (this machine)"
				}
			case !st.Connected:
				label = fmt.Sprintf("Inactive — serial gone (likely Host %d)", st.ActiveHost)
				if st.ActiveHost == 0 {
					label = "Inactive — serial not present"
				}
			default:
				label = "Status unknown"
			}
			mStatus.SetTitle(label)
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
				mStatus.SetTitle("Daemon not running")
				systray.SetIcon(iconInactive)
				mHost1.Disable()
				mHost2.Disable()
				return
			}
			if resp.State != nil {
				apply(*resp.State)
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
