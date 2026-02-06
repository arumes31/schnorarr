package sync

import (
	"path/filepath"
	"strings"
)

// SyncPlan describes the actions needed to sync sender to receiver
type SyncPlan struct {
	FilesToSync   []*FileInfo       // Files to copy/update
	FilesToDelete []string          // Files to delete on receiver
	DirsToCreate  []string          // Directories to create on receiver
	DirsToDelete  []string          // Directories to delete on receiver
	Renames       map[string]string // oldPath -> newPath (on receiver)
}

// CompareManifests compares sender and receiver manifests and creates a sync plan
func CompareManifests(sender, receiver *Manifest, rule string) *SyncPlan {
	plan := &SyncPlan{
		FilesToSync:   make([]*FileInfo, 0),
		FilesToDelete: make([]string, 0),
		DirsToCreate:  make([]string, 0),
		DirsToDelete:  make([]string, 0),
		Renames:       make(map[string]string),
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
	plan.FilesToDelete, plan.DirsToDelete = identifyDeletions(sender, receiver, rule)

	// Step 3: Detect renames (match deletes with syncs)
	plan.detectRenames(receiver)

	return plan
}

// detectRenames matches files to be deleted with files to be synced by Size/ModTime
func (p *SyncPlan) detectRenames(receiver *Manifest) {
	if len(p.FilesToDelete) == 0 || len(p.FilesToSync) == 0 {
		return
	}

	// Map to track which sync targets are already matched
	matchedSyncs := make(map[string]bool)

	newSyncList := make([]*FileInfo, 0)
	newDeleteList := make([]string, 0)

	// Track which deletions were converted to renames
	convertedDeletes := make(map[string]bool)

	for _, delPath := range p.FilesToDelete {
		delFile, ok := receiver.Files[delPath]
		if !ok || delFile.IsDir {
			newDeleteList = append(newDeleteList, delPath)
			continue
		}

		found := false
		for _, syncFile := range p.FilesToSync {
			if matchedSyncs[syncFile.Path] {
				continue
			}

			// Match by Size and ModTime (ignore path)
			if syncFile.Size == delFile.Size && syncFile.ModTime.Unix() == delFile.ModTime.Unix() {
				p.Renames[delPath] = syncFile.Path
				matchedSyncs[syncFile.Path] = true
				convertedDeletes[delPath] = true
				found = true
				break
			}
		}

		if !found {
			newDeleteList = append(newDeleteList, delPath)
		}
	}

	// Filter out matched sync files
	for _, syncFile := range p.FilesToSync {
		if !matchedSyncs[syncFile.Path] {
			newSyncList = append(newSyncList, syncFile)
		}
	}

	p.FilesToSync = newSyncList
	p.FilesToDelete = newDeleteList
}

// identifyDeletions implements smart deletion logic
// Only deletes from receiver directories that originated from sender
func identifyDeletions(sender, receiver *Manifest, rule string) (filesToDelete, dirsToDelete []string) {
	filesToDelete = make([]string, 0)
	dirsToDelete = make([]string, 0)

	// Identify receiver-only top-level directories
	receiverOnlyDirs := make(map[string]bool)

	// In "flat" mode, we act as a pure mirror, so we don't protect receiver-only dirs
	if rule != "flat" {
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
// e.g., "Series1" -> "Series1"
func getTopLevelDir(path string) string {
	normalized := filepath.ToSlash(path)
	parts := strings.Split(normalized, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
