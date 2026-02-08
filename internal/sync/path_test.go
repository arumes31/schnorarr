package sync

import "testing"

func TestResolveTargetPath(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		destHost   string
		destModule string
		expected   string
	}{
		{
			name:       "daemon match",
			target:     "user@host::module/path/file.txt",
			destHost:   "host",
			destModule: "module",
			expected:   "/data/path/file.txt",
		},
		{
			name:       "rsync match",
			target:     "rsync://host/module/path/file.txt",
			destHost:   "host",
			destModule: "module",
			expected:   "/data/path/file.txt",
		},
		{
			name:       "rsync match with user and port",
			target:     "rsync://user@host:873/module/path/file.txt",
			destHost:   "host",
			destModule: "module",
			expected:   "/data/path/file.txt",
		},
		{
			name:       "no match host",
			target:     "rsync://other/module/path",
			destHost:   "host",
			destModule: "module",
			expected:   "rsync://other/module/path",
		},
		{
			name:       "no match module",
			target:     "rsync://host/other/path",
			destHost:   "host",
			destModule: "module",
			expected:   "rsync://host/other/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveTargetPath(tt.target, tt.destHost, tt.destModule)
			if result != tt.expected {
				t.Errorf("ResolveTargetPath() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestUpdateTargetHost(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		destHost string
		expected string
	}{
		{
			name:     "daemon update",
			target:   "user@old::module/path",
			destHost: "new",
			expected: "user@new::module/path",
		},
		{
			name:     "rsync update",
			target:   "rsync://old/module/path",
			destHost: "new",
			expected: "rsync://new/module/path",
		},
		{
			name:     "rsync update with user",
			target:   "rsync://user@old/module/path",
			destHost: "new",
			expected: "rsync://user@new/module/path",
		},
		{
			name:     "rsync update with port",
			target:   "rsync://old:873/module/path",
			destHost: "new",
			expected: "rsync://new:873/module/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := UpdateTargetHost(tt.target, tt.destHost)
			if result != tt.expected {
				t.Errorf("UpdateTargetHost() = %q, want %q", result, tt.expected)
			}
		})
	}
}
