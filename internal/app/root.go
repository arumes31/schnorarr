package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errUnsafePath = errors.New("path is outside the configured data root or uses a symbolic link")

func configuredDataRoot(getenv func(string) string) string {
	if root := strings.TrimSpace(getenv("RSYNC_MODULE_PATH")); root != "" {
		return root
	}
	if root := strings.TrimSpace(getenv("SOURCE_DIR")); root != "" {
		return root
	}
	return "/data"
}

func cleanRelativePath(raw string, allowRoot bool) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errUnsafePath
	}
	cleaned := filepath.Clean(filepath.FromSlash(raw))
	if filepath.IsAbs(cleaned) || filepath.VolumeName(cleaned) != "" || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", errUnsafePath
	}
	if cleaned == "." && !allowRoot {
		return "", errUnsafePath
	}
	return cleaned, nil
}

// rootedPath rejects every symlink component. os.Root also prevents escape,
// but rejecting links makes destructive intent unambiguous and testable.
func rootedPath(root *os.Root, raw string, allowRoot, allowMissing bool) (string, error) {
	relative, err := cleanRelativePath(raw, allowRoot)
	if err != nil || relative == "." {
		return relative, err
	}

	current := ""
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for index, part := range parts {
		if current == "" {
			current = part
		} else {
			current = filepath.Join(current, part)
		}
		info, statErr := root.Lstat(current)
		if statErr != nil {
			if allowMissing && os.IsNotExist(statErr) && index == len(parts)-1 {
				return relative, nil
			}
			return "", fmt.Errorf("%w: %v", errUnsafePath, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errUnsafePath
		}
	}
	return relative, nil
}
