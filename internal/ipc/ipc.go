package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// Request is sent from CLI/tray clients to the daemon.
type Request struct {
	Op   string          `json:"op"`
	Cmd  string          `json:"cmd,omitempty"`
	Host int             `json:"host,omitempty"`
	Args json.RawMessage `json:"args,omitempty"`
}

// State is the polled daemon view of the switch.
type State struct {
	Connected   bool      `json:"connected"`
	PortPath    string    `json:"port_path,omitempty"`
	ActiveHost  int       `json:"active_host,omitempty"` // 1 or 2 when known
	MyHost      int       `json:"my_host"`
	IAmActive   bool      `json:"i_am_active"`
	LastError   string    `json:"last_error,omitempty"`
	LastRaw     string    `json:"last_raw,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
	PollEveryMS int       `json:"poll_every_ms"`
}

// Response is returned by the daemon.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	State *State `json:"state,omitempty"`
	Data  string `json:"data,omitempty"`
}

const (
	OpPing      = "ping"
	OpState     = "state"
	OpCommand   = "command"
	OpSetHost   = "set_host"
	OpShutdown  = "shutdown"
	OpReconnect = "reconnect"
)

func Dial(socketPath string, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	return d.Dial("unix", socketPath)
}

func Call(socketPath string, req Request) (*Response, error) {
	conn, err := Dial(socketPath, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	if err := enc.Encode(req); err != nil {
		return nil, err
	}
	var resp Response
	if err := dec.Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func IsDaemonUp(socketPath string) bool {
	resp, err := Call(socketPath, Request{Op: OpPing})
	return err == nil && resp != nil && resp.OK
}

func RemoveStaleSocket(socketPath string) error {
	if _, err := os.Stat(socketPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if IsDaemonUp(socketPath) {
		return fmt.Errorf("daemon already running at %s", socketPath)
	}
	return os.Remove(socketPath)
}
