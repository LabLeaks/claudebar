package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// statusLineData is the JSON Claude Code sends to the statusline command via stdin
type statusLineData struct {
	Model struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	RateLimits *struct {
		FiveHour *struct {
			UsedPercentage float64 `json:"used_percentage"`
			ResetsAt       int64   `json:"resets_at"`
		} `json:"five_hour"`
		SevenDay *struct {
			UsedPercentage float64 `json:"used_percentage"`
			ResetsAt       int64   `json:"resets_at"`
		} `json:"seven_day"`
	} `json:"rate_limits"`
	ContextWindow *struct {
		UsedPercentage int `json:"used_percentage"`
	} `json:"context_window"`
}

// cachedUsage is what we write to the temp file for the status bar to read
type cachedUsage struct {
	FiveHourPct  float64 `json:"five_hour_pct"`
	FiveHourReset int64  `json:"five_hour_reset"`
	SevenDayPct  float64 `json:"seven_day_pct"`
	ContextPct   int     `json:"context_pct"`
	Model        string  `json:"model"`
	UpdatedAt    int64   `json:"updated_at"`
}

func usageCacheFile(sessionName string) string {
	if sessionName == "" {
		sessionName = "default"
	}
	return filepath.Join(stateDir(), sessionName+".usage-cache.json")
}

// runStatusLine reads Claude's statusline JSON from stdin, caches usage data,
// and outputs the existing statusline format (so we don't break the user's display)
func runStatusLine() {
	// Session name is passed as an argument by writeStatuslineSettings
	sessionName := "default"
	if len(os.Args) > 2 {
		sessionName = os.Args[2]
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return
	}

	var sl statusLineData
	if err := json.Unmarshal(data, &sl); err != nil {
		return
	}

	// Cache usage data for the status bar to read
	cache := cachedUsage{
		Model:     sl.Model.DisplayName,
		UpdatedAt: time.Now().Unix(),
	}
	if sl.RateLimits != nil {
		if sl.RateLimits.FiveHour != nil {
			cache.FiveHourPct = sl.RateLimits.FiveHour.UsedPercentage
			cache.FiveHourReset = sl.RateLimits.FiveHour.ResetsAt
		}
		if sl.RateLimits.SevenDay != nil {
			cache.SevenDayPct = sl.RateLimits.SevenDay.UsedPercentage
		}
	}
	if sl.ContextWindow != nil {
		cache.ContextPct = sl.ContextWindow.UsedPercentage
	}

	cacheData, _ := json.Marshal(cache)
	os.WriteFile(usageCacheFile(sessionName), cacheData, 0644)

	// Output statusline for Claude's built-in display (preserve existing behavior)
	user := os.Getenv("USER")
	host, _ := os.Hostname()
	var input map[string]interface{}
	json.Unmarshal(data, &input)
	dir := ""
	if ws, ok := input["workspace"].(map[string]interface{}); ok {
		if d, ok := ws["current_dir"].(string); ok {
			dir = filepath.Base(d)
		}
	}
	fmt.Printf("%s@%s %s [%s]", user, host, dir, sl.Model.DisplayName)
}

// writeStatuslineSettings writes a settings file that configures claudebar as the statusline handler
func writeStatuslineSettings(selfPath, tmuxSession string) string {
	dir := stateDir()
	path := filepath.Join(dir, tmuxSession+".statusline-settings.json")
	settings := struct {
		StatusLine struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"statusLine"`
	}{}
	settings.StatusLine.Type = "command"
	settings.StatusLine.Command = selfPath + " _statusline " + tmuxSession
	content, err := json.Marshal(settings)
	if err != nil {
		return ""
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		return ""
	}
	return path
}

// loadCachedUsage reads the cached usage data from the temp file
func loadCachedUsage(sessionName string) *cachedUsage {
	data, err := os.ReadFile(usageCacheFile(sessionName))
	if err != nil {
		return nil
	}
	var cache cachedUsage
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}
	// Stale after 10 minutes
	if time.Since(time.Unix(cache.UpdatedAt, 0)) > 10*time.Minute {
		return nil
	}
	return &cache
}
