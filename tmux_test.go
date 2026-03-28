package main

import (
	"strings"
	"testing"
)

func TestMenuCmdContainsAllLabels(t *testing.T) {
	cmd := menuCmd("/usr/local/bin/claudebar")
	for _, m := range menuItems {
		if m.Label == "" {
			continue
		}
		if !strings.Contains(cmd, m.Label) {
			t.Errorf("menuCmd missing label %q", m.Label)
		}
	}
}

func TestMenuCmdContainsAllActions(t *testing.T) {
	cmd := menuCmd("/usr/local/bin/claudebar")
	for _, m := range menuItems {
		if m.Action == "" {
			continue
		}
		if !strings.Contains(cmd, m.Action) {
			t.Errorf("menuCmd missing action %q", m.Action)
		}
	}
}

func TestMenuCmdKillHasConfirmBefore(t *testing.T) {
	cmd := menuCmd("/usr/local/bin/claudebar")
	var killItem menuItem
	for _, m := range menuItems {
		if m.Action == "_kill" {
			killItem = m
			break
		}
	}
	if killItem.Action == "" {
		t.Fatal("no menu item with action _kill found")
	}
	if killItem.Confirm == "" {
		t.Fatal("_kill menu item should have a Confirm prompt")
	}
	killIdx := strings.Index(cmd, "_kill")
	if killIdx < 0 {
		t.Fatal("_kill not found in menuCmd output")
	}
	confirmIdx := strings.LastIndex(cmd[:killIdx], "confirm-before")
	if confirmIdx < 0 {
		t.Error("confirm-before not found before _kill in menuCmd output")
	}
}

func TestMenuCmdHasSeparators(t *testing.T) {
	cmd := menuCmd("/usr/local/bin/claudebar")
	// Separators in tmux display-menu are standalone "" tokens.
	// Verify the output contains at least one separator.
	if !strings.Contains(cmd, ` "" `) {
		t.Error("menuCmd should contain separator entries")
	}
}
