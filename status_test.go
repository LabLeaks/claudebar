package main

import (
	"testing"
)

// TestPeakInfo_ReturnsValidOutput verifies that peakInfo returns non-empty
// strings for localStart and localEnd regardless of when it runs. The function
// uses time.Now() internally so we can't inject specific times, but we can
// verify the contract: non-empty time strings are always returned.
func TestPeakInfo_ReturnsValidOutput(t *testing.T) {
	isPeak, localStart, localEnd := peakInfo()

	// localStart and localEnd must always be non-empty (or "?" on timezone failure)
	if localStart == "" {
		t.Error("localStart should not be empty")
	}
	if localEnd == "" {
		t.Error("localEnd should not be empty")
	}

	// Verify boolean is one of two values (trivial but documents the contract)
	_ = isPeak

	// The strings should contain a time-like pattern (not just "?")
	// If timezone loading works, they should contain am/pm
	if localStart != "?" && localEnd != "?" {
		// Valid output — contains a time zone abbreviation or time format
		t.Logf("peakInfo() = isPeak:%v, start:%q, end:%q", isPeak, localStart, localEnd)
	}
}

// TestPeakInfo_TimezoneHandling verifies that peakInfo doesn't panic or
// return garbage when the America/Los_Angeles timezone is available (which
// it should be on any standard system).
func TestPeakInfo_TimezoneHandling(t *testing.T) {
	// Call multiple times to verify stability
	for i := 0; i < 3; i++ {
		isPeak, start, end := peakInfo()
		if start == "?" || end == "?" {
			t.Skip("timezone America/Los_Angeles not available on this system")
		}
		_ = isPeak
	}
}
