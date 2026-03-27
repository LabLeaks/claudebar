package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestTruncate_RuneAware is a regression test for a bug where truncate()
// operated on bytes instead of runes. Multi-byte UTF-8 characters (common
// in task descriptions with non-English text or emoji) would get cut in
// the middle, producing invalid UTF-8 that corrupts terminal output in
// the task viewer pane.
func TestTruncate_RuneAware(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		max      int
		wantLen  int // in runes
		wantLast rune
	}{
		{
			name:    "ASCII within limit",
			input:   "hello",
			max:     10,
			wantLen: 5,
		},
		{
			name:     "ASCII truncated",
			input:    "hello world",
			max:      6,
			wantLast: '…',
		},
		{
			name:     "Japanese truncated safely",
			input:    "こんにちは世界です",
			max:      5,
			wantLast: '…',
		},
		{
			name:    "Empty string",
			input:   "",
			max:     5,
			wantLen: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.max)
			runes := []rune(got)
			if tt.wantLast != 0 {
				if runes[len(runes)-1] != tt.wantLast {
					t.Errorf("last rune = %c, want %c", runes[len(runes)-1], tt.wantLast)
				}
				if len(runes) > tt.max {
					t.Errorf("truncated length %d exceeds max %d", len(runes), tt.max)
				}
			}
			if tt.wantLen > 0 && len(runes) != tt.wantLen {
				t.Errorf("length = %d, want %d", len(runes), tt.wantLen)
			}
		})
	}
}

// TestWrapText verifies word wrapping for task descriptions and agent prompts
// displayed in detail views. Without wrapping, long descriptions overflow the
// pane width and become unreadable.
func TestWrapText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		width int
		want  int // expected number of lines
	}{
		{"short", "hello", 20, 1},
		{"wraps", "the quick brown fox jumps over the lazy dog", 20, 3},
		{"empty", "", 20, 0},
		{"single long word", "superlongwordthatexceedswidth", 10, 1}, // can't break mid-word
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := wrapText(tt.input, tt.width)
			if len(lines) != tt.want {
				t.Errorf("wrapText(%q, %d) = %d lines, want %d", tt.input, tt.width, len(lines), tt.want)
			}
		})
	}
}

// TestStatusIcon verifies the mapping of task status to display icons.
// These icons are the primary visual indicator in the task list — wrong
// mapping means users can't tell what's in progress vs done at a glance.
func TestStatusIcon(t *testing.T) {
	tests := []struct {
		status, want string
	}{
		{"in_progress", "⟳"},
		{"completed", "✓"},
		{"pending", "○"},
		{"unknown", "○"},
	}
	for _, tt := range tests {
		got := statusIcon(tt.status)
		if got != tt.want {
			t.Errorf("statusIcon(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

// TestTaskLoadSave verifies round-trip persistence of task JSON files.
// Claude writes tasks to ~/.claude/tasks/<list-id>/<id>.json and claudebar
// reads/edits them. If load or save breaks, the task viewer shows stale
// data or loses user edits (status changes, deletions).
func TestTaskLoadSave(t *testing.T) {
	tmp := t.TempDir()
	listID := "test-list"
	taskDir := filepath.Join(tmp, ".claude", "tasks", listID)
	os.MkdirAll(taskDir, 0755)

	// Override HOME so taskListDir resolves to our temp dir
	t.Setenv("HOME", tmp)

	// Write a task
	original := task{
		ID:      "1",
		Subject: "Test task",
		Status:  "pending",
	}
	data, _ := json.MarshalIndent(original, "", "  ")
	os.WriteFile(filepath.Join(taskDir, "1.json"), data, 0644)

	// Load and verify
	tasks, err := loadTasks(listID)
	if err != nil {
		t.Fatalf("loadTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(tasks))
	}
	if tasks[0].Subject != "Test task" {
		t.Errorf("subject = %q, want %q", tasks[0].Subject, "Test task")
	}

	// Save modified task
	tasks[0].Status = "completed"
	saveTask(listID, tasks[0])

	// Reload and verify
	tasks2, _ := loadTasks(listID)
	if tasks2[0].Status != "completed" {
		t.Errorf("status after save = %q, want %q", tasks2[0].Status, "completed")
	}
}

// TestTaskLoadSkipsHiddenFiles verifies that loadTasks ignores dotfiles
// like .lock and .highwatermark in the tasks directory. Claude uses these
// for concurrency control in agent teams — including them would show
// garbage entries in the task viewer.
func TestTaskLoadSkipsHiddenFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	listID := "test-list"
	taskDir := filepath.Join(tmp, ".claude", "tasks", listID)
	os.MkdirAll(taskDir, 0755)

	// Create hidden files that Claude uses for locking
	os.WriteFile(filepath.Join(taskDir, ".lock"), []byte(""), 0644)
	os.WriteFile(filepath.Join(taskDir, ".highwatermark"), []byte("3"), 0644)

	// Create a real task
	data, _ := json.MarshalIndent(task{ID: "1", Subject: "Real task", Status: "pending"}, "", "  ")
	os.WriteFile(filepath.Join(taskDir, "1.json"), data, 0644)

	tasks, _ := loadTasks(listID)
	if len(tasks) != 1 {
		t.Errorf("got %d tasks (should skip dotfiles), want 1", len(tasks))
	}
}
