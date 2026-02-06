package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

const DBPath = "/config/history.db"

// DB is the singleton database instance
var DB *sql.DB

// HistoryItem represents a single sync event
type HistoryItem struct {
	Time   string
	Action string
	Path   string
	Size   string // Formatted size
}

// TrafficStats holds traffic statistics
type TrafficStats struct {
	Today int64
	Total int64
}

// DailyTraffic represents traffic for a single day
type DailyTraffic struct {
	Date          string
	Bytes         int64
	Size          string
	HeightPercent int
}

// Init initializes the database connection and creates tables
func Init() error {
	var err error

	// Retry logic for database initialization (handles concurrent startup)
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			// Exponential backoff: 100ms, 200ms, 400ms, 800ms, 1600ms
			backoff := time.Duration(100*(1<<uint(i-1))) * time.Millisecond
			log.Printf("Database init retry %d/%d after %v", i+1, maxRetries, backoff)
			time.Sleep(backoff)
		}

		DB, err = sql.Open("sqlite", DBPath)
		if err != nil {
			if i == maxRetries-1 {
				return fmt.Errorf("failed to open DB after %d retries: %w", maxRetries, err)
			}
			continue
		}

		// Set connection pool limits to reduce contention
		DB.SetMaxOpenConns(1) // SQLite works best with single writer
		DB.SetMaxIdleConns(1)
		DB.SetConnMaxLifetime(time.Hour)

		// Enable WAL mode for better concurrent access
		if _, err = DB.Exec("PRAGMA journal_mode=WAL"); err != nil {
			DB.Close()
			if i == maxRetries-1 {
				return fmt.Errorf("failed to enable WAL mode after %d retries: %w", maxRetries, err)
			}
			continue
		}

		// Set busy timeout to handle lock contention
		if _, err = DB.Exec("PRAGMA busy_timeout=5000"); err != nil {
			DB.Close()
			if i == maxRetries-1 {
				return fmt.Errorf("failed to set busy timeout after %d retries: %w", maxRetries, err)
			}
			continue
		}

		createTable := `CREATE TABLE IF NOT EXISTS history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT,
		action TEXT,
		file_path TEXT,
		size_bytes INTEGER DEFAULT 0
	);`

		if _, err = DB.Exec(createTable); err != nil {
			DB.Close()
			if i == maxRetries-1 {
				return fmt.Errorf("failed to create table after %d retries: %w", maxRetries, err)
			}
			continue
		}

		// Migration for existing DBs (ignore errors)
		_, _ = DB.Exec("ALTER TABLE history ADD COLUMN size_bytes INTEGER DEFAULT 0")

		// Success!
		log.Println("Database initialized successfully")
		return nil
	}

	return fmt.Errorf("failed to initialize database after %d retries: %w", maxRetries, err)
}

// LogEvent logs a sync event to the database
func LogEvent(timestamp, action, filePath string, size int64) error {
	// Simple deduplication check
	var exists int
	err := DB.QueryRow("SELECT id FROM history WHERE timestamp=? AND action=? AND file_path=?",
		timestamp, action, filePath).Scan(&exists)
	if err == nil {
		return nil // Already exists
	}

	_, err = DB.Exec("INSERT INTO history (timestamp, action, file_path, size_bytes) VALUES (?, ?, ?, ?)",
		timestamp, action, filePath, size)
	if err != nil {
		return fmt.Errorf("DB insert error: %w", err)
	}

	return nil
}

// GetHistory retrieves history items with optional filtering
func GetHistory(limit int, queryStr string) ([]HistoryItem, error) {
	query := "SELECT timestamp, action, file_path, size_bytes FROM history"
	var args []interface{}

	if queryStr != "" {
		query += " WHERE file_path LIKE ?"
		args = append(args, "%"+queryStr+"%")
	}

	query += " ORDER BY id DESC"

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}
	defer rows.Close()

	var items []HistoryItem
	for rows.Next() {
		var item HistoryItem
		var sizeBytes int64
		if err := rows.Scan(&item.Time, &item.Action, &item.Path, &sizeBytes); err != nil {
			log.Printf("Row Scan Error: %v", err)
			continue
		}
		item.Size = FormatBytes(sizeBytes)
		items = append(items, item)
	}
	return items, nil
}

// GetTrafficStats returns overall traffic statistics
func GetTrafficStats() TrafficStats {
	var s TrafficStats

	// Total
	if err := DB.QueryRow("SELECT COALESCE(SUM(size_bytes), 0) FROM history WHERE action='Added'").Scan(&s.Total); err != nil {
		log.Printf("DB Total Stats Error: %v", err)
	}

	// Today (Assuming date format YYYY/MM/DD)
	todayPrefix := time.Now().Format("2006/01/02")
	if err := DB.QueryRow("SELECT COALESCE(SUM(size_bytes), 0) FROM history WHERE action='Added' AND timestamp LIKE ?",
		todayPrefix+"%").Scan(&s.Today); err != nil {
		log.Printf("DB Today Stats Error: %v", err)
	}

	return s
}

// GetDailyTraffic returns daily traffic for the last N days
func GetDailyTraffic(days int) []DailyTraffic {
	query := `SELECT substr(timestamp, 1, 10) as date, SUM(size_bytes) as total 
	          FROM history 
	          WHERE action='Added' 
	          GROUP BY date 
	          ORDER BY date DESC 
	          LIMIT ?`

	rows, err := DB.Query(query, days)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []DailyTraffic
	var maxBytes int64 = 0

	for rows.Next() {
		var d DailyTraffic
		if err := rows.Scan(&d.Date, &d.Bytes); err != nil {
			log.Printf("Daily Traffic Scan Error: %v", err)
			continue
		}
		d.Size = FormatBytes(d.Bytes)
		if d.Bytes > maxBytes {
			maxBytes = d.Bytes
		}
		results = append(results, d)
	}

	// Calculate percentages for chart
	for i := range results {
		if maxBytes > 0 {
			results[i].HeightPercent = int((float64(results[i].Bytes) / float64(maxBytes)) * 100)
			if results[i].HeightPercent < 5 {
				results[i].HeightPercent = 5 // Min height
			}
		} else {
			results[i].HeightPercent = 0
		}
	}

	// Reverse to show oldest to newest (left to right)
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}

	return results
}

// FormatBytes converts bytes to human-readable format
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
