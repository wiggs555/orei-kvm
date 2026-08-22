package protocol

import (
	"testing"
)

func TestParseHost(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"Input USB host 1", 1},
		{"Input USB host 2", 2},
		{"Set input USB host 1", 1},
		{"1", 1},
		{"2", 2},
	}
	for _, tc := range cases {
		got, err := ParseHost(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%q: got %d want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseKeyBaudAutoswitch(t *testing.T) {
	on, err := ParseKeyOn("Key on")
	if err != nil || !on {
		t.Fatalf("key on: %v %v", on, err)
	}
	off, err := ParseKeyOn("Key off")
	if err != nil || off {
		t.Fatalf("key off: %v %v", off, err)
	}
	baud, err := ParseBaud("Baud rate 115200")
	if err != nil || baud != 115200 {
		t.Fatalf("baud: %v %v", baud, err)
	}
	as, err := ParseAutoswitchOn("Autoswitch on")
	if err != nil || !as {
		t.Fatalf("autoswitch: %v %v", as, err)
	}
}

func TestParseStatus(t *testing.T) {
	raw := `Status Info 2-Port USB 3.2 Gen 1 Extender
TX FW 1.0.0 RX FW 1.0.0
Source    Key     Baud      AutoSwitch
01           On      115200    On
In/Out     Host_1               Host_2           HDMI_Out1      HDMI_Out2
Cable      Connected    Connected     Connected       Connected`
	st := ParseStatus(raw)
	if st.Source != 1 {
		t.Fatalf("source=%d", st.Source)
	}
	if !st.KeyOn || !st.AutoswitchOn || st.Baud != 115200 {
		t.Fatalf("status fields: %+v", st)
	}
	if st.TXFW != "1.0.0" || st.RXFW != "1.0.0" {
		t.Fatalf("fw tx=%q rx=%q", st.TXFW, st.RXFW)
	}
}

func TestBaudIndex(t *testing.T) {
	idx, err := BaudIndex(115200)
	if err != nil || idx != 6 {
		t.Fatalf("got %d %v", idx, err)
	}
	idx, err = BaudIndex(3)
	if err != nil || idx != 3 {
		t.Fatalf("index passthrough: %d %v", idx, err)
	}
}

func TestPowerMode(t *testing.T) {
	m, err := ParsePowerMode("follow")
	if err != nil || m != PowerFollow {
		t.Fatalf("%v %v", m, err)
	}
}
