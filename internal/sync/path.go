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

	var host, module, subPath string

	// Handle rsync://host/module/path
	if strings.HasPrefix(target, "rsync://") {
		pathPart := strings.TrimPrefix(target, "rsync://")
		if idx := strings.Index(pathPart, "@"); idx != -1 {
			pathPart = pathPart[idx+1:]
		}
		idx := strings.Index(pathPart, "/")
		if idx != -1 {
			host = pathPart[:idx]
			if pIdx := strings.Index(host, ":"); pIdx != -1 {
				host = host[:pIdx]
			}
			modulePath := pathPart[idx+1:]
			pParts := strings.SplitN(modulePath, "/", 2)
			module = pParts[0]
			if len(pParts) > 1 {
				subPath = "/" + pParts[1]
			}
		}
	} else {
		// Pattern for syncuser@host::module/path
		pattern := regexp.MustCompile(`^[^@]+@([^:]+)::([^/]+)(/.*)?$`)
		matches := pattern.FindStringSubmatch(target)
		if len(matches) >= 3 {
			host = matches[1]
			module = matches[2]
			if len(matches) > 3 {
				subPath = matches[3]
			}
		}
	}

	// If it matches our destination, treat it as /data/subPath
	if host == destHost && module == destModule {
		resolved := "/data" + subPath
		return strings.ReplaceAll(resolved, "//", "/")
	}

	return target
}

// UpdateTargetHost replaces the host part of an rsync URI with destHost
func UpdateTargetHost(target, destHost string) string {
	if destHost == "" {
		return target
	}

	// Pattern for syncuser@host::module/path
	pattern := regexp.MustCompile(`^([^@]+@)([^:]+)(::.*)$`)
	if pattern.MatchString(target) {
		return pattern.ReplaceAllString(target, "${1}"+destHost+"${3}")
	}

	// Pattern for rsync://host/module/path or rsync://user@host/module/path
	if strings.HasPrefix(target, "rsync://") {
		// rsync://[user@][host][:port]/path
		pathPart := strings.TrimPrefix(target, "rsync://")
		if strings.Contains(pathPart, "@") {
			parts := strings.SplitN(pathPart, "@", 2)
			userPart := parts[0]
			remaining := parts[1]
			// Find end of host (slash or colon)
			idx := strings.IndexAny(remaining, "/:")
			if idx != -1 {
				return "rsync://" + userPart + "@" + destHost + remaining[idx:]
			}
			return "rsync://" + userPart + "@" + destHost
		} else {
			idx := strings.IndexAny(pathPart, "/:")
			if idx != -1 {
				return "rsync://" + destHost + pathPart[idx:]
			}
			return "rsync://" + destHost
		}
	}

	return target
}
