package serial

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

var (
	ErrNotConnected = errors.New("serial port not connected")
	ErrPortMissing  = errors.New("serial port not available")
)

// PortInfo describes a discovered serial device.
type PortInfo struct {
	Name        string
	Description string
	VID         string
	PID         string
}

// ListPorts returns available serial ports.
func ListPorts() ([]PortInfo, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		// Fall back to bare names
		names, err2 := serial.GetPortsList()
		if err2 != nil {
			return nil, err
		}
		out := make([]PortInfo, 0, len(names))
		for _, n := range names {
			out = append(out, PortInfo{Name: n})
		}
		return out, nil
	}
	out := make([]PortInfo, 0, len(ports))
	for _, p := range ports {
		info := PortInfo{
			Name:        p.Name,
			Description: p.Product,
		}
		if p.IsUSB {
			info.VID = p.VID
			info.PID = p.PID
			if info.Description == "" {
				info.Description = p.SerialNumber
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// FindPort picks a configured path or the best matching pattern.
// On macOS both /dev/tty.* and /dev/cu.* exist for one adapter. cu.* (callout)
// is the one that can be opened while the daemon is running; tty.* often
// fails or blocks, which the tray then reports as inactive.
func FindPort(preferred string, patterns []string) (string, error) {
	ports, err := ListPorts()
	if err != nil {
		if preferred != "" {
			return preferred, nil
		}
		return "", err
	}
	return pickPort(ports, preferred, patterns)
}

// pickPort chooses preferred, or the best pattern match. Callout devices
// (cu.*) outrank dial-in devices (tty.*).
func pickPort(ports []PortInfo, preferred string, patterns []string) (string, error) {
	if preferred != "" {
		for _, p := range ports {
			if samePort(p.Name, preferred) {
				return p.Name, nil
			}
		}
		// Still try opening preferred even if not enumerated.
		return preferred, nil
	}
	best := ""
	bestRank := int(^uint(0) >> 1)
	for _, p := range ports {
		hay := strings.ToLower(p.Name + " " + p.Description)
		matched := false
		for _, pat := range patterns {
			if pat == "" {
				continue
			}
			if strings.Contains(hay, strings.ToLower(pat)) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if r := portRank(p.Name); r < bestRank {
			best = p.Name
			bestRank = r
		}
	}
	if best != "" {
		return best, nil
	}
	if len(ports) == 1 {
		return ports[0].Name, nil
	}
	return "", fmt.Errorf("%w: no matching serial port (set port in config or pass --port)", ErrPortMissing)
}

func samePort(a, b string) bool {
	if a == b {
		return true
	}
	return filepath.Base(a) == filepath.Base(b) && filepath.Base(a) != "" && filepath.Base(a) != "."
}

// portRank prefers macOS callout devices over dial-in devices.
func portRank(name string) int {
	base := strings.ToLower(filepath.Base(name))
	switch {
	case strings.HasPrefix(base, "cu.") || strings.Contains(base, "callout"):
		return 0
	case strings.HasPrefix(base, "tty.") || strings.HasPrefix(base, "tty"):
		return 2
	default:
		return 1
	}
}

// PortExists reports whether a named port is currently enumerated.
// A listing failure returns true: the caller should keep a live port open
// and learn about a real disconnect from the next read, instead of treating
// an enumeration glitch as "serial not present".
func PortExists(name string) bool {
	if name == "" {
		return false
	}
	ports, err := ListPorts()
	if err != nil {
		return true
	}
	for _, p := range ports {
		if samePort(p.Name, name) {
			return true
		}
	}
	return false
}

// Client talks to the OREI RS-232 ASCII interface.
type Client struct {
	mu       sync.Mutex
	port     serial.Port
	path     string
	baud     int
	readWait time.Duration
}

func NewClient(baud int) *Client {
	if baud == 0 {
		baud = 115200
	}
	return &Client{
		baud:     baud,
		readWait: 400 * time.Millisecond,
	}
}

func (c *Client) Path() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.path
}

func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.port != nil
}

func (c *Client) Open(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port != nil {
		_ = c.port.Close()
		c.port = nil
	}
	mode := &serial.Mode{
		BaudRate: c.baud,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	p, err := serial.Open(path, mode)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	_ = p.SetReadTimeout(c.readWait)
	c.port = p
	c.path = path
	// Drain any boot banner
	_, _ = readAvailable(p, 100*time.Millisecond)
	return nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port == nil {
		return nil
	}
	err := c.port.Close()
	c.port = nil
	return err
}

func (c *Client) SetBaud(baud int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baud = baud
}

// Command sends an ASCII command (without CRLF) and returns the response text.
func (c *Client) Command(cmd string) (string, error) {
	return c.CommandWithConfirm(cmd, "")
}

// CommandWithConfirm sends cmd, then optionally a follow-up confirmation line
// (used by set reset → Yes).
func (c *Client) CommandWithConfirm(cmd, confirm string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port == nil {
		return "", ErrNotConnected
	}
	if err := drain(c.port); err != nil {
		return "", err
	}
	payload := strings.TrimRight(cmd, "\r\n") + "\r\n"
	if _, err := c.port.Write([]byte(payload)); err != nil {
		_ = c.port.Close()
		c.port = nil
		return "", fmt.Errorf("write: %w", err)
	}
	resp, err := readResponse(c.port, c.readWait)
	if err != nil {
		return resp, err
	}
	if confirm != "" {
		time.Sleep(50 * time.Millisecond)
		if _, err := c.port.Write([]byte(strings.TrimRight(confirm, "\r\n") + "\r\n")); err != nil {
			_ = c.port.Close()
			c.port = nil
			return resp, fmt.Errorf("write confirm: %w", err)
		}
		more, err := readResponse(c.port, c.readWait*2)
		if more != "" {
			if resp != "" {
				resp += "\n" + more
			} else {
				resp = more
			}
		}
		if err != nil {
			return resp, err
		}
	}
	return strings.TrimSpace(resp), nil
}

func drain(p serial.Port) error {
	_ = p.ResetInputBuffer()
	_ = p.ResetOutputBuffer()
	return nil
}

func readAvailable(p serial.Port, wait time.Duration) ([]byte, error) {
	_ = p.SetReadTimeout(wait)
	buf := make([]byte, 4096)
	n, err := p.Read(buf)
	if n > 0 {
		return buf[:n], nil
	}
	if err != nil && !errors.Is(err, io.EOF) {
		// timeout is normal
		return nil, nil
	}
	return nil, nil
}

func readResponse(p serial.Port, idle time.Duration) (string, error) {
	var out bytes.Buffer
	buf := make([]byte, 1024)
	deadline := time.Now().Add(2 * time.Second)
	_ = p.SetReadTimeout(idle)
	idleSinceData := false
	for time.Now().Before(deadline) {
		n, err := p.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			idleSinceData = true
			// Keep reading while data is flowing; after a quiet idle window, stop.
			_ = p.SetReadTimeout(idle)
			continue
		}
		if idleSinceData {
			break
		}
		if err != nil && !errors.Is(err, io.EOF) {
			// On many platforms timeout returns an error with 0 bytes.
			if out.Len() > 0 {
				break
			}
			// No data yet — wait a bit more unless hard error.
			if isLikelyDisconnect(err) {
				return clean(out.String()), err
			}
		}
		// First read timed out with no data — still return empty (some cmds echo little).
		if !idleSinceData {
			break
		}
	}
	return clean(out.String()), nil
}

func clean(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(s)
}

func isLikelyDisconnect(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "device not configured") ||
		strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "input/output error") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "access denied")
}
