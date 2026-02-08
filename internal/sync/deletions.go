package sync

import (
	"path/filepath"
	"sort"
	"strings"
)

// identifyDeletions implements smart deletion logic
// Only deletes from receiver directories that originated from sender
func identifyDeletions(sender, receiver *Manifest, rule string) (filesToDelete, dirsToDelete []string) {
	filesToDelete = make([]string, 0)
	dirsToDelete = make([]string, 0)

	// isManaged checks if a path should be handled by the sync process.
	// A path is "managed" only if its parent hierarchy exists on the sender.
	// Additionally, a directory itself is only managed if it exists on the sender.
	// This "Smart Deletion" protects receiver-only folders from being deleted.
	isManaged := func(path string, isDir bool) bool {
		normalized := filepath.ToSlash(path)
		parts := strings.Split(normalized, "/")

		current := ""
		for i := 0; i < len(parts); i++ {
			if i == 0 {
				current = parts[i]
			} else {
				current = current + "/" + parts[i]
			}

			if i < len(parts)-1 {
				// Check parent components
				if !sender.HasDir(current) && !sender.HasFile(current) {
					return false
				}
			} else if isDir {
				// The directory itself must exist on sender to be managed
				if !sender.HasDir(current) && !sender.HasFile(current) {
					return false
				}
			}
		}
		return true
	}

	for path, receiverFile := range receiver.Files {
		// Skip if the path is not managed by the sender
		if !isManaged(path, receiverFile.IsDir) {
			continue
		}

		// If the item doesn't exist on the sender, it's a candidate for deletion
		if !sender.HasFile(path) && !sender.HasDir(path) {
			if receiverFile.IsDir {
				// Don't delete directories in "flat" mode
				if rule != "flat" {
					dirsToDelete = append(dirsToDelete, path)
				}
			} else {
				filesToDelete = append(filesToDelete, path)
			}
		}
	}

	// Sort directories to ensure consistent deletion order (lexicographical)
	// The execution phase iterates backwards to delete leaf dirs first.
	sort.Strings(dirsToDelete)

	return filesToDelete, dirsToDelete
}

