package tailer

import (
	"bufio"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const RsyncLog = "/tmp/rsync.log"

// EventCallback is called when a sync event is parsed from logs
type EventCallback func(timestamp, action, path string, size int64)

// ErrorCallback is called when an error is found in logs
type ErrorCallback func(msg string)

// Tailer continuously reads and parses the rsync log file
type Tailer struct {
	onEvent EventCallback
	onError ErrorCallback
}

// New creates a new log tailer
func New(onEvent EventCallback, onError ErrorCallback) *Tailer {
	return &Tailer{
		onEvent: onEvent,
		onError: onError,
	}
}

// Start begins tailing the rsync log file
func (t *Tailer) Start() {
	log.Println("Starting rsync log tailer...")

	// Wait for log file to exist
	for {
		if _, err := os.Stat(RsyncLog); os.IsNotExist(err) {
			time.Sleep(1 * time.Second)
			continue
		}
		break
	}

	file, err := os.Open(RsyncLog)
	if err != nil {
		log.Printf("Failed to open Log: %v", err)
		return
	}
	defer func() { _ = file.Close() }()

	// Move to end
	if _, err := file.Seek(0, 2); err != nil {
		log.Printf("Seek Error: %v", err)
	}
	reader := bufio.NewReader(file)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				time.Sleep(1 * time.Second)
				continue
			}
			log.Printf("Read Error: %v", err)
			continue
		}

		t.parseLine(strings.TrimSpace(line))
	}
}

// parseLine extracts sync events from log lines
func (t *Tailer) parseLine(line string) {
	if strings.Contains(line, "[ERROR]") {
		parts := strings.SplitN(line, "[ERROR]", 2)
		if len(parts) > 1 && t.onError != nil {
			t.onError(strings.TrimSpace(parts[1]))
		}
	} else if strings.Contains(line, "[WRAPPER]") {
		parts := strings.SplitN(line, "[WRAPPER]", 2)
		if len(parts) > 1 {
			timestamp := strings.TrimSpace(parts[0])
			content := strings.TrimSpace(parts[1])

			action, path := t.parseContent(content)
			if path != "" && t.onEvent != nil {
				// Filter for media extensions
				lowerPath := strings.ToLower(path)
				if strings.Contains(lowerPath, ".mkv") ||
					strings.Contains(lowerPath, ".mp4") ||
					strings.Contains(lowerPath, ".avi") {

					var size int64 = 0
					if action == "Added" {
						// Try to get file size
						fullPath := filepath.Join("/data", path)
						if info, err := os.Stat(fullPath); err == nil {
							size = info.Size()
						}
					}
					t.onEvent(timestamp, action, path, size)
				}
			}
		}
	}
}

// parseContent extracts action and path from rsync output
func (t *Tailer) parseContent(content string) (action, path string) {
	if strings.Contains(content, "*deleting") {
		action = "Deleted"
		pathParts := strings.SplitN(content, "*deleting", 2)
		if len(pathParts) > 1 {
			path = strings.TrimSpace(pathParts[1])
		}
	} else if strings.Contains(content, ">f") || strings.Contains(content, "<f") {
		action = "Added"
		// Content format: "<f+++++++++ filename with spaces.mkv"
		// Find first space to separate itemize code from filename
		if idx := strings.Index(content, " "); idx != -1 {
			path = strings.TrimSpace(content[idx:])
		}
	} else {
		// Fallback check for media files directly
		lowerContent := strings.ToLower(content)
		if strings.Contains(lowerContent, ".mkv") ||
			strings.Contains(lowerContent, ".mp4") ||
			strings.Contains(lowerContent, ".avi") {
			path = content
		}
	}
	return action, path
}
