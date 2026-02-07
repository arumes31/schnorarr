package sync

import (
	"fmt"
	"path/filepath"
	"time"
)

// executeSyncPhase executes the sync part of the plan
func (e *Engine) executeSyncPhase(plan *SyncPlan, targetManifest *Manifest) (map[string]bool, error) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	isDryRun := e.isDryRun()
	touchedDirs := make(map[string]bool)

	for _, dirPath := range plan.DirsToCreate {
		fullPath := filepath.Join(e.config.TargetDir, dirPath)
		parentDir := filepath.Dir(dirPath)
		if parentDir == "." {
			parentDir = ""
		}
		touchedDirs[parentDir] = true
		if isDryRun {
			e.reportEvent(timestamp, "DRY-Created", dirPath, 0)
		} else {
			if err := e.transferer.CreateDir(fullPath); err != nil {
				e.reportError(fmt.Sprintf("Failed to create dir %s: %v", dirPath, err))
				continue
			}
			targetManifest.Add(&FileInfo{Path: filepath.ToSlash(dirPath), IsDir: true})
		}
	}

	for oldPath, newPath := range plan.Renames {
		touchedDirs[filepath.Dir(oldPath)] = true
		touchedDirs[filepath.Dir(newPath)] = true
		if isDryRun {
			e.reportEvent(timestamp, "DRY-Renamed", fmt.Sprintf("%s -> %s", oldPath, newPath), 0)
		} else {
			oldFullPath, newFullPath := filepath.Join(e.config.TargetDir, oldPath), filepath.Join(e.config.TargetDir, newPath)
			if err := e.transferer.RenameFile(oldFullPath, newFullPath); err == nil {
				if file, exists := targetManifest.Files[oldPath]; exists {
					delete(targetManifest.Files, oldPath)
					file.Path = newPath
					targetManifest.Files[newPath] = file
				}
				e.reportEvent(timestamp, "Renamed", fmt.Sprintf("%s -> %s", oldPath, newPath), 0)
			} else {
				e.reportError(fmt.Sprintf("Failed to rename %s -> %s: %v", oldPath, newPath, err))
			}
		}
	}

	for _, file := range plan.FilesToSync {
		touchedDirs[filepath.Dir(file.Path)] = true
		if isDryRun {
			e.reportEvent(timestamp, "DRY-Added", file.Path, file.Size)
		} else {
			srcPath, dstPath := filepath.Join(e.config.SourceDir, file.Path), filepath.Join(e.config.TargetDir, file.Path)
			if err := e.transferer.CopyFile(srcPath, dstPath); err != nil {
				e.reportError(fmt.Sprintf("Failed to copy %s: %v", file.Path, err))
				continue
			}
			targetManifest.Add(&FileInfo{Path: file.Path, Size: file.Size, ModTime: file.ModTime, IsDir: false})
			e.reportEvent(timestamp, "Added", file.Path, file.Size)
		}
		e.pausedMu.Lock()
		e.planRemainingBytes -= file.Size
		if e.planRemainingBytes < 0 {
			e.planRemainingBytes = 0
		}
		e.pausedMu.Unlock()
	}
	return touchedDirs, nil
}

func (e *Engine) executeCleanupPhase(plan *SyncPlan, targetManifest *Manifest, touchedDirs map[string]bool) error {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	isDryRun := e.isDryRun()
	if len(plan.FilesToDelete) == 0 && len(plan.DirsToDelete) == 0 {
		return nil
	}

	for _, filePath := range plan.FilesToDelete {
		if isDryRun {
			e.reportEvent(timestamp, "DRY-Deleted", filePath, 0)
		} else {
			if err := e.transferer.DeleteFile(filepath.Join(e.config.TargetDir, filePath)); err == nil {
				delete(targetManifest.Files, filePath)
				e.reportEvent(timestamp, "Deleted", filePath, 0)
			} else {
				e.reportError(fmt.Sprintf("Failed to delete %s: %v", filePath, err))
			}
		}
	}

	for i := len(plan.DirsToDelete) - 1; i >= 0; i-- {
		dirPath := plan.DirsToDelete[i]
		if isDryRun {
			e.reportEvent(timestamp, "DRY-Deleted", dirPath, 0)
		} else {
			if err := e.transferer.DeleteDir(filepath.Join(e.config.TargetDir, dirPath)); err == nil {
				delete(targetManifest.Dirs, dirPath)
				delete(targetManifest.Files, dirPath)
			}
		}
	}
	return nil
}
