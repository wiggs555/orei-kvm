package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
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

func init() {
	// AppKit ([NSApp run] inside systray) aborts with SIGTRAP unless it runs
	// on the process main thread. Pin this goroutine before it can migrate.
	runtime.LockOSThread()
}

var (
	flagPort   string
	flagBaud   int
	flagMyHost int
	flagSocket string
	flagMock   bool
	flagDirect bool
	flagYes    bool
	flagTray   bool
	cfg        config.Config
)

const trayChildEnv = "OREI_KVM_TRAY_CHILD"

// version is the CLI release.
const version = "0.1.3"

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
		Version:      version,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !flagTray {
				return cmd.Help()
			}
			return runDaemonAndTray()
		},
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
	root.Flags().BoolVar(&flagTray, "tray", false, "run the daemon and system tray in the background")

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
			if withTray {
				// systray must run on this goroutine (the locked main thread).
				return runDaemonAndTray()
			}
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			d := daemon.New(cfg, flagMock)
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
			if err := detachTrayFromTerminal(); err != nil {
				return err
			}
			if !ipc.IsDaemonUp(cfg.SocketPath) {
				return runDaemonAndTray()
			}
			return tray.Run(cfg.SocketPath, cfg.MyHost)
		},
	}
}

// runDaemonAndTray starts the daemon on a background goroutine and blocks in
// the tray on the caller, which must be the main OS thread on macOS.
func runDaemonAndTray() error {
	if err := detachTrayFromTerminal(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.New(cfg, flagMock).Start(ctx)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for !ipc.IsDaemonUp(cfg.SocketPath) {
		if time.Now().After(deadline) {
			stop()
			return fmt.Errorf("timed out waiting for daemon socket %s", cfg.SocketPath)
		}
		select {
		case err := <-errCh:
			if err != nil {
				return err
			}
			return fmt.Errorf("daemon exited before socket %s was ready", cfg.SocketPath)
		case <-time.After(50 * time.Millisecond):
		}
	}

	trayErr := tray.Run(cfg.SocketPath, cfg.MyHost)
	stop()
	dErr := <-errCh
	if trayErr != nil {
		return trayErr
	}
	return dErr
}

// shouldDetachTray reports whether this launch should fork a child and return
// the terminal. Launchd and an already-detached child stay in the foreground
// so the supervisor still tracks the process that owns the tray.
func shouldDetachTray(child bool, stdoutIsTTY bool) bool {
	return !child && stdoutIsTTY
}

func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// detachTrayFromTerminal starts a new session running this same command and
// exits the foreground process. The child keeps the tray on its main thread
// and exits when the tray quits.
func detachTrayFromTerminal() error {
	if !shouldDetachTray(os.Getenv(trayChildEnv) == "1", stdoutIsTTY()) {
		return nil
	}
	logPath := filepath.Join(filepath.Dir(cfg.SocketPath), "orei-kvm.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	cmd := exec.Command(os.Args[0], os.Args[1:]...)
	cmd.Env = append(os.Environ(), trayChildEnv+"=1")
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "orei-kvm tray running in background (pid %d, log %s)\n", cmd.Process.Pid, logPath)
	os.Exit(0)
	return nil
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
	var (
		cycleRX      bool
		cycleDelay   time.Duration
		cyclePort    int
		cycleRestore string
	)
	c := &cobra.Command{
		Use:   "host",
		Short: "Get or set the active USB host (1 or 2)",
	}
	c.PersistentFlags().BoolVar(&cycleRX, "cycle-rx", false, "power-cycle RX USB ports after switching")
	c.PersistentFlags().DurationVar(&cycleDelay, "cycle-delay", 15*time.Second, "how long RX ports stay off when using --cycle-rx")
	c.PersistentFlags().IntVar(&cyclePort, "cycle-port", -1, "RX USB port to cycle with --cycle-rx (0=all; default: config)")
	c.PersistentFlags().StringVar(&cycleRestore, "cycle-restore", "", "restore mode after --cycle-rx: on|follow")

	after := func(cmd *cobra.Command, host int) error {
		if err := setHost(host); err != nil {
			return err
		}
		if !cycleRX {
			return nil
		}
		return runUSBCycle(usbCycleOpts{
			rx:          true,
			port:        cyclePort,
			delay:       cycleDelay,
			delaySet:    cmd.Flags().Changed("cycle-delay") || (cmd.Parent() != nil && cmd.Parent().PersistentFlags().Changed("cycle-delay")),
			restore:     cycleRestore,
			afterSwitch: true,
		})
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
			return after(cmd, host)
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "1",
		Short: "Switch to host 1",
		RunE:  func(cmd *cobra.Command, args []string) error { return after(cmd, 1) },
	})
	c.AddCommand(&cobra.Command{
		Use:   "2",
		Short: "Switch to host 2",
		RunE:  func(cmd *cobra.Command, args []string) error { return after(cmd, 2) },
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
	c.AddCommand(cmdUSBDCycle(false))
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
	c.AddCommand(cmdUSBDCycle(true))
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
			fmt.Printf("port:           %q\n", cfg.Port)
			fmt.Printf("baud:           %d\n", cfg.Baud)
			fmt.Printf("my_host:        %d\n", cfg.MyHost)
			fmt.Printf("poll_interval:  %s\n", cfg.PollInterval)
			fmt.Printf("socket:         %s\n", cfg.SocketPath)
			fmt.Printf("port_patterns:  %v\n", cfg.PortPatterns)
			fmt.Printf("cycle_on_active: %v\n", cfg.CycleOnActive)
			fmt.Printf("cycle_side:     %s\n", cfg.CycleSide)
			fmt.Printf("cycle_port:     %d\n", cfg.CyclePort)
			fmt.Printf("cycle_delay:    %s\n", cfg.CycleDelay)
			fmt.Printf("cycle_restore:  %s\n", cfg.CycleRestore)
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

type usbCycleOpts struct {
	rx, tx      bool
	port        int // -1 = config default
	delay       time.Duration
	delaySet    bool
	restore     string
	afterSwitch bool
}

func cmdUSBDCycle(rx bool) *cobra.Command {
	var delay time.Duration
	var restore string
	side := "TX"
	use := "cycle [port]"
	if rx {
		side = "RX"
	}
	c := &cobra.Command{
		Use:   use,
		Short: "Power-cycle " + side + " USB device ports (off, wait, restore)",
		Long: `Force-off USB device port power, wait, then restore.

After a host switch, some HID devices (notably an Apple Magic Trackpad on
macOS) do not re-enumerate until the port is power-cycled. Keyboard-class
devices often switch cleanly without this.

Port 0 (default) cycles all ports on this side. Prefer the specific port the
trackpad is on so the keyboard stays up. Do not cycle the port that holds
the RS-232 adapter.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			port := -1
			if len(args) == 1 {
				var err error
				port, err = strconv.Atoi(args[0])
				if err != nil {
					return err
				}
			}
			return runUSBCycle(usbCycleOpts{
				rx:       rx,
				tx:       !rx,
				port:     port,
				delay:    delay,
				delaySet: cmd.Flags().Changed("delay"),
				restore:  restore,
			})
		},
	}
	c.Flags().DurationVar(&delay, "delay", 15*time.Second, "how long ports stay off")
	c.Flags().StringVar(&restore, "restore", "", "power mode after cycle: on|follow (default on)")
	return c
}

func runUSBCycle(opts usbCycleOpts) error {
	port := opts.port
	if port < 0 {
		port = cfg.CyclePort
	}
	delay := opts.delay
	if !opts.delaySet {
		delay = cfg.CycleDelay
	}
	restoreStr := opts.restore
	if restoreStr == "" {
		restoreStr = cfg.CycleRestore
	}
	if restoreStr == "" {
		restoreStr = "on"
	}
	restore, err := protocol.ParsePowerMode(restoreStr)
	if err != nil {
		return err
	}
	if err := protocol.ValidateCycleRestore(restore); err != nil {
		return err
	}
	if opts.rx {
		if err := protocol.ValidateRXPort(port); err != nil {
			return err
		}
	}
	if opts.tx {
		if err := protocol.ValidateTXPort(port); err != nil {
			return err
		}
	}

	sides := make([]string, 0, 2)
	if opts.rx {
		sides = append(sides, "RX")
	}
	if opts.tx {
		sides = append(sides, "TX")
	}
	fmt.Fprintf(os.Stderr, "power-cycling %s USB port %d: off %s, restore %s\n", strings.Join(sides, "+"), port, delay, restore)

	err = doUSBCycle(opts.rx, opts.tx, port, delay, restore)
	if err != nil && opts.afterSwitch {
		fmt.Fprintf(os.Stderr, "host switched; USB cycle failed: %v\nrun on the active host: orei-kvm rx-usbd cycle\n(or set cycle_on_active: true in config)\n", err)
		return nil
	}
	return err
}

func doUSBCycle(rx, tx bool, port int, delay time.Duration, restore protocol.PowerMode) error {
	useDaemon := !flagDirect && ipc.IsDaemonUp(cfg.SocketPath)
	if useDaemon {
		runSide := func(isRX bool) error {
			set := protocol.CmdSetTXUSBD
			if isRX {
				set = protocol.CmdSetRXUSBD
			}
			resp, err := run(set(port, int(protocol.PowerForceOff)))
			if resp != "" {
				fmt.Println(resp)
			}
			if err != nil {
				return err
			}
			if delay > 0 {
				time.Sleep(delay)
			}
			resp, err = run(set(port, int(restore)))
			if resp != "" {
				fmt.Println(resp)
			}
			return err
		}
		if rx {
			if err := runSide(true); err != nil {
				return err
			}
		}
		if tx {
			return runSide(false)
		}
		return nil
	}

	bus, closer, err := openDirect()
	if err != nil {
		return err
	}
	defer closer()
	dev := device.New(bus)
	if rx {
		resp, err := dev.CycleRXUSBD(port, delay, restore)
		if resp != "" {
			fmt.Println(resp)
		}
		if err != nil {
			return err
		}
	}
	if tx {
		resp, err := dev.CycleTXUSBD(port, delay, restore)
		if resp != "" {
			fmt.Println(resp)
		}
		return err
	}
	return nil
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
