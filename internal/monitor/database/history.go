package database

import (
	"log"
	"strconv"
)

// HistoryItem represents a single sync event
type HistoryItem struct {
	Time   string
	Action string
	Path   string
	Size   string
}

// LogEvent saves a sync event to the database
func LogEvent(timestamp, action, path string, size int64, engineID string) error {
	_, err := DB.Exec("INSERT INTO history (timestamp, action, file_path, size_bytes, engine_id) VALUES (?, ?, ?, ?, ?)",
		timestamp, action, path, size, engineID)
	return err
}

// GetHistory retrieves recent sync history with pagination
func GetHistory(limit, offset int, query string) ([]HistoryItem, error) {
	q := "SELECT timestamp, action, file_path, size_bytes FROM history"
	args := []interface{}{}

	if query != "" {
		q += " WHERE file_path LIKE ?"
		args = append(args, "%"+query+"%")
	}

	q += " ORDER BY id DESC"

	if limit > 0 {
		q += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}

	rows, err := DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []HistoryItem
	for rows.Next() {
		var i HistoryItem
		var sizeBytes int64
		if err := rows.Scan(&i.Time, &i.Action, &i.Path, &sizeBytes); err != nil {
			log.Printf("History Scan Error: %v", err)
			continue
		}
		i.Size = FormatBytes(sizeBytes)
		items = append(items, i)
	}
	return items, nil
}

// GetHistoryCount returns the total number of history items matching the query
func GetHistoryCount(query string) (int, error) {
	q := "SELECT COUNT(*) FROM history"
	args := []interface{}{}

	if query != "" {
		q += " WHERE file_path LIKE ?"
		args = append(args, "%"+query+"%")
	}

	var count int
	err := DB.QueryRow(q, args...).Scan(&count)
	return count, err
}

// GetTopFiles returns the largest files synced in the last 24 hours
func GetTopFiles() []HistoryItem {
	q := "SELECT timestamp, action, file_path, size_bytes FROM history WHERE action='Added' AND timestamp > datetime('now', '-1 day') ORDER BY size_bytes DESC LIMIT 5"
	rows, err := DB.Query(q)
	if err != nil { return nil }
	defer rows.Close()

	var items []HistoryItem
	for rows.Next() {
		var i HistoryItem; var sz int64
		_ = rows.Scan(&i.Time, &i.Action, &i.Path, &sz)
		i.Size = FormatBytes(sz)
		items = append(items, i)
	}
	return items
}

// PruneHistory deletes history items older than the specified retention period
func PruneHistory(days int) error {
	cutoff := "date('now', '-" + FormatInt(days) + " days')"
	_, err := DB.Exec("DELETE FROM history WHERE timestamp < " + cutoff)
	return err
}

func FormatInt(n int) string {
	return strconv.Itoa(n)
}