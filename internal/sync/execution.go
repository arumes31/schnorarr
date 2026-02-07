package sync

import (
	"fmt"
	"log"
	"path/filepath"
	"time"
)

// executeSyncPhase executes the copy/update/rename/mkdir part of the plan (Task 1)
// It returns a map of "touched" directory paths (where files were added/updated)
func (e *Engine) executeSyncPhase(plan *SyncPlan, targetManifest *Manifest) (map[string]bool, error) {
	timestamp := time.Now().Format("2006/01/02 15:04:05")
	isDryRun := e.isDryRun()
	touchedDirs := make(map[string]bool)

	if isDryRun {
		log.Printf("[%s] === DRY RUN (Task 1: Sync) ===", e.config.ID)
	}

	// Create directories first
	for _, dirPath := range plan.DirsToCreate {
		fullPath := filepath.Join(e.config.TargetDir, dirPath)
		// Mark parent directory as touched since we're modifying its structure
		parentDir := filepath.Dir(dirPath)
		if parentDir == "." {
			parentDir = ""
		}
		touchedDirs[parentDir] = true

		if isDryRun {
			log.Printf("[%s] [DRY RUN] Would create directory: %s", e.config.ID, dirPath)
			e.reportEvent(timestamp, "DRY-Created", dirPath, 0)
		} else {
			if err := e.transferer.CreateDir(fullPath); err != nil {
				e.reportError(fmt.Sprintf("Failed to create dir %s: %v", dirPath, err))
				continue
			}
			log.Printf("[%s] Created directory: %s", e.config.ID, dirPath)
			targetManifest.Add(&FileInfo{Path: filepath.ToSlash(dirPath), IsDir: true})
		}
	}

	// Handle Renames
	for oldPath, newPath := range plan.Renames {
		// Mark source and destination directories as touched
		oldDir := filepath.Dir(oldPath)
		if oldDir == "." {
			oldDir = ""
		}
		newDir := filepath.Dir(newPath)
		if newDir == "." {
			newDir = ""
		}
		touchedDirs[oldDir] = true
		touchedDirs[newDir] = true

		if isDryRun {
			log.Printf("[%s] [DRY RUN] Would rename: %s -> %s", e.config.ID, oldPath, newPath)
			e.reportEvent(timestamp, "DRY-Renamed", fmt.Sprintf("%s -> %s", oldPath, newPath), 0)
		} else {
			oldFullPath := filepath.Join(e.config.TargetDir, oldPath)
			newFullPath := filepath.Join(e.config.TargetDir, newPath)

			if err := e.transferer.RenameFile(oldFullPath, newFullPath); err != nil {
				log.Printf("[%s] Failed to rename %s to %s: %v", e.config.ID, oldPath, newPath, err)
				continue
			}
			log.Printf("[%s] Renamed: %s -> %s", e.config.ID, oldPath, newPath)

			if file, exists := targetManifest.Files[oldPath]; exists {
				delete(targetManifest.Files, oldPath)
				file.Path = newPath
				targetManifest.Files[newPath] = file
			}
			e.reportEvent(timestamp, "Renamed", fmt.Sprintf("%s -> %s", oldPath, newPath), 0)
		}
	}

	// Copy/update files
	for _, file := range plan.FilesToSync {
		// Mark directory as touched
		fileDir := filepath.Dir(file.Path)
		if fileDir == "." {
			fileDir = ""
		}
		touchedDirs[fileDir] = true

		if isDryRun {
			log.Printf("[%s] [DRY RUN] Would sync file: %s (%d bytes)", e.config.ID, file.Path, file.Size)
			e.reportEvent(timestamp, "DRY-Added", file.Path, file.Size)
		} else {
			srcPath := filepath.Join(e.config.SourceDir, file.Path)
			dstPath := filepath.Join(e.config.TargetDir, file.Path)

			if err := e.transferer.CopyFile(srcPath, dstPath); err != nil {
				e.reportError(fmt.Sprintf("Failed to copy %s: %v", file.Path, err))
				continue
			}
			log.Printf("[%s] Synced file: %s (%d bytes)", e.config.ID, file.Path, file.Size)

			targetManifest.Add(&FileInfo{
				Path:    file.Path,
				Size:    file.Size,
				ModTime: file.ModTime,
				IsDir:   false,
			})
			e.reportEvent(timestamp, "Added", file.Path, file.Size)
		}
		
		// Update remaining bytes in plan
		e.pausedMu.Lock()
		e.planRemainingBytes -= file.Size
		if e.planRemainingBytes < 0 {
			e.planRemainingBytes = 0
		}
		e.pausedMu.Unlock()
	}
	return touchedDirs, nil
}

// executeCleanupPhase executes the deletion part of the plan (Task 2)
func (e *Engine) executeCleanupPhase(plan *SyncPlan, targetManifest *Manifest, touchedDirs map[string]bool) error {
	timestamp := time.Now().Format("2006/01/02 15:04:05")
	isDryRun := e.isDryRun()

	if len(plan.FilesToDelete) == 0 && len(plan.DirsToDelete) == 0 {
		return nil
	}

	log.Printf("[%s] Starting Task 2: Post-Sync Cleanup", e.config.ID)

	if isDryRun {
		log.Printf("[%s] === DRY RUN (Task 2: Cleanup) ===", e.config.ID)
	}

	// Delete files
	for _, filePath := range plan.FilesToDelete {
		if isDryRun {
			log.Printf("[%s] [DRY RUN] Would delete file: %s", e.config.ID, filePath)
			e.reportEvent(timestamp, "DRY-Deleted", filePath, 0)
		} else {
			fullPath := filepath.Join(e.config.TargetDir, filePath)
			if err := e.transferer.DeleteFile(fullPath); err != nil {
				e.reportError(fmt.Sprintf("Failed to delete file %s: %v", filePath, err))
				continue
			}
			log.Printf("[%s] Deleted file: %s", e.config.ID, filePath)
			delete(targetManifest.Files, filePath)
			e.reportEvent(timestamp, "Deleted", filePath, 0)
		}
	}

	// Delete directories
	for i := len(plan.DirsToDelete) - 1; i >= 0; i-- {
		dirPath := plan.DirsToDelete[i]

		if isDryRun {
			log.Printf("[%s] [DRY RUN] Would delete directory: %s", e.config.ID, dirPath)
			e.reportEvent(timestamp, "DRY-Deleted", dirPath, 0)
		} else {
			fullPath := filepath.Join(e.config.TargetDir, dirPath)
			if err := e.transferer.DeleteDir(fullPath); err != nil {
				e.reportError(fmt.Sprintf("Failed to delete dir %s: %v", dirPath, err))
				continue
			}
			log.Printf("[%s] Deleted directory: %s", e.config.ID, dirPath)
			delete(targetManifest.Dirs, dirPath)
			delete(targetManifest.Files, dirPath)
		}
	}
	return nil
}
