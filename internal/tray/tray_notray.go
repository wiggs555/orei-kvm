//go:build notray

package tray

import "fmt"

// Run is a stub when built with -tags notray (headless CI / servers).
func Run(socketPath string, myHost int) error {
	return fmt.Errorf("system tray support not included in this build (rebuild without -tags notray); socket=%s my_host=%d", socketPath, myHost)
}
