package database

import (
	"database/sql"
	"encoding/json"
	"time"
)

type PersistentState struct {
	PendingDeletions   []string
	WaitingForApproval bool
	Conflicts          []ConflictPersistence
}

type ConflictPersistence struct {
	Path         string
	SourceSize   int64
	SourceTime   int64
	ReceiverSize int64
	ReceiverTime int64
}

type QueuedSync struct {
	ManifestJSON string
	Timestamp    int64
}

func SaveEngineState(engineID string, waiting bool, deletions []string, conflicts []ConflictPersistence) error {
	if DB == nil {
		return nil
	}
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 1. Save general state
	_, err = tx.Exec(`INSERT OR REPLACE INTO engine_state (engine_id, waiting_for_approval) VALUES (?, ?)`, engineID, waiting)
	if err != nil {
		return err
	}

	// 2. Save pending deletions
	_, err = tx.Exec(`DELETE FROM engine_pending_actions WHERE engine_id = ?`, engineID)
	if err != nil {
		return err
	}
	for _, p := range deletions {
		_, err = tx.Exec(`INSERT INTO engine_pending_actions (engine_id, path) VALUES (?, ?)`, engineID, p)
		if err != nil {
			return err
		}
	}

	// 3. Save conflicts
	_, err = tx.Exec(`DELETE FROM engine_conflicts WHERE engine_id = ?`, engineID)
	if err != nil {
		return err
	}
	for _, c := range conflicts {
		_, err = tx.Exec(`INSERT INTO engine_conflicts (engine_id, path, source_size, source_time, receiver_size, receiver_time) 
			VALUES (?, ?, ?, ?, ?, ?)`, engineID, c.Path, c.SourceSize, c.SourceTime, c.ReceiverSize, c.ReceiverTime)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func LoadEngineState(engineID string) (*PersistentState, error) {
	state := &PersistentState{
		PendingDeletions: []string{},
		Conflicts:        []ConflictPersistence{},
	}
	if DB == nil {
		return state, nil
	}

	// 1. Load general state
	err := DB.QueryRow(`SELECT waiting_for_approval FROM engine_state WHERE engine_id = ?`, engineID).Scan(&state.WaitingForApproval)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// 2. Load pending deletions
	rows, err := DB.Query(`SELECT path FROM engine_pending_actions WHERE engine_id = ?`, engineID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		state.PendingDeletions = append(state.PendingDeletions, p)
	}

	// 3. Load conflicts
	crows, err := DB.Query(`SELECT path, source_size, source_time, receiver_size, receiver_time FROM engine_conflicts WHERE engine_id = ?`, engineID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = crows.Close() }()
	for crows.Next() {
		var c ConflictPersistence
		if err := crows.Scan(&c.Path, &c.SourceSize, &c.SourceTime, &c.ReceiverSize, &c.ReceiverTime); err != nil {
			return nil, err
		}
		state.Conflicts = append(state.Conflicts, c)
	}

	return state, nil
}

func SaveEngineQueue(engineID string, manifest interface{}) error {
	if DB == nil {
		return nil
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	_, err = DB.Exec(`INSERT OR REPLACE INTO engine_queue (engine_id, manifest_json, timestamp) VALUES (?, ?, ?)`,
		engineID, string(data), time.Now().Unix())
	return err
}

func ClearEngineQueue(engineID string) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(`DELETE FROM engine_queue WHERE engine_id = ?`, engineID)
	return err
}

func LoadEngineQueue(engineID string) (string, error) {
	if DB == nil {
		return "", nil
	}
	var jsonStr string
	err := DB.QueryRow(`SELECT manifest_json FROM engine_queue WHERE engine_id = ?`, engineID).Scan(&jsonStr)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return jsonStr, err
}
