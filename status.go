package main

import (
	"fmt"
	"time"
)

func runStatus() {
	peak, localStart, localEnd := peakInfo()

	if peak {
		fmt.Printf("#[fg=#ff6b6b,bold]⚡PEAK#[fg=#888888] (til %s)", localEnd)
	} else {
		fmt.Printf("#[fg=#00ff88]🌙 OFF-PEAK#[fg=#888888] (til %s)", localStart)
	}

	// Show usage from cached statusline data
	sess, _ := tmuxOutput("display-message", "-p", "#{session_name}")
	usage := loadCachedUsage(sess)
	if usage == nil || (usage.FiveHourPct == 0 && usage.FiveHourReset == 0) {
		// No rate limit data yet (arrives after first API response with usage info)
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

		// Show reset time like peak does
		if usage.FiveHourReset > 0 {
			resetTime := time.Unix(usage.FiveHourReset, 0)
			local := resetTime.In(time.Now().Location())
			fmt.Printf("#[fg=#888888] (resets %s)", local.Format("3:04pm"))
		}
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
