package sync

import (
	"path/filepath"
	"strings"
)

// identifyDeletions implements smart deletion logic
// Only deletes from receiver directories that originated from sender
func identifyDeletions(sender, receiver *Manifest, rule string) (filesToDelete, dirsToDelete []string) {
	filesToDelete = make([]string, 0)

	// Identify receiver-only top-level directories
	receiverOnlyDirs := make(map[string]bool)

	for dir := range receiver.Dirs {
		// Get top-level directory
		topLevel := getTopLevelDir(dir)
		if topLevel == "" {
			continue
		}

		// Check if this top-level dir exists on sender
		if !sender.HasDir(topLevel) && !sender.HasFile(topLevel) {
			receiverOnlyDirs[topLevel] = true
		}
	}

	// Process receiver files
	for path, receiverFile := range receiver.Files {
		// Get top-level component for this file
		topLevel := getTopLevelDir(path)

		// Skip if this path is protected (resides in or IS a receiver-only top-level dir)
		if receiverOnlyDirs[topLevel] {
			continue
		}

		// Check if file exists on sender
		if !sender.HasFile(path) {
			if !receiverFile.IsDir {
				filesToDelete = append(filesToDelete, path)
			}
		}
	}

	return filesToDelete, []string{}
}

// getTopLevelDir extracts the first directory component from a path
// e.g., "test12/season1/file.mkv" -> "test12"
// e.g., "Series1" -> "Series1"
func getTopLevelDir(path string) string {
	normalized := filepath.ToSlash(path)
	parts := strings.Split(normalized, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
