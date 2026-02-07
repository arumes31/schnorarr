package main

import (
	"log"
	"schnorarr/internal/app"
)

const Port = "8080"

func main() {
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