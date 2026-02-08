package sync

import (
	"path/filepath"
	"strings"
)

// identifyDeletions implements smart deletion logic
// Only deletes from receiver directories that originated from sender
func identifyDeletions(sender, receiver *Manifest, rule string) (filesToDelete, dirsToDelete []string) {
	filesToDelete = make([]string, 0)

	// Identify receiver-only directories
	// A directory is considered "protected" if it or any of its parent directories
	// do not exist on the sender.
	protectedDirs := make(map[string]bool)

	for dir := range receiver.Dirs {
		parts := strings.Split(filepath.ToSlash(dir), "/")
		current := ""
		for i, part := range parts {
			if i == 0 {
				current = part
			} else {
				current = current + "/" + part
			}

			// If this directory component does not exist on the sender,
			// then the entire directory tree from here down is protected.
			if !sender.HasDir(current) && !sender.HasFile(current) {
				protectedDirs[dir] = true
				break
			}
		}
	}

	// Process receiver files
	for path, receiverFile := range receiver.Files {
		if receiverFile.IsDir {
			continue
		}

		// Check if the file's parent directory is protected
		parent := filepath.ToSlash(filepath.Dir(path))
		if parent == "." {
			// At root, check if the file itself exists on sender
			if !sender.HasFile(path) {
				filesToDelete = append(filesToDelete, path)
			}
			continue
		}

		isProtected := false
		parts := strings.Split(parent, "/")
		current := ""
		for i, part := range parts {
			if i == 0 {
				current = part
			} else {
				current = current + "/" + part
			}
			if protectedDirs[current] {
				isProtected = true
				break
			}
		}

		if isProtected {
			continue
		}

		// Check if file exists on sender
		if !sender.HasFile(path) {
			filesToDelete = append(filesToDelete, path)
		}
	}

	return filesToDelete, []string{}
}

