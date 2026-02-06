package sync

// SyncPlan describes the actions needed to sync sender to receiver
type SyncPlan struct {
	FilesToSync   []*FileInfo       // Files to copy/update
	FilesToDelete []string          // Files to delete on receiver
	DirsToCreate  []string          // Directories to create on receiver
	DirsToDelete  []string          // Directories to delete on receiver
	Renames       map[string]string // oldPath -> newPath (on receiver)
}

// CompareManifests compares sender and receiver manifests and creates a sync plan
func CompareManifests(sender, receiver *Manifest) *SyncPlan {
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
	plan.FilesToDelete, plan.DirsToDelete = identifyDeletions(sender, receiver)

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

// identifyDeletions identifies files and directories on the receiver that are missing or different on the sender
func identifyDeletions(sender, receiver *Manifest) (filesToDelete, dirsToDelete []string) {
	filesToDelete = make([]string, 0)
	dirsToDelete = make([]string, 0)

	for path, receiverInfo := range receiver.Files {
		senderInfo, exists := sender.Files[path]

		// If it doesn't exist on sender, it's a deletion candidate
		if !exists {
			if receiverInfo.IsDir {
				dirsToDelete = append(dirsToDelete, path)
			} else {
				filesToDelete = append(filesToDelete, path)
			}
			continue
		}

		// If it exists but the type (file vs dir) changed, it must be deleted first
		if senderInfo.IsDir != receiverInfo.IsDir {
			if receiverInfo.IsDir {
				dirsToDelete = append(dirsToDelete, path)
			} else {
				filesToDelete = append(filesToDelete, path)
			}
		}
	}

	return filesToDelete, dirsToDelete
}
