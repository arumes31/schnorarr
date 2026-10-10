package database

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

// EngineStorageToken persists a stable identity for an engine across restarts.
func EngineStorageToken(id string) (string, error) {
	if DB == nil {
		return "", fmt.Errorf("database is not initialized")
	}
	return engineStorageToken(DB, id)
}

func engineStorageToken(db *sql.DB, id string) (string, error) {
	key := "storage_token_" + id
	var token string
	err := db.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&token)
	if err == nil {
		return token, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	token = hex.EncodeToString(random[:])
	if _, err := db.Exec("INSERT INTO settings (key,value) VALUES (?,?) ON CONFLICT(key) DO NOTHING", key, token); err != nil {
		return "", err
	}
	if err := db.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&token); err != nil {
		return "", err
	}
	return token, nil
}
