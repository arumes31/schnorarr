package database

// SaveSetting saves or updates a setting in the database
func SaveSetting(key, value string) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", key, value)
	return err
}

// GetSetting retrieves a setting from the database
func GetSetting(key, defaultValue string) string {
	if DB == nil {
		return defaultValue
	}
	var value string
	err := DB.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&value)
	if err != nil {
		return defaultValue
	}
	return value
}
