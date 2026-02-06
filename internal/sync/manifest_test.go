package sync

import (
	"testing"
	"time"
)

func TestManifest_Add(t *testing.T) {
	m := NewManifest("/test/root")

	file := &FileInfo{
		Path:    "dir/file.txt",
		Size:    100,
		ModTime: time.Now(),
		IsDir:   false,
	}

	m.Add(file)

	if !m.HasFile("dir/file.txt") {
		t.Error("File should exist in manifest")
	}

	dir := &FileInfo{
		Path:  "dir",
		IsDir: true,
	}

	m.Add(dir)

	if !m.HasDir("dir") {
		t.Error("Directory should exist in manifest")
	}
}

func TestFileInfo_NeedsUpdate(t *testing.T) {
	now := time.Now()
	older := now.Add(-1 * time.Hour)

	tests := []struct {
		name     string
		sender   *FileInfo
		receiver *FileInfo
		expected bool
	}{
		{
			name: "Same file",
			sender: &FileInfo{
				Size:    100,
				ModTime: now,
			},
			receiver: &FileInfo{
				Size:    100,
				ModTime: now,
			},
			expected: false,
		},
		{
			name: "Different size",
			sender: &FileInfo{
				Size:    200,
				ModTime: now,
			},
			receiver: &FileInfo{
				Size:    100,
				ModTime: now,
			},
			expected: true,
		},
		{
			name: "Sender newer",
			sender: &FileInfo{
				Size:    100,
				ModTime: now,
			},
			receiver: &FileInfo{
				Size:    100,
				ModTime: older,
			},
			expected: true,
		},
		{
			name: "Receiver newer",
			sender: &FileInfo{
				Size:    100,
				ModTime: older,
			},
			receiver: &FileInfo{
				Size:    100,
				ModTime: now,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sender.NeedsUpdate(tt.receiver)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}
