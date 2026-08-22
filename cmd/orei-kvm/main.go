package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/wiggs555/orei-kvm/internal/config"
	"github.com/wiggs555/orei-kvm/internal/daemon"
	"github.com/wiggs555/orei-kvm/internal/device"
	"github.com/wiggs555/orei-kvm/internal/ipc"
	"github.com/wiggs555/orei-kvm/internal/protocol"
	"github.com/wiggs555/orei-kvm/internal/serial"
	"github.com/wiggs555/orei-kvm/internal/tray"
)

var (
	flagPort     string
	flagBaud     int
	flagMyHost   int
	flagSocket   string
	flagMock     bool
	flagDirect   bool
	flagYes      bool
	cfg          config.Config
)

func main() {
	root := &cobra.Command{
		Use:   "orei-kvm",
		Short: "Control OREI USB3-EX2H330R-K over RS-232",
		Long: `CLI and daemon for the OREI USB3-EX2H330R-K dual-host USB/HDMI extender.

Implements the full ASCII RS-232 command set from the device manual.
Because the serial adapter rides a switched USB port, it is only present
on the currently selected host — the daemon polls for port presence and
treats disappearance as "this machine is inactive".`,
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			var err error
			cfg, err = config.Load()
			if err != nil {
				return err
			}
			if flagPort != "" {
				cfg.Port = flagPort
			}
			if flagBaud != 0 {
				cfg.Baud = flagBaud
			}
			if flagMyHost != 0 {
				cfg.MyHost = flagMyHost
			}
			if flagSocket != "" {
				cfg.SocketPath = flagSocket
			}
			return nil
		},
	}

	root.PersistentFlags().StringVar(&flagPort, "port", "", "serial device path (default: auto-detect)")
	root.PersistentFlags().IntVar(&flagBaud, "baud", 0, "serial baud rate (default 115200)")
	root.PersistentFlags().IntVar(&flagMyHost, "my-host", 0, "which KVM host this machine is (1 or 2)")
	root.PersistentFlags().StringVar(&flagSocket, "socket", "", "daemon Unix socket path")
	root.PersistentFlags().BoolVar(&flagMock, "mock", false, "use in-process mock device (no hardware)")
	root.PersistentFlags().BoolVar(&flagDirect, "direct", false, "talk to serial directly (skip daemon)")

	root.AddCommand(
		cmdDaemon(),
		cmdTray(),
		cmdStatus(),
		cmdHost(),
		cmdKey(),
		cmdBaud(),
		cmdAutoswitch(),
		cmdUSB5V(),
		cmdFW(),
		cmdReboot(),
		cmdReset(),
		cmdStatusDevice(),
		cmdTXUSBD(),
		cmdRXUSBD(),
		cmdHDBT(),
		cmdRaw(),
		cmdPorts(),
		cmdDeviceHelp(),
		cmdConfig(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func cmdDaemon() *cobra.Command {
	var withTray bool
	c := &cobra.Command{
		Use:   "daemon",
		Short: "Run background daemon (serial polling + IPC)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			d := daemon.New(cfg, flagMock)
			if withTray {
				go func() {
					// Give the socket a moment to come up.
					time.Sleep(200 * time.Millisecond)
					_ = tray.Run(cfg.SocketPath, cfg.MyHost)
					stop()
				}()
			}
			return d.Start(ctx)
		},
	}
	c.Flags().BoolVar(&withTray, "tray", false, "also show system tray UI")
	return c
}

func cmdTray() *cobra.Command {
	return &cobra.Command{
		Use:   "tray",
		Short: "Show system tray (starts daemon if needed)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !ipc.IsDaemonUp(cfg.SocketPath) {
				ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
				defer stop()
				d := daemon.New(cfg, flagMock)
				errCh := make(chan error, 1)
				go func() { errCh <- d.Start(ctx) }()
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					if ipc.IsDaemonUp(cfg.SocketPath) {
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
				go func() {
					_ = tray.Run(cfg.SocketPath, cfg.MyHost)
					stop()
				}()
				return <-errCh
			}
			return tray.Run(cfg.SocketPath, cfg.MyHost)
		},
	}
}

func cmdStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon / connection / active host status",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getState()
			if err != nil {
				return err
			}
			fmt.Printf("connected:   %v\n", st.Connected)
			fmt.Printf("port:        %s\n", st.PortPath)
			fmt.Printf("my_host:     %d\n", st.MyHost)
			fmt.Printf("active_host: %d\n", st.ActiveHost)
			fmt.Printf("i_am_active: %v\n", st.IAmActive)
			if st.LastError != "" {
				fmt.Printf("last_error:  %s\n", st.LastError)
			}
			if st.LastRaw != "" {
				fmt.Printf("last_raw:    %s\n", st.LastRaw)
			}
			fmt.Printf("updated_at:  %s\n", st.UpdatedAt.Format(time.RFC3339))
			return nil
		},
	}
}

func cmdHost() *cobra.Command {
	c := &cobra.Command{
		Use:   "host",
		Short: "Get or set the active USB host (1 or 2)",
	}
	c.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Query active host",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := run("get input")
			if err != nil {
				return err
			}
			if host, err := protocol.ParseHost(resp); err == nil {
				fmt.Printf("host %d\n", host)
			}
			fmt.Println(resp)
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "set [1|2]",
		Short: "Switch active host",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			return setHost(host)
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "1",
		Short: "Switch to host 1",
		RunE:  func(cmd *cobra.Command, args []string) error { return setHost(1) },
	})
	c.AddCommand(&cobra.Command{
		Use:   "2",
		Short: "Switch to host 2",
		RunE:  func(cmd *cobra.Command, args []string) error { return setHost(2) },
	})
	return c
}

func cmdKey() *cobra.Command {
	c := &cobra.Command{Use: "key", Short: "Front-panel key lock"}
	c.AddCommand(simpleGet("get", "get key"))
	c.AddCommand(simpleSet("on", "set key on"))
	c.AddCommand(simpleSet("off", "set key off"))
	return c
}

func cmdBaud() *cobra.Command {
	c := &cobra.Command{Use: "baud", Short: "RS-232 baud rate"}
	c.AddCommand(simpleGet("get", "get baud"))
	c.AddCommand(&cobra.Command{
		Use:   "set [rate|index]",
		Short: "Set baud (4800…115200 or index 1-6)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			idx, err := protocol.BaudIndex(n)
			if err != nil {
				return err
			}
			return printRun(protocol.CmdSetBaud(idx))
		},
	})
	return c
}

func cmdAutoswitch() *cobra.Command {
	c := &cobra.Command{Use: "autoswitch", Short: "USB 5V auto-switching"}
	c.AddCommand(simpleGet("get", "get autoswitch"))
	c.AddCommand(simpleSet("on", "set autoswitch on"))
	c.AddCommand(simpleSet("off", "set autoswitch off"))
	return c
}

func cmdUSB5V() *cobra.Command {
	return &cobra.Command{
		Use:   "usb5v [0|1|2]",
		Short: "Get USB host 5V presence (0=all)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			x := 0
			if len(args) == 1 {
				var err error
				x, err = strconv.Atoi(args[0])
				if err != nil {
					return err
				}
			}
			if err := protocol.ValidateUSB5V(x); err != nil {
				return err
			}
			return printRun(protocol.CmdGetUSB5V(x))
		},
	}
}

func cmdFW() *cobra.Command {
	return &cobra.Command{
		Use:   "fw",
		Short: "Get firmware versions",
		RunE:  func(cmd *cobra.Command, args []string) error { return printRun(protocol.CmdGetFW()) },
	}
}

func cmdReboot() *cobra.Command {
	return &cobra.Command{
		Use:   "reboot",
		Short: "Reboot the device",
		RunE:  func(cmd *cobra.Command, args []string) error { return printRun(protocol.CmdReboot()) },
	}
}

func cmdReset() *cobra.Command {
	c := &cobra.Command{
		Use:   "reset",
		Short: "Factory reset (requires --yes)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !flagYes {
				return fmt.Errorf("refusing factory reset without --yes")
			}
			return printRunConfirm(protocol.CmdReset(), protocol.CmdResetConfirm())
		},
	}
	c.Flags().BoolVar(&flagYes, "yes", false, "confirm factory reset")
	return c
}

func cmdStatusDevice() *cobra.Command {
	return &cobra.Command{
		Use:   "device-status",
		Short: "Run device get status",
		RunE:  func(cmd *cobra.Command, args []string) error { return printRun(protocol.CmdGetStatus()) },
	}
}

func cmdTXUSBD() *cobra.Command {
	c := &cobra.Command{Use: "tx-usbd", Short: "TX USB device port power"}
	c.AddCommand(&cobra.Command{
		Use:   "get [port]",
		Short: "Get TX USB device power (0=all, 1-2)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			port := 0
			if len(args) == 1 {
				var err error
				port, err = strconv.Atoi(args[0])
				if err != nil {
					return err
				}
			}
			return printRun(protocol.CmdGetTXUSBD(port))
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "set <port> <off|follow|on>",
		Short: "Set TX USB device power",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			port, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			mode, err := protocol.ParsePowerMode(args[1])
			if err != nil {
				return err
			}
			return printRun(protocol.CmdSetTXUSBD(port, int(mode)))
		},
	})
	return c
}

func cmdRXUSBD() *cobra.Command {
	c := &cobra.Command{Use: "rx-usbd", Short: "RX USB device port power"}
	c.AddCommand(&cobra.Command{
		Use:   "get [port]",
		Short: "Get RX USB device power (0=all, 1-4)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			port := 0
			if len(args) == 1 {
				var err error
				port, err = strconv.Atoi(args[0])
				if err != nil {
					return err
				}
			}
			return printRun(protocol.CmdGetRXUSBD(port))
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "set <port> <off|follow|on>",
		Short: "Set RX USB device power",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			port, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			mode, err := protocol.ParsePowerMode(args[1])
			if err != nil {
				return err
			}
			return printRun(protocol.CmdSetRXUSBD(port, int(mode)))
		},
	})
	return c
}

func cmdHDBT() *cobra.Command {
	return &cobra.Command{
		Use:   "hdbt-update",
		Short: "Set service port to HDBT UART for FW update",
		RunE:  func(cmd *cobra.Command, args []string) error { return printRun(protocol.CmdHDBTUpdate()) },
	}
}

func cmdRaw() *cobra.Command {
	return &cobra.Command{
		Use:   "raw [command...]",
		Short: "Send a raw ASCII command",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return printRun(strings.Join(args, " "))
		},
	}
}

func cmdPorts() *cobra.Command {
	return &cobra.Command{
		Use:   "ports",
		Short: "List serial ports",
		RunE: func(cmd *cobra.Command, args []string) error {
			ports, err := serial.ListPorts()
			if err != nil {
				return err
			}
			if len(ports) == 0 {
				fmt.Println("no serial ports found")
				return nil
			}
			for _, p := range ports {
				extra := p.Description
				if p.VID != "" {
					extra = fmt.Sprintf("%s VID=%s PID=%s", extra, p.VID, p.PID)
				}
				fmt.Printf("%s\t%s\n", p.Name, strings.TrimSpace(extra))
			}
			return nil
		},
	}
}

func cmdDeviceHelp() *cobra.Command {
	return &cobra.Command{
		Use:   "device-help",
		Short: "Ask the device for its command list (?)",
		RunE:  func(cmd *cobra.Command, args []string) error { return printRun(protocol.CmdHelp()) },
	}
}

func cmdConfig() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Show or write config file"}
	c.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print config file path",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := config.Path()
			if err != nil {
				return err
			}
			fmt.Println(p)
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show effective config",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("port:          %q\n", cfg.Port)
			fmt.Printf("baud:          %d\n", cfg.Baud)
			fmt.Printf("my_host:       %d\n", cfg.MyHost)
			fmt.Printf("poll_interval: %s\n", cfg.PollInterval)
			fmt.Printf("socket:        %s\n", cfg.SocketPath)
			fmt.Printf("port_patterns: %v\n", cfg.PortPatterns)
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Write default config file",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cfg.Save(); err != nil {
				return err
			}
			p, _ := config.Path()
			fmt.Println("wrote", p)
			return nil
		},
	})
	return c
}

func simpleGet(use, cmdStr string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: cmdStr,
		RunE:  func(cmd *cobra.Command, args []string) error { return printRun(cmdStr) },
	}
}

func simpleSet(use, cmdStr string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: cmdStr,
		RunE:  func(cmd *cobra.Command, args []string) error { return printRun(cmdStr) },
	}
}

func setHost(host int) error {
	if err := protocol.ValidateHost(host); err != nil {
		return err
	}
	if flagDirect || flagMock && !ipc.IsDaemonUp(cfg.SocketPath) {
		resp, err := directCommand(protocol.CmdSetInput(host))
		if err != nil {
			// Switching away often kills the serial link mid-response.
			if resp != "" {
				fmt.Println(resp)
			}
			fmt.Printf("switched toward host %d (serial may have disconnected)\n", host)
			return nil
		}
		fmt.Println(resp)
		return nil
	}
	if !ipc.IsDaemonUp(cfg.SocketPath) {
		return fmt.Errorf("daemon not running; start with: orei-kvm daemon  (or pass --direct)")
	}
	resp, err := ipc.Call(cfg.SocketPath, ipc.Request{Op: ipc.OpSetHost, Host: host})
	if err != nil {
		return err
	}
	if resp.Data != "" {
		fmt.Println(resp.Data)
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	if resp.State != nil {
		fmt.Printf("active_host=%d connected=%v\n", resp.State.ActiveHost, resp.State.Connected)
	}
	return nil
}

func printRun(cmdStr string) error {
	resp, err := run(cmdStr)
	if resp != "" {
		fmt.Println(resp)
	}
	return err
}

func printRunConfirm(cmdStr, confirm string) error {
	if flagDirect || !ipc.IsDaemonUp(cfg.SocketPath) {
		bus, closer, err := openDirect()
		if err != nil {
			return err
		}
		defer closer()
		resp, err := bus.CommandWithConfirm(cmdStr, confirm)
		if resp != "" {
			fmt.Println(resp)
		}
		return err
	}
	// Daemon auto-confirms set reset.
	return printRun(cmdStr)
}

func run(cmdStr string) (string, error) {
	useDaemon := !flagDirect && ipc.IsDaemonUp(cfg.SocketPath)
	if useDaemon {
		resp, err := ipc.Call(cfg.SocketPath, ipc.Request{Op: ipc.OpCommand, Cmd: cmdStr})
		if err != nil {
			return "", err
		}
		if !resp.OK {
			return resp.Data, fmt.Errorf("%s", resp.Error)
		}
		return resp.Data, nil
	}
	if flagDirect {
		return directCommand(cmdStr)
	}
	return "", fmt.Errorf("daemon not running; start with: orei-kvm daemon  (or pass --direct --port …)")
}

func getState() (ipc.State, error) {
	if flagDirect {
		bus, closer, err := openDirect()
		if err != nil {
			return ipc.State{MyHost: cfg.MyHost, LastError: err.Error()}, nil
		}
		defer closer()
		resp, err := bus.Command(protocol.CmdGetInput())
		st := ipc.State{Connected: err == nil, MyHost: cfg.MyHost, PortPath: cfg.Port, LastRaw: resp, UpdatedAt: time.Now()}
		if err == nil {
			if h, perr := protocol.ParseHost(resp); perr == nil {
				st.ActiveHost = h
				st.IAmActive = h == cfg.MyHost
			}
		} else {
			st.LastError = err.Error()
		}
		return st, nil
	}
	if !ipc.IsDaemonUp(cfg.SocketPath) {
		return ipc.State{}, fmt.Errorf("daemon not running; start with: orei-kvm daemon")
	}
	resp, err := ipc.Call(cfg.SocketPath, ipc.Request{Op: ipc.OpState})
	if err != nil {
		return ipc.State{}, err
	}
	if resp.State == nil {
		return ipc.State{}, fmt.Errorf("empty state")
	}
	return *resp.State, nil
}

func directCommand(cmdStr string) (string, error) {
	bus, closer, err := openDirect()
	if err != nil {
		return "", err
	}
	defer closer()
	return bus.Command(cmdStr)
}

func openDirect() (device.Bus, func(), error) {
	if flagMock {
		// For one-shot direct mock without daemon, use a throwaway mock.
		// Prefer attaching to daemon mock when available.
		return nil, nil, fmt.Errorf("direct --mock requires the mock daemon; run: orei-kvm --mock daemon")
	}
	client := serial.NewClient(cfg.Baud)
	path, err := serial.FindPort(cfg.Port, cfg.PortPatterns)
	if err != nil {
		return nil, nil, err
	}
	if err := client.Open(path); err != nil {
		return nil, nil, err
	}
	cfg.Port = path
	return client, func() { _ = client.Close() }, nil
}
