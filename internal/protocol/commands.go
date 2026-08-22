package protocol

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Baud index mapping from the OREI RS-232 command table.
var BaudRates = map[int]int{
	1: 4800,
	2: 9600,
	3: 19200,
	4: 38400,
	5: 57600,
	6: 115200,
}

var baudToIndex = map[int]int{
	4800:   1,
	9600:   2,
	19200:  3,
	38400:  4,
	57600:  5,
	115200: 6,
}

func BaudIndex(rate int) (int, error) {
	if idx, ok := baudToIndex[rate]; ok {
		return idx, nil
	}
	if rate >= 1 && rate <= 6 {
		return rate, nil
	}
	return 0, fmt.Errorf("unsupported baud rate %d (use 4800/9600/19200/38400/57600/115200 or index 1-6)", rate)
}

func BaudRateFromIndex(idx int) (int, error) {
	rate, ok := BaudRates[idx]
	if !ok {
		return 0, fmt.Errorf("baud index must be 1-6, got %d", idx)
	}
	return rate, nil
}

// PowerMode for TX/RX USB device ports.
type PowerMode int

const (
	PowerForceOff PowerMode = 0
	PowerFollow   PowerMode = 1
	PowerForceOn  PowerMode = 2
)

func ParsePowerMode(s string) (PowerMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "0", "off", "force-off", "force_off":
		return PowerForceOff, nil
	case "1", "follow", "follow-input", "follow_input", "host":
		return PowerFollow, nil
	case "2", "on", "force-on", "force_on":
		return PowerForceOn, nil
	default:
		return 0, fmt.Errorf("power mode must be off|follow|on (0|1|2), got %q", s)
	}
}

func (p PowerMode) String() string {
	switch p {
	case PowerForceOff:
		return "force-off"
	case PowerFollow:
		return "follow"
	case PowerForceOn:
		return "force-on"
	default:
		return fmt.Sprintf("unknown(%d)", int(p))
	}
}

func CmdHelp() string    { return "?" }
func CmdHelpAlt() string { return "help" }
func CmdGetFW() string   { return "get fw version" }
func CmdReboot() string  { return "set reboot" }
func CmdReset() string   { return "set reset" }
func CmdResetConfirm() string {
	return "Yes"
}
func CmdGetStatus() string { return "get status" }
func CmdSetKey(on bool) string {
	if on {
		return "set key on"
	}
	return "set key off"
}
func CmdGetKey() string         { return "get key" }
func CmdSetBaud(idx int) string { return fmt.Sprintf("set baud %d", idx) }
func CmdGetBaud() string        { return "get baud" }
func CmdSetInput(host int) string {
	return fmt.Sprintf("set input %d", host)
}
func CmdGetInput() string      { return "get input" }
func CmdGetUSB5V(x int) string { return fmt.Sprintf("get usb5v %d", x) }
func CmdSetAutoswitch(on bool) string {
	if on {
		return "set autoswitch on"
	}
	return "set autoswitch off"
}
func CmdGetAutoswitch() string { return "get autoswitch" }
func CmdSetTXUSBD(port, power int) string {
	return fmt.Sprintf("set tx usbd %d power %d", port, power)
}
func CmdGetTXUSBD(port int) string {
	return fmt.Sprintf("get tx usbd %d power", port)
}
func CmdSetRXUSBD(port, power int) string {
	return fmt.Sprintf("set rx usbd %d power %d", port, power)
}
func CmdGetRXUSBD(port int) string {
	return fmt.Sprintf("get rx usbd %d power", port)
}
func CmdHDBTUpdate() string { return "set hdbt update" }

var (
	reInputHost = regexp.MustCompile(`(?i)(?:input\s+)?usb\s+host\s+(\d)|host[_\s-]*(\d)`)
	reKey       = regexp.MustCompile(`(?i)key\s+(on|off)`)
	reBaud      = regexp.MustCompile(`(?i)baud(?:\s+rate)?\s+(\d+)`)
	reAutosw    = regexp.MustCompile(`(?i)autoswitch\s+(on|off)`)
	reFWLine    = regexp.MustCompile(`(?i)TX\s+FW\s+(\S+)\s+RX\s+FW\s+(\S+)`)
	reStatusRow = regexp.MustCompile(`(?i)\b0?([12])\b\s+(On|Off)\s+(\d+)\s+(On|Off)`)
)

// ParseHost extracts the selected host (1 or 2) from get input / set input feedback.
func ParseHost(response string) (int, error) {
	s := strings.TrimSpace(response)
	if m := reInputHost.FindStringSubmatch(s); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				n, _ := strconv.Atoi(g)
				if n == 1 || n == 2 {
					return n, nil
				}
			}
		}
	}
	if s == "1" || s == "2" {
		n, _ := strconv.Atoi(s)
		return n, nil
	}
	return 0, fmt.Errorf("could not parse host from response: %q", summarize(s))
}

func ParseKeyOn(response string) (bool, error) {
	m := reKey.FindStringSubmatch(response)
	if m == nil {
		return false, fmt.Errorf("could not parse key status from: %q", summarize(response))
	}
	return strings.EqualFold(m[1], "on"), nil
}

func ParseBaud(response string) (int, error) {
	m := reBaud.FindStringSubmatch(response)
	if m == nil {
		return 0, fmt.Errorf("could not parse baud from: %q", summarize(response))
	}
	return strconv.Atoi(m[1])
}

func ParseAutoswitchOn(response string) (bool, error) {
	m := reAutosw.FindStringSubmatch(response)
	if m == nil {
		return false, fmt.Errorf("could not parse autoswitch from: %q", summarize(response))
	}
	return strings.EqualFold(m[1], "on"), nil
}

// Status is a parsed get status response.
type Status struct {
	Raw          string
	TXFW         string
	RXFW         string
	Source       int
	KeyOn        bool
	Baud         int
	AutoswitchOn bool
}

func ParseStatus(response string) Status {
	st := Status{Raw: response}
	if m := reFWLine.FindStringSubmatch(response); m != nil {
		st.TXFW = m[1]
		st.RXFW = m[2]
	}
	if m := reStatusRow.FindStringSubmatch(response); m != nil {
		st.Source, _ = strconv.Atoi(m[1])
		st.KeyOn = strings.EqualFold(m[2], "On")
		st.Baud, _ = strconv.Atoi(m[3])
		st.AutoswitchOn = strings.EqualFold(m[4], "On")
	}
	return st
}

func summarize(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " / ")
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

func ValidateHost(host int) error {
	if host != 1 && host != 2 {
		return fmt.Errorf("host must be 1 or 2, got %d", host)
	}
	return nil
}

func ValidateTXPort(port int) error {
	if port < 0 || port > 2 {
		return fmt.Errorf("TX USB device port must be 0 (all), 1, or 2; got %d", port)
	}
	return nil
}

func ValidateRXPort(port int) error {
	if port < 0 || port > 4 {
		return fmt.Errorf("RX USB device port must be 0 (all) or 1-4; got %d", port)
	}
	return nil
}

func ValidateUSB5V(x int) error {
	if x < 0 || x > 2 {
		return fmt.Errorf("usb5v index must be 0 (all), 1, or 2; got %d", x)
	}
	return nil
}
