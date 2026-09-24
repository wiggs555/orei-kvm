package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wiggs555/orei-kvm/internal/config"
	"github.com/wiggs555/orei-kvm/internal/device"
	"github.com/wiggs555/orei-kvm/internal/ipc"
	"github.com/wiggs555/orei-kvm/internal/mock"
	"github.com/wiggs555/orei-kvm/internal/protocol"
	"github.com/wiggs555/orei-kvm/internal/serial"
)

// Transport is the minimal serial surface used by the daemon.
type Transport interface {
	Open(path string) error
	Close() error
	Connected() bool
	Path() string
	Command(cmd string) (string, error)
	CommandWithConfirm(cmd, confirm string) (string, error)
}

// Daemon owns the serial connection, polls host state, and serves IPC.
type Daemon struct {
	cfg      config.Config
	mockMode bool
	mockDev  *mock.Device
	mockCli  *mock.Client
	client   *serial.Client
	dev      *device.Device

	mu    sync.RWMutex
	state ipc.State

	cycleMu     sync.Mutex
	sawInactive bool
	cycling     bool

	ln     net.Listener
	cancel context.CancelFunc
	log    *log.Logger
}

func New(cfg config.Config, mockMode bool) *Daemon {
	d := &Daemon{
		cfg:      cfg,
		mockMode: mockMode,
		log:      log.New(os.Stderr, "orei-kvm: ", log.LstdFlags),
	}
	d.state = ipc.State{
		MyHost:      cfg.MyHost,
		PollEveryMS: int(cfg.PollInterval.Milliseconds()),
		UpdatedAt:   time.Now(),
	}
	if mockMode {
		d.mockDev = mock.New()
		// Start with this machine active so first connect works.
		d.mockDev.SetHost(cfg.MyHost)
		d.mockCli = mock.NewClient(d.mockDev, cfg.MyHost)
		d.dev = device.New(d.mockCli)
	} else {
		d.client = serial.NewClient(cfg.Baud)
		d.dev = device.New(d.client)
	}
	return d
}

func (d *Daemon) transport() Transport {
	if d.mockMode {
		return d.mockCli
	}
	return d.client
}

func (d *Daemon) Start(ctx context.Context) error {
	if err := config.EnsureRuntimeDir(d.cfg.SocketPath); err != nil {
		return err
	}
	if err := ipc.RemoveStaleSocket(d.cfg.SocketPath); err != nil {
		return err
	}
	ln, err := net.Listen("unix", d.cfg.SocketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(d.cfg.SocketPath, 0o600); err != nil {
		_ = ln.Close()
		return err
	}
	d.ln = ln
	ctx, d.cancel = context.WithCancel(ctx)

	d.tryConnect()
	go d.pollLoop(ctx)
	go d.serve(ctx)

	d.log.Printf("daemon listening on %s (mock=%v my_host=%d)", d.cfg.SocketPath, d.mockMode, d.cfg.MyHost)
	<-ctx.Done()
	return d.Shutdown()
}

func (d *Daemon) Shutdown() error {
	if d.cancel != nil {
		d.cancel()
	}
	if d.ln != nil {
		_ = d.ln.Close()
	}
	_ = d.transport().Close()
	_ = os.Remove(d.cfg.SocketPath)
	return nil
}

func (d *Daemon) State() ipc.State {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.state
}

func (d *Daemon) setState(mut func(*ipc.State)) {
	d.mu.Lock()
	mut(&d.state)
	d.state.UpdatedAt = time.Now()
	d.state.MyHost = d.cfg.MyHost
	d.state.PollEveryMS = int(d.cfg.PollInterval.Milliseconds())
	st := d.state
	d.mu.Unlock()
	d.considerUSBCycle(st)
}

func (d *Daemon) considerUSBCycle(st ipc.State) {
	inactive := !st.Connected || !st.IAmActive
	d.cycleMu.Lock()
	if inactive {
		d.sawInactive = true
		d.cycleMu.Unlock()
		return
	}
	should := d.cfg.CycleOnActive && d.sawInactive && !d.cycling
	if should {
		d.sawInactive = false
		d.cycling = true
	}
	d.cycleMu.Unlock()
	if should {
		go d.cycleUSBAfterSwitch()
	}
}

func (d *Daemon) cycleUSBAfterSwitch() {
	defer func() {
		d.cycleMu.Lock()
		d.cycling = false
		d.cycleMu.Unlock()
	}()
	delay := d.cfg.CycleDelay
	if delay < 0 {
		delay = 15 * time.Second
	}
	side, err := config.NormalizeCycleSide(d.cfg.CycleSide)
	if err != nil {
		d.log.Printf("cycle_on_active: %v", err)
		return
	}
	restore, err := protocol.ParsePowerMode(d.cfg.CycleRestore)
	if err != nil {
		d.log.Printf("cycle_on_active: invalid cycle_restore: %v", err)
		return
	}
	if err := protocol.ValidateCycleRestore(restore); err != nil {
		d.log.Printf("cycle_on_active: %v", err)
		return
	}
	port := d.cfg.CyclePort
	d.log.Printf("became active: power-cycling %s USB port %d (off %s, restore %s)", side, port, delay, restore)

	doRX := side == "rx" || side == "both"
	doTX := side == "tx" || side == "both"
	if doRX {
		if _, err := d.dev.SetRXUSBD(port, protocol.PowerForceOff); err != nil {
			d.log.Printf("cycle_on_active: RX off: %v", err)
			return
		}
	}
	if doTX {
		if _, err := d.dev.SetTXUSBD(port, protocol.PowerForceOff); err != nil {
			d.log.Printf("cycle_on_active: TX off: %v", err)
			return
		}
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	if doRX {
		if resp, err := d.dev.SetRXUSBD(port, restore); err != nil {
			d.log.Printf("cycle_on_active: RX restore: %v", err)
		} else if resp != "" {
			d.log.Printf("cycle_on_active: RX %s", strings.ReplaceAll(resp, "\n", " / "))
		}
	}
	if doTX {
		if resp, err := d.dev.SetTXUSBD(port, restore); err != nil {
			d.log.Printf("cycle_on_active: TX restore: %v", err)
		} else if resp != "" {
			d.log.Printf("cycle_on_active: TX %s", strings.ReplaceAll(resp, "\n", " / "))
		}
	}
}

func (d *Daemon) tryConnect() {
	t := d.transport()
	_ = t.Close()

	path := d.cfg.Port
	if d.mockMode {
		path = "mock://orei"
	} else {
		var err error
		path, err = serial.FindPort(d.cfg.Port, d.cfg.PortPatterns)
		if err != nil {
			d.setState(func(s *ipc.State) {
				s.Connected = false
				s.IAmActive = false
				s.ActiveHost = 0
				s.PortPath = d.cfg.Port
				s.LastError = err.Error()
			})
			return
		}
	}
	if err := t.Open(path); err != nil {
		d.setState(func(s *ipc.State) {
			s.Connected = false
			s.IAmActive = false
			// If port missing, this machine is likely not the active host.
			s.ActiveHost = 0
			s.PortPath = path
			s.LastError = err.Error()
		})
		return
	}
	// Remember the callout path so the next poll reads this device instead of
	// searching again and closing it when enumeration hiccups.
	if d.cfg.Port == "" {
		d.cfg.Port = path
	}
	d.refreshFromDevice("connect")
}

func (d *Daemon) refreshFromDevice(reason string) {
	t := d.transport()
	if !t.Connected() {
		d.setState(func(s *ipc.State) {
			s.Connected = false
			s.IAmActive = false
		})
		return
	}
	resp, err := t.Command(protocol.CmdGetInput())
	if err != nil {
		_ = t.Close()
		d.setState(func(s *ipc.State) {
			s.Connected = false
			s.IAmActive = false
			s.LastError = err.Error()
		})
		return
	}
	host, perr := protocol.ParseHost(resp)
	d.setState(func(s *ipc.State) {
		s.Connected = true
		s.PortPath = t.Path()
		s.LastRaw = resp
		s.LastError = ""
		if perr == nil {
			s.ActiveHost = host
			s.IAmActive = host == d.cfg.MyHost
		} else {
			// Connected implies we are active host even if parse fails.
			s.IAmActive = true
			s.ActiveHost = d.cfg.MyHost
			s.LastError = perr.Error()
		}
	})
	_ = reason
}

func (d *Daemon) pollLoop(ctx context.Context) {
	interval := d.cfg.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.pollOnce()
		}
	}
}

func (d *Daemon) pollOnce() {
	if d.mockMode {
		if d.mockCli.PortPresent() {
			if !d.mockCli.Connected() {
				d.tryConnect()
				return
			}
			d.refreshFromDevice("poll")
			return
		}
		_ = d.mockCli.Close()
		d.setState(func(s *ipc.State) {
			s.Connected = false
			s.IAmActive = false
			// Other host is active when our serial is gone.
			if d.cfg.MyHost == 1 {
				s.ActiveHost = 2
			} else {
				s.ActiveHost = 1
			}
			s.LastError = "serial adapter not present (this host is inactive)"
		})
		return
	}

	// An open port stays open. macOS often drops the callout device from the
	// IOKit list while it is open, and a background poll next to AppKit can
	// see an empty list. Either one made the tray report inactive while a
	// fresh --direct command still opened the same adapter.
	if d.client.Connected() {
		d.refreshFromDevice("poll")
		return
	}

	path := d.cfg.Port
	if path == "" {
		found, err := serial.FindPort("", d.cfg.PortPatterns)
		if err != nil {
			d.setState(func(s *ipc.State) {
				s.Connected = false
				s.IAmActive = false
				if d.cfg.MyHost == 1 {
					s.ActiveHost = 2
				} else {
					s.ActiveHost = 1
				}
				s.LastError = err.Error()
			})
			return
		}
		path = found
		d.cfg.Port = path
	}
	d.tryConnect()
}

func (d *Daemon) serve(ctx context.Context) {
	for {
		conn, err := d.ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				return
			}
		}
		go d.handleConn(conn)
	}
}

func (d *Daemon) handleConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	var req ipc.Request
	if err := dec.Decode(&req); err != nil {
		_ = enc.Encode(ipc.Response{OK: false, Error: err.Error()})
		return
	}
	resp := d.dispatch(req)
	_ = enc.Encode(resp)
}

func (d *Daemon) dispatch(req ipc.Request) ipc.Response {
	switch req.Op {
	case ipc.OpPing:
		st := d.State()
		return ipc.Response{OK: true, State: &st}
	case ipc.OpState:
		st := d.State()
		return ipc.Response{OK: true, State: &st}
	case ipc.OpReconnect:
		d.tryConnect()
		st := d.State()
		return ipc.Response{OK: true, State: &st}
	case ipc.OpShutdown:
		go func() {
			time.Sleep(50 * time.Millisecond)
			if d.cancel != nil {
				d.cancel()
			}
		}()
		return ipc.Response{OK: true}
	case ipc.OpSetHost:
		return d.setHost(req.Host)
	case ipc.OpCommand:
		return d.runCommand(req.Cmd)
	default:
		return ipc.Response{OK: false, Error: fmt.Sprintf("unknown op %q", req.Op)}
	}
}

func (d *Daemon) setHost(host int) ipc.Response {
	if err := protocol.ValidateHost(host); err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}
	st := d.State()
	if !st.Connected {
		return ipc.Response{OK: false, Error: "serial not connected — this host is inactive; switch from the active host or use the front-panel button", State: &st}
	}
	resp, err := d.transport().Command(protocol.CmdSetInput(host))
	if err != nil {
		// Expected when switching away: port disappears mid-flight.
		d.setState(func(s *ipc.State) {
			s.Connected = false
			s.IAmActive = host == d.cfg.MyHost
			s.ActiveHost = host
			s.LastRaw = resp
			s.LastError = err.Error()
		})
		st = d.State()
		return ipc.Response{OK: true, Data: resp, State: &st}
	}
	d.setState(func(s *ipc.State) {
		s.ActiveHost = host
		s.IAmActive = host == d.cfg.MyHost
		s.LastRaw = resp
		s.LastError = ""
		if !s.IAmActive {
			s.Connected = false
			_ = d.transport().Close()
		}
	})
	st = d.State()
	return ipc.Response{OK: true, Data: resp, State: &st}
}

func (d *Daemon) runCommand(cmd string) ipc.Response {
	cmd = trimCmd(cmd)
	if cmd == "" {
		return ipc.Response{OK: false, Error: "empty command"}
	}
	st := d.State()
	if !st.Connected {
		return ipc.Response{OK: false, Error: "serial not connected", State: &st}
	}
	var (
		resp string
		err  error
	)
	if cmd == protocol.CmdReset() {
		resp, err = d.transport().CommandWithConfirm(cmd, protocol.CmdResetConfirm())
	} else {
		resp, err = d.transport().Command(cmd)
	}
	if err != nil {
		d.pollOnce()
		st = d.State()
		return ipc.Response{OK: false, Error: err.Error(), Data: resp, State: &st}
	}
	// Refresh after mutating commands.
	lower := cmd
	if len(lower) >= 3 && (hasPrefixFold(lower, "set ") || hasPrefixFold(lower, "get input")) {
		d.refreshFromDevice("command")
	}
	st = d.State()
	return ipc.Response{OK: true, Data: resp, State: &st}
}

func trimCmd(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		a, b := s[i], prefix[i]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}
