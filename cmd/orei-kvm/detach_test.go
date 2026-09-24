package main

import "testing"

func TestShouldDetachTray(t *testing.T) {
	if !shouldDetachTray(false, true) {
		t.Fatal("a terminal launch should background the tray")
	}
	if shouldDetachTray(true, true) {
		t.Fatal("the detached child must stay in the foreground")
	}
	if shouldDetachTray(false, false) {
		t.Fatal("launchd and redirected output must stay in the foreground")
	}
}
