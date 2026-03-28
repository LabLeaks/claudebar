package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestStatusLineDataParsing verifies that Claude's statusline JSON is
// correctly parsed into our data structures.
//
// User story: "As a user, I want to see my plan usage percentage in the
// status bar so I know how much capacity I have left before rate limiting."
//
// The statusline JSON is the ONLY source of usage data — if parsing fails,
// the status bar shows "USAGE ..." forever instead of the actual percentage.
func TestStatusLineDataParsing(t *testing.T) {
	input := `{
		"model": {"id": "claude-opus-4-6", "display_name": "Opus 4.6"},
		"rate_limits": {
			"five_hour": {"used_percentage": 23.5, "resets_at": 1738425600},
			"seven_day": {"used_percentage": 41.2, "resets_at": 1738857600}
		},
		"context_window": {"used_percentage": 15}
	}`

	var sl statusLineData
	if err := json.Unmarshal([]byte(input), &sl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if sl.Model.DisplayName != "Opus 4.6" {
		t.Errorf("model = %q, want %q", sl.Model.DisplayName, "Opus 4.6")
	}
	if sl.RateLimits == nil || sl.RateLimits.FiveHour == nil {
		t.Fatal("rate_limits.five_hour should not be nil")
	}
	if sl.RateLimits.FiveHour.UsedPercentage != 23.5 {
		t.Errorf("five_hour pct = %f, want 23.5", sl.RateLimits.FiveHour.UsedPercentage)
	}
	if sl.ContextWindow == nil || sl.ContextWindow.UsedPercentage != 15 {
		t.Error("context_window.used_percentage should be 15")
	}
}

// TestStatusLineDataNullRateLimits verifies graceful handling when
// rate_limits is null or missing from the statusline JSON.
//
// User story: "The status bar should show 'USAGE ...' instead of crashing
// when usage data isn't available yet (before the first API response)."
//
// rate_limits is only populated for Pro/Max subscribers after the first
// API call that returns usage info. All other times it's null.
func TestStatusLineDataNullRateLimits(t *testing.T) {
	input := `{
		"model": {"id": "claude-opus-4-6", "display_name": "Opus 4.6"},
		"context_window": {"used_percentage": 5}
	}`

	var sl statusLineData
	if err := json.Unmarshal([]byte(input), &sl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if sl.RateLimits != nil {
		t.Error("rate_limits should be nil when not present")
	}
	if sl.Model.DisplayName != "Opus 4.6" {
		t.Error("model should still parse when rate_limits is missing")
	}
}

// TestCacheRoundTrip verifies that usage data survives write→read.
//
// User story: "Usage data should persist between status bar refreshes
// (every 5 seconds) without data loss or corruption."
//
// The statusline handler writes the cache and the status bar reads it
// in separate process invocations. If the JSON is malformed or fields
// are lost, the status bar shows wrong data.
func TestCacheRoundTrip(t *testing.T) {
	tmp := t.TempDir()

	original := cachedUsage{
		FiveHourPct:   42.5,
		FiveHourReset: 1738425600,
		SevenDayPct:   15.0,
		ContextPct:    8,
		Model:         "Opus 4.6",
		UpdatedAt:     1700000000,
	}

	path := tmp + "/usage-cache.json"
	data, _ := json.Marshal(original)
	os.WriteFile(path, data, 0644)

	readData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var restored cachedUsage
	if err := json.Unmarshal(readData, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.FiveHourPct != 42.5 {
		t.Errorf("FiveHourPct = %f, want 42.5", restored.FiveHourPct)
	}
	if restored.FiveHourReset != 1738425600 {
		t.Errorf("FiveHourReset = %d, want 1738425600", restored.FiveHourReset)
	}
	if restored.Model != "Opus 4.6" {
		t.Errorf("Model = %q, want %q", restored.Model, "Opus 4.6")
	}
}

func TestUsageCacheFileIncludesSessionName(t *testing.T) {
	path := usageCacheFile("my-project")
	if !strings.HasSuffix(path, "my-project.usage-cache.json") {
		t.Errorf("usageCacheFile(%q) = %q, should end with my-project.usage-cache.json", "my-project", path)
	}
}

func TestUsageCacheFileEmptySessionDefaults(t *testing.T) {
	path := usageCacheFile("")
	if !strings.Contains(path, "default.usage-cache.json") {
		t.Errorf("usageCacheFile(%q) = %q, should contain 'default'", "", path)
	}
}

func TestUsageCacheFileDifferentSessions(t *testing.T) {
	path1 := usageCacheFile("project-a")
	path2 := usageCacheFile("project-b")
	if path1 == path2 {
		t.Errorf("different sessions should produce different paths: %q == %q", path1, path2)
	}
}

func TestLoadCachedUsageWithSessionName(t *testing.T) {
	sessionName := "test-session-load"
	cache := cachedUsage{
		FiveHourPct: 55.5,
		Model:       "Opus 4.6",
		UpdatedAt:   time.Now().Unix(),
	}
	data, _ := json.Marshal(cache)
	path := usageCacheFile(sessionName)
	os.WriteFile(path, data, 0644)
	defer os.Remove(path)

	loaded := loadCachedUsage(sessionName)
	if loaded == nil {
		t.Fatal("loadCachedUsage returned nil")
	}
	if loaded.FiveHourPct != 55.5 {
		t.Errorf("FiveHourPct = %f, want 55.5", loaded.FiveHourPct)
	}

	other := loadCachedUsage("other-session")
	if other != nil {
		t.Error("loadCachedUsage for different session should return nil")
	}
}

func TestWriteStatuslineSettingsIncludesSession(t *testing.T) {
	path := writeStatuslineSettings("/usr/local/bin/claudebar", "my-project")
	if path == "" {
		t.Fatal("writeStatuslineSettings returned empty path")
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading settings: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "_statusline my-project") {
		t.Errorf("settings should pass session name as arg to _statusline, got: %s", content)
	}
}
