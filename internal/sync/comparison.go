package sync

import (
	"path/filepath"
	"strings"
)

// SyncPlan describes the actions needed to sync sender to receiver
type SyncPlan struct {
	FilesToSync   []*FileInfo // Files to copy/update
	FilesToDelete []string    // Files to delete on receiver
	DirsToCreate  []string    // Directories to create on receiver
	DirsToDelete  []string    // Directories to delete on receiver
}

// CompareManifests compares sender and receiver manifests and creates a sync plan
func CompareManifests(sender, receiver *Manifest) *SyncPlan {
	plan := &SyncPlan{
		FilesToSync:   make([]*FileInfo, 0),
		FilesToDelete: make([]string, 0),
		DirsToCreate:  make([]string, 0),
		DirsToDelete:  make([]string, 0),
	}

	// Step 1: Find files to sync (new or modified)
	for path, senderFile := range sender.Files {
		if senderFile.IsDir {
			// Check if directory needs to be created
			if !receiver.HasDir(path) {
				plan.DirsToCreate = append(plan.DirsToCreate, path)
			}
		} else {
			// Check if file needs sync
			receiverFile, exists := receiver.Files[path]
			if !exists || senderFile.NeedsUpdate(receiverFile) {
				plan.FilesToSync = append(plan.FilesToSync, senderFile)
			}
		}
	}

	// Step 2: Find files/dirs to delete (smart deletion logic)
	plan.FilesToDelete, plan.DirsToDelete = identifyDeletions(sender, receiver)

	return plan
}

// identifyDeletions implements smart deletion logic
// Only deletes from receiver directories that originated from sender
func identifyDeletions(sender, receiver *Manifest) (filesToDelete, dirsToDelete []string) {
	filesToDelete = make([]string, 0)
	dirsToDelete = make([]string, 0)

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
		// Get top-level directory for this file
		topLevel := getTopLevelDir(path)

		// Skip if this file is in a receiver-only directory
		if receiverOnlyDirs[topLevel] {
			continue
		}

		// Check if file exists on sender
		if !sender.HasFile(path) {
			if receiverFile.IsDir {
				dirsToDelete = append(dirsToDelete, path)
			} else {
				filesToDelete = append(filesToDelete, path)
			}
		}
	}

	return filesToDelete, dirsToDelete
}

// getTopLevelDir extracts the first directory component from a path
// e.g., "test12/season1/file.mkv" -> "test12"
func getTopLevelDir(path string) string {
	normalized := filepath.ToSlash(path)
	parts := strings.Split(normalized, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
