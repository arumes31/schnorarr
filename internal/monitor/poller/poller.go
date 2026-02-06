package poller

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const RsyncLog = "/tmp/rsync.log"

// Poller periodically touches directories to trigger lsyncd
type Poller struct {
	interval time.Duration
	dataDir  string
}

// New creates a new directory poller
func New(interval time.Duration, dataDir string) *Poller {
	if dataDir == "" {
		dataDir = "/data"
	}
	return &Poller{
		interval: interval,
		dataDir:  dataDir,
	}
}

// Start begins the polling loop
func (p *Poller) Start() {
	log.Printf("Starting Poller (Interval: %v)...", p.interval)
	ticker := time.NewTicker(p.interval)

	for range ticker.C {
		err := filepath.Walk(p.dataDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				// Determine depth. /data is depth 0.
				rel, _ := filepath.Rel(p.dataDir, path)
				depth := len(strings.Split(rel, string(os.PathSeparator)))
				if rel == "." {
					depth = 0
				}

				if depth <= 2 {
					now := time.Now()
					e := os.Chtimes(path, now, now)
					if e != nil {
						log.Printf("Touch Error %s: %v", path, e)
					}
				} else {
					return filepath.SkipDir
				}
			}
			return nil
		})

		if err != nil {
			log.Printf("[ERROR] Polling failed: %v", err)
			// Log to rsync log so tailer picks it up
			p.logError(err)
		}
	}
}

// logError writes polling errors to rsync log
func (p *Poller) logError(err error) {
	f, e := os.OpenFile(RsyncLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if e == nil {
		defer f.Close()
		if _, err := f.WriteString(fmt.Sprintf("%s [ERROR] Polling failed: %v\n",
			time.Now().Format("2006/01/02 15:04:05"), err)); err != nil {
			log.Printf("Failed to write to rsync log: %v", err)
		}
	}
}
