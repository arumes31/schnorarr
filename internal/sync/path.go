package sync

import (
	"regexp"
	"strings"
)

// ResolveTargetPath attempts to convert an rsync-style target string
// (e.g., syncuser@receiver::video-sync/receiver1) into a local path
// if it matches the current environment's module and destination.
func ResolveTargetPath(target, destHost, destModule string) string {
	if destHost == "" || destModule == "" {
		return target
	}

	// Pattern for syncuser@host::module/path
	pattern := regexp.MustCompile(`^[^@]+@([^:]+)::([^/]+)(/.*)?$`)
	matches := pattern.FindStringSubmatch(target)

	if len(matches) >= 3 {
		host := matches[1]
		module := matches[2]
		subPath := ""
		if len(matches) > 3 {
			subPath = matches[3]
		}

		// If it matches our destination, treat it as /data/subPath
		if host == destHost && module == destModule {
			resolved := "/data" + subPath
			return strings.ReplaceAll(resolved, "//", "/")
		}
	}

	return target
}
