package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const usageCacheMaxAge = 10 * time.Minute

// statusLineData is the JSON Claude Code sends to the statusline command via stdin.
// See sprint-005 CC source analysis for full schema.
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
		TotalInputTokens  int `json:"total_input_tokens"`
		TotalOutputTokens int `json:"total_output_tokens"`
		ContextWindowSize int `json:"context_window_size"`
		CurrentUsage      *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"current_usage"`
		UsedPercentage      *int `json:"used_percentage"`
		RemainingPercentage *int `json:"remaining_percentage"`
	} `json:"context_window"`
	Workspace *struct {
		CurrentDir string   `json:"current_dir"`
		ProjectDir string   `json:"project_dir"`
		AddedDirs  []string `json:"added_dirs"`
	} `json:"workspace"`
	Version string `json:"version"`
}

// cachedUsage is what we write to the temp file for the status bar to read
type cachedUsage struct {
	FiveHourPct  float64 `json:"five_hour_pct"`
	FiveHourReset int64  `json:"five_hour_reset"`
	SevenDayPct  float64 `json:"seven_day_pct"`
	ContextPct   int     `json:"context_pct"`
	Model        string  `json:"model"`
	UpdatedAt    int64   `json:"updated_at"`

	// OpenRouter-specific usage (set when router uses OpenRouter proxy)
	TotalTokens  int     `json:"total_tokens,omitempty"`
	CachedTokens int     `json:"cached_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	RouterActive bool    `json:"router_active,omitempty"`
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
		if sl.ContextWindow.UsedPercentage != nil {
			cache.ContextPct = *sl.ContextWindow.UsedPercentage
		}
		// For router sessions, use cumulative token counts from CC's own tracking
		cache.TotalTokens = sl.ContextWindow.TotalInputTokens + sl.ContextWindow.TotalOutputTokens
		if sl.ContextWindow.CurrentUsage != nil {
			cache.CachedTokens = sl.ContextWindow.CurrentUsage.CacheReadInputTokens
		}
	}

	// Check if router is active — CC's statusline gives us tokens directly
	sess := currentSession()
	state := loadState(sess)
	if state.Router != "" {
		cache.RouterActive = true
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
	if time.Since(time.Unix(cache.UpdatedAt, 0)) > usageCacheMaxAge {
		return nil
	}
	return &cache
}

