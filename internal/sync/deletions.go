package sync

import (
	"path/filepath"
	"sort"
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
		curr := filepath.ToSlash(path)

		// If it's a directory, the directory itself must exist on the sender to be considered managed.
		// If it doesn't exist on the sender, we ignore it (and its contents).
		if isDir {
			if _, exists := sender.GetDir(curr); !exists {
				return false
			}
		}

		// All parent directory components must also exist on the sender.
		for {
			curr = filepath.Dir(curr)
			if curr == "." || curr == "/" || curr == "" {
				break
			}
			// Normalize for lookup
			lookup := filepath.ToSlash(curr)
			if _, exists := sender.GetDir(lookup); !exists {
				return false
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
		if receiverFile.IsDir {
			if _, exists := sender.GetDir(path); !exists {
				// Don't delete directories in "flat" mode
				if rule != "flat" {
					dirsToDelete = append(dirsToDelete, path)
				}
			}
		} else {
			if _, exists := sender.GetFile(path); !exists {
				// Safety Check for Subdirectories:
				// If the file is in a subdirectory (not root), check if that subdirectory exists and is empty on the sender.
				// If the sender has the directory but zero files in it, we assume it's a "protected empty folder" (e.g. season folder)
				// and do NOT delete the receiver's files within it.
				parent := filepath.Dir(path)
				parent = filepath.ToSlash(parent)

				// Only apply protection if we are not at root
				if parent != "." && parent != "/" {
					// Check if parent dir exists in sender (it should, based on isManaged, but let's be double sure)
					if _, dirExists := sender.GetDir(parent); dirExists {
						// Check if sender has any files in this directory
						if sender.GetFileCountInDir(parent) == 0 {
							// Sender has the folder but no files -> Prevent deletion of receiver contents
							continue
						}
					}
				}

				filesToDelete = append(filesToDelete, path)
			}
		}
	}

	// Sort directories to ensure consistent deletion order (lexicographical)
	// The execution phase iterates backwards to delete leaf dirs first.
	sort.Strings(dirsToDelete)

	return filesToDelete, dirsToDelete
}
