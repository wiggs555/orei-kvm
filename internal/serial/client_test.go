package serial

import (
	"testing"
	"time"

	goserial "go.bug.st/serial"
)

// floodPort returns full buffers instantly until the caller stops reading.
// Models a stuck USB-serial adapter that never goes idle.
type floodPort struct{}

func (floodPort) SetMode(*goserial.Mode) error                  { return nil }
func (floodPort) Read(p []byte) (int, error)                    { return len(p), nil }
func (floodPort) Write(p []byte) (int, error)                   { return len(p), nil }
func (floodPort) Drain() error                                  { return nil }
func (floodPort) ResetInputBuffer() error                       { return nil }
func (floodPort) ResetOutputBuffer() error                      { return nil }
func (floodPort) SetDTR(bool) error                             { return nil }
func (floodPort) SetRTS(bool) error                             { return nil }
func (floodPort) GetModemStatusBits() (*goserial.ModemStatusBits, error) {
	return &goserial.ModemStatusBits{}, nil
}
func (floodPort) SetReadTimeout(time.Duration) error { return nil }
func (floodPort) Close() error                       { return nil }
func (floodPort) Break(time.Duration) error          { return nil }

func TestReadResponseCapsFlood(t *testing.T) {
	resp, err := readResponse(floodPort{}, time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(resp) > maxResponseBytes {
		t.Fatalf("response grew past cap: %d > %d", len(resp), maxResponseBytes)
	}
	if len(resp) < maxResponseBytes/2 {
		t.Fatalf("expected a large capped response, got %d bytes", len(resp))
	}
}
