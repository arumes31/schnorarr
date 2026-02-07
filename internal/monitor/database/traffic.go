package database

import (
	"log"
	"sync"
	"time"
)

var (
	// engine_id -> bytes
	unflushedBytes = make(map[string]int64)
	trafficMu      sync.Mutex
)

// AddTraffic records an increment of bytes sent today in memory for a specific engine
func AddTraffic(engineID string, bytes int64) error {
	if bytes <= 0 {
		return nil
	}
	
	trafficMu.Lock()
	unflushedBytes[engineID] += bytes
	trafficMu.Unlock()
	return nil
}

// StartTrafficManager begins the background flush loop
func StartTrafficManager() {
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		for range ticker.C {
			if err := FlushTraffic(); err != nil {
				log.Printf("[Database] Traffic flush failed: %v", err)
			}
		}
	}()
}

// FlushTraffic writes buffered traffic data to the database
func FlushTraffic() error {
	trafficMu.Lock()
	if len(unflushedBytes) == 0 {
		trafficMu.Unlock()
		return nil
	}
	
	// Copy and clear the buffer
	toFlush := make(map[string]int64)
	for id, b := range unflushedBytes {
		toFlush[id] = b
		delete(unflushedBytes, id)
	}
	trafficMu.Unlock()

	today := time.Now().Format("2006/01/02")
	
	tx, err := DB.Begin()
	if err != nil {
		return err
	}

	for id, bytes := range toFlush {
		_, err := tx.Exec(`INSERT INTO traffic (date, engine_id, bytes_sent) 
			VALUES (?, ?, ?) 
			ON CONFLICT(date, engine_id) DO UPDATE SET bytes_sent = bytes_sent + ?`, 
			today, id, bytes, bytes)
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("[Database] Rollback failed: %v", rbErr)
			}
			// Put bytes back on failure
			trafficMu.Lock()
			unflushedBytes[id] += bytes
			trafficMu.Unlock()
			return err
		}
	}
	
	if err := tx.Commit(); err != nil {
		return err
	}

	// Vacuum occasionally or on demand? For now just log and continue
	return nil
}
