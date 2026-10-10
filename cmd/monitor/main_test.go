package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"schnorarr/internal/storage"
)

func TestCheckRsyncStorage(t *testing.T) {
	root, config := t.TempDir(), t.TempDir()
	token := strings.Repeat("a", 64)
	receiver := storage.NewReceiver(root, filepath.Join(config, "storage-tokens.json"))
	for _, folder := range []string{"TV shows", "movies"} {
		full := filepath.Join(root, folder)
		if err := os.Mkdir(full, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(full, storage.MarkerName), []byte(token), 0600); err != nil {
			t.Fatal(err)
		}
		if err := receiver.Verify(context.Background(), folder, token); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DB_PATH", filepath.Join(config, "history.db"))
	t.Setenv("SOURCE_DIR", root)
	t.Setenv("RSYNC_MODULE_NAME", "video-sync")
	for _, tc := range []struct {
		name       string
		args       []string
		request    string
		modulePath string
		wantErr    bool
	}{
		{name: "multiple paths with spaces", args: []string{"rsyncd", "--server", ".", "TV shows/episode one.mkv", "movies/film.mkv"}, request: "video-sync/TV shows/episode one.mkv video-sync/movies/film.mkv", modulePath: root},
		{name: "reject unregistered second path", args: []string{"rsyncd", ".", "movies/film.mkv", "other/film.mkv"}, request: "video-sync/movies/film.mkv video-sync/other/film.mkv", modulePath: root, wantErr: true},
		{name: "reject traversal", args: []string{"rsyncd", ".", "../outside"}, request: "video-sync/movies/film.mkv", modulePath: root, wantErr: true},
		{name: "reject missing separator", args: []string{"rsyncd", "movies/film.mkv"}, request: "video-sync/movies/film.mkv", modulePath: root, wantErr: true},
		{name: "reject missing paths", args: []string{"rsyncd", "."}, request: "video-sync/movies/film.mkv", modulePath: root, wantErr: true},
		{name: "module root overrides source dir", args: []string{"rsyncd", ".", "movies/film.mkv"}, request: "video-sync/movies/film.mkv", modulePath: t.TempDir(), wantErr: true},
		{name: "reject missing module root", args: []string{"rsyncd", ".", "movies/film.mkv"}, request: "video-sync/movies/film.mkv", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("RSYNC_MODULE_PATH", tc.modulePath)
			t.Setenv("RSYNC_REQUEST", tc.request)
			for i, arg := range tc.args {
				t.Setenv("RSYNC_ARG"+strconv.Itoa(i), arg)
			}
			if err := checkRsyncStorage(context.Background()); (err != nil) != tc.wantErr {
				t.Fatalf("checkRsyncStorage() = %v, want error %v", err, tc.wantErr)
			}
		})
	}
}
