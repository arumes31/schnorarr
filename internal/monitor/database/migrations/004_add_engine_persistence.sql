-- Tables for Sync Engine persistence

CREATE TABLE IF NOT EXISTS engine_pending_actions (
    engine_id TEXT,
    path TEXT,
    PRIMARY KEY (engine_id, path)
);

CREATE TABLE IF NOT EXISTS engine_conflicts (
    engine_id TEXT,
    path TEXT,
    source_size INTEGER,
    source_time INTEGER,
    receiver_size INTEGER,
    receiver_time INTEGER,
    PRIMARY KEY (engine_id, path)
);

CREATE TABLE IF NOT EXISTS engine_queue (
    engine_id TEXT PRIMARY KEY,
    manifest_json TEXT,
    timestamp INTEGER
);

CREATE TABLE IF NOT EXISTS engine_state (
    engine_id TEXT PRIMARY KEY,
    waiting_for_approval BOOLEAN DEFAULT 0
);
