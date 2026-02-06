package sync

import (
	"testing"
	"time"
)

func TestCompareManifests_FilesToSync(t *testing.T) {
	sender := NewManifest("/sender")
	receiver := NewManifest("/receiver")

	now := time.Now()

	// Add files to sender
	sender.Add(&FileInfo{Path: "new_file.txt", Size: 100, ModTime: now})
	sender.Add(&FileInfo{Path: "modified_file.txt", Size: 200, ModTime: now})
	sender.Add(&FileInfo{Path: "unchanged_file.txt", Size: 50, ModTime: now})

	// Add files to receiver
	receiver.Add(&FileInfo{Path: "modified_file.txt", Size: 100, ModTime: now.Add(-1 * time.Hour)})
	receiver.Add(&FileInfo{Path: "unchanged_file.txt", Size: 50, ModTime: now})

	plan := CompareManifests(sender, receiver)

	// Should sync new_file.txt and modified_file.txt
	if len(plan.FilesToSync) != 2 {
		t.Errorf("Expected 2 files to sync, got %d", len(plan.FilesToSync))
	}
}

func TestCompareManifests_SmartDeletion(t *testing.T) {
	sender := NewManifest("/sender")
	receiver := NewManifest("/receiver")

	now := time.Now()

	// Sender has Series1/Season1/
	sender.Add(&FileInfo{Path: "Series1", IsDir: true})
	sender.Add(&FileInfo{Path: "Series1/Season1", IsDir: true})
	sender.Add(&FileInfo{Path: "Series1/Season1/file.mkv", Size: 100, ModTime: now})

	// Receiver has Series1/Season1/ AND receiver-only test12/
	receiver.Add(&FileInfo{Path: "Series1", IsDir: true})
	receiver.Add(&FileInfo{Path: "Series1/Season1", IsDir: true})
	receiver.Add(&FileInfo{Path: "Series1/Season1/file.mkv", Size: 100, ModTime: now})
	receiver.Add(&FileInfo{Path: "Series1/Season1/old_file.mkv", Size: 50, ModTime: now})

	receiver.Add(&FileInfo{Path: "test12", IsDir: true})
	receiver.Add(&FileInfo{Path: "test12/season1", IsDir: true})
	receiver.Add(&FileInfo{Path: "test12/season1/episode.mkv", Size: 200, ModTime: now})

	plan := CompareManifests(sender, receiver)

	// Should delete old_file.mkv (in sender-originated directory)
	if len(plan.FilesToDelete) != 1 {
		t.Errorf("Expected 1 file to delete, got %d", len(plan.FilesToDelete))
	}

	if len(plan.FilesToDelete) > 0 && plan.FilesToDelete[0] != "Series1/Season1/old_file.mkv" {
		t.Errorf("Expected to delete Series1/Season1/old_file.mkv, got %s", plan.FilesToDelete[0])
	}

	// Should NOT delete anything in test12/ (receiver-only)
	for _, path := range plan.FilesToDelete {
		if len(path) >= 6 && path[:6] == "test12" {
			t.Errorf("Should not delete files in receiver-only directory: %s", path)
		}
	}

	for _, path := range plan.DirsToDelete {
		if len(path) >= 6 && path[:6] == "test12" {
			t.Errorf("Should not delete receiver-only directory: %s", path)
		}
	}
}

func TestGetTopLevelDir(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"test12/season1/file.mkv", "test12"},
		{"Series1/Season1/episode.mkv", "Series1"},
		{"file.txt", "file.txt"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := getTopLevelDir(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}
