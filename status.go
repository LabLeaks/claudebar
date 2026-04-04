package main

import (
	"fmt"
	"os"
	"time"
)

func runStatus() {
	// Session name passed as argument from tmux status-format #{session_name}
	sess := ""
	if len(os.Args) > 2 {
		sess = os.Args[2]
	}
	if sess == "" {
		sess = currentSession()
	}
	state := loadState(sess)
	usage := loadCachedUsage(sess)
	routerActive := state.Router != ""

	// Peak/offpeak only relevant for direct Anthropic sessions
	if !routerActive {
		peak, localStart, localEnd := peakInfo()
		if peak {
			fmt.Printf("#[fg=#ff6b6b,bold]⚡PEAK#[fg=#888888] (til %s)", localEnd)
		} else {
			fmt.Printf("#[fg=#00ff88]🌙 OFF-PEAK#[fg=#888888] (til %s)", localStart)
		}
	}

	// Show active router name
	if routerActive {
		fmt.Printf("#[fg=#b388ff,bold]⚡ %s", state.Router)
	}

	// Usage display: token/cost for router, % for Anthropic
	if routerActive {
		if usage != nil && usage.RouterActive && (usage.TotalTokens > 0 || usage.CostUSD > 0) {
			fmt.Printf("#[fg=#1a1a2e]  #[fg=#00ff88]⏱ %s | $%.2f", formatTokens(usage.TotalTokens), usage.CostUSD)
		} else {
			fmt.Printf("#[fg=#1a1a2e]  #[fg=#555555]⏱ TOKENS ... | $...")
		}
	} else if usage == nil || (usage.FiveHourPct == 0 && usage.FiveHourReset == 0) {
		fmt.Printf("#[fg=#1a1a2e]  #[fg=#555555]⏱ USAGE ...")
	} else {
		pct := usage.FiveHourPct
		color := "#00ff88" // green
		if pct > 80 {
			color = "#ff6b6b" // red
		} else if pct > 50 {
			color = "#ffd700" // yellow
		}
		fmt.Printf("#[fg=#1a1a2e]  #[fg=%s]⏱ USAGE %d%%", color, int(pct))

		if usage.FiveHourReset > 0 {
			resetTime := time.Unix(usage.FiveHourReset, 0)
			local := resetTime.In(time.Now().Location())
			fmt.Printf("#[fg=#888888] (resets %s)", local.Format("3:04pm"))
		}
	}
}

// formatTokens formats token counts for display (e.g., 282000 → "282K", 1500000 → "1.5M")
func formatTokens(tokens int) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%dK", tokens/1_000)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

// peakInfo checks if we're in Anthropic peak hours (weekdays 5am-11am PT)
// and returns the peak window in the user's local timezone
func peakInfo() (isPeak bool, localStart, localEnd string) {
	pt, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return false, "?", "?"
	}

	now := time.Now()
	nowPT := now.In(pt)

	weekday := nowPT.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		// Show next Monday's peak window
		daysUntilMonday := (8 - int(weekday)) % 7
		if daysUntilMonday == 0 {
			daysUntilMonday = 1
		}
		monday := nowPT.AddDate(0, 0, daysUntilMonday)
		startPT := time.Date(monday.Year(), monday.Month(), monday.Day(), 5, 0, 0, 0, pt)
		endPT := time.Date(monday.Year(), monday.Month(), monday.Day(), 11, 0, 0, 0, pt)
		return false, startPT.In(now.Location()).Format("Mon 3:04pm MST"), endPT.In(now.Location()).Format("3:04pm MST")
	}

	hour := nowPT.Hour()
	isPeak = hour >= 5 && hour < 11

	startPT := time.Date(nowPT.Year(), nowPT.Month(), nowPT.Day(), 5, 0, 0, 0, pt)
	endPT := time.Date(nowPT.Year(), nowPT.Month(), nowPT.Day(), 11, 0, 0, 0, pt)
	localStart = startPT.In(now.Location()).Format("3:04pm MST")
	localEnd = endPT.In(now.Location()).Format("3:04pm MST")

	return isPeak, localStart, localEnd
}
