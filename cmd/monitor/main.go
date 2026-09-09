package main

import (
	"context"
	"log"
	"os"
	"schnorarr/internal/app"
	"schnorarr/internal/storage"
	"strings"
)

const Port = "8080"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--check-storage" {
		receiver := storage.ReceiverFromEnv()
		module, request := os.Getenv("RSYNC_MODULE_NAME"), os.Getenv("RSYNC_REQUEST")
		if module == "" || !(request == module || strings.HasPrefix(request, module+"/")) {
			log.Fatal("missing rsync transfer path")
		}
		path := strings.TrimPrefix(strings.TrimPrefix(request, module), "/")
		if err := receiver.CheckTarget(context.Background(), path, false); err != nil {
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
