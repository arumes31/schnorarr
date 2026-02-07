package database

import (
	"time"
)

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

func GetTrafficStats() TrafficStats {
	var s TrafficStats
	if DB == nil { return s }
	_ = DB.QueryRow("SELECT COALESCE(SUM(bytes_sent), 0) FROM traffic").Scan(&s.Total)
	
	// Fix: Use LIKE to ensure we match the date prefix correctly
	todayPrefix := time.Now().Format("2006/01/02") + "%"
	_ = DB.QueryRow("SELECT COALESCE(SUM(bytes_sent), 0) FROM traffic WHERE date LIKE ?", todayPrefix).Scan(&s.Today)
	return s
}

func GetYesterdayTraffic() int64 {
	if DB == nil { return 0 }
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006/01/02") + "%"
	var size int64
	_ = DB.QueryRow("SELECT COALESCE(SUM(bytes_sent), 0) FROM traffic WHERE date LIKE ?", yesterday).Scan(&size)
	return size
}

func GetEngineTrafficStats(engineID string) TrafficStats {
	var s TrafficStats
	if DB == nil { return s }
	_ = DB.QueryRow("SELECT COALESCE(SUM(bytes_sent), 0) FROM traffic WHERE engine_id=?", engineID).Scan(&s.Total)
	
	todayPrefix := time.Now().Format("2006/01/02") + "%"
	_ = DB.QueryRow("SELECT COALESCE(SUM(bytes_sent), 0) FROM traffic WHERE engine_id=? AND date LIKE ?", engineID, todayPrefix).Scan(&s.Today)
	return s
}

func GetDailyTraffic(days int) []DailyTraffic {
	if DB == nil { return nil }
	query := `SELECT date, SUM(bytes_sent) FROM traffic GROUP BY date ORDER BY date DESC LIMIT ?`
	rows, err := DB.Query(query, days)
	if err != nil { return nil }
	defer rows.Close()
	var results []DailyTraffic; var maxBytes int64 = 0
	for rows.Next() {
		var d DailyTraffic; if err := rows.Scan(&d.Date, &d.Bytes); err != nil { continue }
		d.Size = FormatBytes(d.Bytes); if d.Bytes > maxBytes { maxBytes = d.Bytes }
		results = append(results, d)
	}
	for i := range results {
		if maxBytes > 0 {
			results[i].HeightPercent = int((float64(results[i].Bytes) / float64(maxBytes)) * 100)
			if results[i].HeightPercent < 5 { results[i].HeightPercent = 5 }
		}
	}
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 { results[i], results[j] = results[j], results[i] }
	return results
}

func ReportEngineSuccess(id string) {
	if DB == nil { return }
	_, _ = DB.Exec("INSERT INTO engine_stats (engine_id, success_count) VALUES (?, 1) ON CONFLICT(engine_id) DO UPDATE SET success_count = success_count + 1", id)
}

func ReportEngineError(id string, msg string) {
	if DB == nil { return }
	_, _ = DB.Exec("INSERT INTO engine_stats (engine_id, error_count, last_error_msg) VALUES (?, 1, ?) ON CONFLICT(engine_id) DO UPDATE SET error_count = error_count + 1, last_error_msg = ?", id, msg, msg)
}

func GetEngineHealth(id string) (grade string, color string) {
	if DB == nil { return "N/A", "#94a3b8" }
	var succ, err int
	_ = DB.QueryRow("SELECT success_count, error_count FROM engine_stats WHERE engine_id=?", id).Scan(&succ, &err)
	total := succ + err
	if total == 0 { return "N/A", "#94a3b8" }
	ratio := float64(succ) / float64(total)
	if ratio >= 0.95 { return "A", "#00ffad" }
	if ratio >= 0.85 { return "B", "#0079d3" }
	if ratio >= 0.70 { return "C", "#ffb300" }
	if ratio >= 0.50 { return "D", "#f97316" }
	return "F", "#ff3d00"
}
