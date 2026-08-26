package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const AppName = "orei-kvm"

// Config holds runtime settings for the CLI and daemon.
type Config struct {
	// Port is the serial device path. Empty means auto-detect.
	Port string `yaml:"port"`
	// Baud is the serial baud rate (default 115200).
	Baud int `yaml:"baud"`
	// MyHost is which KVM host this machine is wired as (1 or 2).
	MyHost int `yaml:"my_host"`
	// PollInterval is how often the daemon checks port presence / host.
	PollInterval time.Duration `yaml:"poll_interval"`
	// SocketPath is the Unix domain socket used for daemon IPC.
	SocketPath string `yaml:"socket"`
	// PortPatterns optional substrings used during auto-detect
	// (matched against port name or description).
	PortPatterns []string `yaml:"port_patterns"`

	// CycleOnActive power-cycles USB device ports when this machine
	// becomes the active host (after a switch). Helps macOS re-enumerate
	// HID devices such as a Magic Trackpad.
	CycleOnActive bool `yaml:"cycle_on_active"`
	// CycleSide is "rx", "tx", or "both" (default rx).
	CycleSide string `yaml:"cycle_side"`
	// CyclePort is the USB device port to cycle; 0 means all ports.
	CyclePort int `yaml:"cycle_port"`
	// CycleDelay is how long ports stay off (default 15s).
	CycleDelay time.Duration `yaml:"cycle_delay"`
	// CycleRestore is the power mode after the off period: on or follow.
	CycleRestore string `yaml:"cycle_restore"`
}

func Default() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Port:         "",
		Baud:         115200,
		MyHost:       1,
		PollInterval: time.Second,
		SocketPath:   filepath.Join(home, ".orei-kvm", "orei-kvm.sock"),
		PortPatterns: []string{
			"usbserial",
			"usbmodem",
			"ttyUSB",
			"ttyACM",
			"cu.usb",
			"cu.SLAB",
			"cu.wchusbserial",
			"FTDI",
			"CP210",
			"CH340",
			"PL2303",
		},
		CycleOnActive: false,
		CycleSide:     "rx",
		CyclePort:     0,
		CycleDelay:    15 * time.Second,
		CycleRestore:  "on",
	}
}

func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", AppName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

func Load() (Config, error) {
	cfg := Default()
	path, err := Path()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	type rawConfig struct {
		Port          string         `yaml:"port"`
		Baud          int            `yaml:"baud"`
		MyHost        int            `yaml:"my_host"`
		PollInterval  time.Duration  `yaml:"poll_interval"`
		SocketPath    string         `yaml:"socket"`
		PortPatterns  []string       `yaml:"port_patterns"`
		CycleOnActive *bool          `yaml:"cycle_on_active"`
		CycleSide     string         `yaml:"cycle_side"`
		CyclePort     *int           `yaml:"cycle_port"`
		CycleDelay    *time.Duration `yaml:"cycle_delay"`
		CycleRestore  string         `yaml:"cycle_restore"`
	}
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if raw.Port != "" {
		cfg.Port = raw.Port
	}
	if raw.Baud != 0 {
		cfg.Baud = raw.Baud
	}
	if raw.MyHost == 1 || raw.MyHost == 2 {
		cfg.MyHost = raw.MyHost
	}
	if raw.PollInterval > 0 {
		cfg.PollInterval = raw.PollInterval
	}
	if raw.SocketPath != "" {
		cfg.SocketPath = expandHome(raw.SocketPath)
	}
	if len(raw.PortPatterns) > 0 {
		cfg.PortPatterns = raw.PortPatterns
	}
	if raw.CycleOnActive != nil {
		cfg.CycleOnActive = *raw.CycleOnActive
	}
	if raw.CycleSide != "" {
		cfg.CycleSide = strings.ToLower(strings.TrimSpace(raw.CycleSide))
	}
	if raw.CyclePort != nil {
		cfg.CyclePort = *raw.CyclePort
	}
	if raw.CycleDelay != nil {
		cfg.CycleDelay = *raw.CycleDelay
	}
	if raw.CycleRestore != "" {
		cfg.CycleRestore = strings.ToLower(strings.TrimSpace(raw.CycleRestore))
	}
	return cfg, nil
}

func (c Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.SocketPath), 0o755); err != nil {
		return err
	}
	type out struct {
		Port          string   `yaml:"port"`
		Baud          int      `yaml:"baud"`
		MyHost        int      `yaml:"my_host"`
		PollInterval  string   `yaml:"poll_interval"`
		SocketPath    string   `yaml:"socket"`
		PortPatterns  []string `yaml:"port_patterns"`
		CycleOnActive bool     `yaml:"cycle_on_active"`
		CycleSide     string   `yaml:"cycle_side"`
		CyclePort     int      `yaml:"cycle_port"`
		CycleDelay    string   `yaml:"cycle_delay"`
		CycleRestore  string   `yaml:"cycle_restore"`
	}
	data, err := yaml.Marshal(out{
		Port:          c.Port,
		Baud:          c.Baud,
		MyHost:        c.MyHost,
		PollInterval:  c.PollInterval.String(),
		SocketPath:    c.SocketPath,
		PortPatterns:  c.PortPatterns,
		CycleOnActive: c.CycleOnActive,
		CycleSide:     c.CycleSide,
		CyclePort:     c.CyclePort,
		CycleDelay:    c.CycleDelay.String(),
		CycleRestore:  c.CycleRestore,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func expandHome(p string) string {
	if len(p) > 0 && p[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

func EnsureRuntimeDir(socketPath string) error {
	return os.MkdirAll(filepath.Dir(socketPath), 0o755)
}

// NormalizeCycleSide returns rx, tx, or both.
func NormalizeCycleSide(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "rx":
		return "rx", nil
	case "tx":
		return "tx", nil
	case "both", "all":
		return "both", nil
	default:
		return "", fmt.Errorf("cycle_side must be rx, tx, or both, got %q", s)
	}
}
