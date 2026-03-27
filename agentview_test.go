package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInboxMessageDisplayText verifies that inbox messages are rendered
// readably in the agent detail view.
//
// User story: "As a user viewing team member details, I want to see
// human-readable message summaries, not raw JSON task assignments."
//
// Claude's inter-agent messages use JSON-encoded task_assignment payloads
// in the text field. Without parsing, users see raw JSON which is useless
// for understanding what agents are doing.
func TestInboxMessageDisplayText(t *testing.T) {
	tests := []struct {
		name string
		msg  inboxMessage
		want string
	}{
		{
			name: "JSON task_assignment shows subject",
			msg:  inboxMessage{Text: `{"type":"task_assignment","subject":"Initialize shadcn/ui","taskId":"2"}`},
			want: "[task] Initialize shadcn/ui",
		},
		{
			name: "plain text passes through",
			msg:  inboxMessage{Text: "Please run the tests before proceeding"},
			want: "Please run the tests before proceeding",
		},
		{
			name: "summary field preferred over long text",
			msg: inboxMessage{
				Text:    "Very long message that goes on and on about implementation details...",
				Summary: "Run tests first",
			},
			want: "Run tests first",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.msg.displayText()
			if got != tt.want {
				t.Errorf("displayText() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestInboxMessageTime verifies timestamp parsing from both formats
// Claude uses in inbox messages.
//
// User story: "As a user viewing an agent's inbox, I want to see when
// messages were sent so I can understand the conversation timeline."
//
// Claude uses ISO 8601 strings in some messages and Unix millisecond
// timestamps in others. Both must parse correctly or the "5m ago" / "2h ago"
// labels will show "?" or wrong times.
func TestInboxMessageTime(t *testing.T) {
	// String timestamp (ISO 8601)
	msg1 := inboxMessage{Timestamp: "2026-03-25T22:12:04.089Z"}
	t1 := msg1.time()
	if t1.IsZero() {
		t.Error("string timestamp should parse, got zero time")
	}
	if t1.Year() != 2026 {
		t.Errorf("year = %d, want 2026", t1.Year())
	}

	// Float64 timestamp (Unix millis, from JSON number)
	msg2 := inboxMessage{Timestamp: float64(1774623817220)}
	t2 := msg2.time()
	if t2.IsZero() {
		t.Error("float64 timestamp should parse, got zero time")
	}
}

// TestListTeamsForProject verifies that the agent viewer only shows teams
// relevant to the current project directory.
//
// User story: "As a user working on project A, I should only see teams
// created for project A, not teams from project B or global teams."
//
// This was a real issue — the viewer showed all teams globally, including
// stale teams from other projects, confusing users about which agents
// were active in their current context.
func TestListTeamsForProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	teamsDir := filepath.Join(tmp, ".claude", "teams")

	// Create a team that matches our project
	matchingTeam := filepath.Join(teamsDir, "my-feature")
	os.MkdirAll(matchingTeam, 0755)
	os.WriteFile(filepath.Join(matchingTeam, "config.json"), []byte(`{
		"members": [{"name": "lead", "agentType": "lead", "cwd": "/Users/gk/work/myproject"}]
	}`), 0644)

	// Create a team from a different project
	otherTeam := filepath.Join(teamsDir, "other-work")
	os.MkdirAll(otherTeam, 0755)
	os.WriteFile(filepath.Join(otherTeam, "config.json"), []byte(`{
		"members": [{"name": "lead", "agentType": "lead", "cwd": "/Users/gk/work/different-project"}]
	}`), 0644)

	// Create a stale team with no config (just inboxes)
	staleTeam := filepath.Join(teamsDir, "stale")
	os.MkdirAll(filepath.Join(staleTeam, "inboxes"), 0755)

	teams := listTeamsForProject("/Users/gk/work/myproject")

	if len(teams) != 1 {
		t.Fatalf("got %d teams, want 1 (only the matching project)", len(teams))
	}
	if teams[0] != "my-feature" {
		t.Errorf("team = %q, want %q", teams[0], "my-feature")
	}
}
