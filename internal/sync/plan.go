package sync

import "time"

// ConflictDetail provides side-by-side info for files that exist on both ends but differ
type ConflictDetail struct {
	Path         string    `json:"path"`
	SourceSize   int64     `json:"source_size"`
	SourceTime   time.Time `json:"source_time"`
	ReceiverSize int64     `json:"receiver_size"`
	ReceiverTime time.Time `json:"receiver_time"`
}

// SyncPlan describes the actions needed to sync sender to receiver
type SyncPlan struct {
	FilesToSync   []*FileInfo       // Files to copy/update
	FilesToDelete []string          // Files to delete on receiver
	DirsToCreate  []string          // Directories to create on receiver
	DirsToDelete  []string          // Directories to delete on receiver
	Renames       map[string]string // oldPath -> newPath (on receiver)
	Conflicts     []*ConflictDetail // Detailed side-by-side info
}

// CompareManifests compares sender and receiver manifests and creates a sync plan
func CompareManifests(sender, receiver *Manifest, rule string) *SyncPlan {
	plan := &SyncPlan{
		FilesToSync:   make([]*FileInfo, 0),
		FilesToDelete: make([]string, 0),
		DirsToCreate:  make([]string, 0),
		DirsToDelete:  make([]string, 0),
		Renames:       make(map[string]string),
		Conflicts:     make([]*ConflictDetail, 0),
	}

	for path, senderFile := range sender.Files {
		if senderFile.IsDir {
			if !receiver.HasDir(path) {
				plan.DirsToCreate = append(plan.DirsToCreate, path)
			}
		} else {
			receiverFile, exists := receiver.Files[path]
			if !exists {
				plan.FilesToSync = append(plan.FilesToSync, senderFile)
			} else if senderFile.NeedsUpdate(receiverFile) {
				plan.FilesToSync = append(plan.FilesToSync, senderFile)
				plan.Conflicts = append(plan.Conflicts, &ConflictDetail{
					Path:         path,
					SourceSize:   senderFile.Size,
					SourceTime:   senderFile.ModTime,
					ReceiverSize: receiverFile.Size,
					ReceiverTime: receiverFile.ModTime,
				})
			}
		}
	}
	
	plan.FilesToDelete, plan.DirsToDelete = identifyDeletions(sender, receiver, rule)
	plan.detectRenames(receiver)
	return plan
}

func (p *SyncPlan) detectRenames(receiver *Manifest) {
	if len(p.FilesToDelete) == 0 || len(p.FilesToSync) == 0 {
		return
	}
	matchedSyncs := make(map[string]bool)
	newSyncList := make([]*FileInfo, 0)
	newDeleteList := make([]string, 0)
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
			if syncFile.Size == delFile.Size && syncFile.ModTime.Unix() == delFile.ModTime.Unix() {
				p.Renames[delPath] = syncFile.Path
				matchedSyncs[syncFile.Path] = true
				found = true
				break
			}
		}
		if !found {
			newDeleteList = append(newDeleteList, delPath)
		}
	}
	for _, syncFile := range p.FilesToSync {
		if !matchedSyncs[syncFile.Path] {
			newSyncList = append(newSyncList, syncFile)
		}
	}
	p.FilesToSync = newSyncList
	p.FilesToDelete = newDeleteList
}