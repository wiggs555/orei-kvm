package config

import "testing"

func TestNormalizeCycleSide(t *testing.T) {
	cases := map[string]string{
		"":     "rx",
		"rx":   "rx",
		"TX":   "tx",
		"both": "both",
		"all":  "both",
	}
	for in, want := range cases {
		got, err := NormalizeCycleSide(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
	if _, err := NormalizeCycleSide("hdmi"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDefaultCycleSettings(t *testing.T) {
	cfg := Default()
	if cfg.CycleOnActive {
		t.Fatal("cycle_on_active should default off")
	}
	if cfg.CycleSide != "rx" || cfg.CyclePort != 0 || cfg.CycleRestore != "on" {
		t.Fatalf("defaults: %+v", cfg)
	}
	if cfg.CycleDelay.Seconds() != 15 {
		t.Fatalf("delay %s", cfg.CycleDelay)
	}
}
