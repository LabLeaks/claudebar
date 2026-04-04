package main

import (
	"strings"
	"testing"
	"time"
)

// TestCycleFeature verifies the three-state cycle: OFF → ON → ALWAYS → OFF.
// This powers the Features menu (⌥M → ⚙) where each click advances the state.
// A bug here means features get stuck or skip states.
func TestCycleFeature(t *testing.T) {
	tests := []struct {
		name                   string
		sessionOn, configOn    bool
		wantSession, wantConfig bool
	}{
		{
			name:       "OFF → ON",
			sessionOn:  false,
			configOn:   false,
			wantSession: true,
			wantConfig:  false,
		},
		{
			name:       "ON → ALWAYS",
			sessionOn:  true,
			configOn:   false,
			wantSession: true,
			wantConfig:  true,
		},
		{
			name:       "ALWAYS → OFF",
			sessionOn:  true,
			configOn:   true,
			wantSession: false,
			wantConfig:  false,
		},
		{
			name:       "config on but session off — edge case → OFF",
			sessionOn:  false,
			configOn:   true,
			wantSession: false,
			wantConfig:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSession, gotConfig := cycleFeature(tt.sessionOn, tt.configOn)
			if gotSession != tt.wantSession {
				t.Errorf("session: got %v, want %v", gotSession, tt.wantSession)
			}
			if gotConfig != tt.wantConfig {
				t.Errorf("config: got %v, want %v", gotConfig, tt.wantConfig)
			}
		})
	}
}

// TestFeatureState verifies the display string for each feature state.
// These strings appear in the Features menu — wrong labels confuse users
// about whether a feature is active.
func TestFeatureState(t *testing.T) {
	tests := []struct {
		name               string
		sessionOn, configOn bool
		wantContains       string
	}{
		{"both off → OFF", false, false, "OFF"},
		{"session on only → ON", true, false, "ON"},
		{"config on, session off → ALWAYS", false, true, "ALWAYS"},
		{"both on → ALWAYS", true, true, "ALWAYS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := featureState(tt.sessionOn, tt.configOn)
			if !strings.Contains(got, tt.wantContains) {
				t.Errorf("featureState(%v, %v) = %q, want it to contain %q",
					tt.sessionOn, tt.configOn, got, tt.wantContains)
			}
		})
	}
}

// TestTimeAgo verifies human-readable relative time formatting. This appears
// in the session picker (claudebar sessions) showing when each session was
// last active. Wrong output makes it hard to identify the right session.
func TestTimeAgo(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"0 seconds ago", now, "just now"},
		{"30 seconds ago", now.Add(-30 * time.Second), "just now"},
		{"5 minutes ago", now.Add(-5 * time.Minute), "5m ago"},
		{"59 minutes ago", now.Add(-59 * time.Minute), "59m ago"},
		{"2 hours ago", now.Add(-2 * time.Hour), "2h ago"},
		{"23 hours ago", now.Add(-23 * time.Hour), "23h ago"},
		{"48 hours ago", now.Add(-48 * time.Hour), "2d ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := timeAgo(tt.t, false)
			if got != tt.want {
				t.Errorf("timeAgo(%v, false) = %q, want %q", now.Sub(tt.t), got, tt.want)
			}
		})
	}
}
