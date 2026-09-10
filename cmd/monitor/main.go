package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"schnorarr/internal/app"
	"schnorarr/internal/storage"
	"strconv"
)

const Port = "8080"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--check-storage" {
		if err := checkRsyncStorage(context.Background()); err != nil {
			log.Fatal(err)
		}
		return
	}
	// Initialize Application
	application, err := app.New()
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}

	// Start Application
	if err := application.Start(Port); err != nil {
		log.Fatalf("Application failed: %v", err)
	}
}

func checkRsyncStorage(ctx context.Context) error {
	receiver := storage.ReceiverFromEnv()
	root := os.Getenv("RSYNC_MODULE_PATH")
	if !filepath.IsAbs(root) {
		return fmt.Errorf("missing or invalid rsync module path")
	}
	receiver.Root = filepath.Clean(root)
	pathsStarted, checked := false, false
	// Rsync strips the module name and preserves each path in a separate argument.
	for i := 0; ; i++ {
		arg, ok := os.LookupEnv("RSYNC_ARG" + strconv.Itoa(i))
		if !ok {
			break
		}
		if !pathsStarted {
			pathsStarted = arg == "."
			continue
		}
		if err := receiver.CheckTarget(ctx, arg, false); err != nil {
			return err
		}
		checked = true
	}
	if !checked {
		return fmt.Errorf("missing rsync transfer path")
	}
	return nil
}
