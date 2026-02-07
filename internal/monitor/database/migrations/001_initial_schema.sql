-- Initial schema for Mirrqr Sync Node

CREATE TABLE IF NOT EXISTS history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT,
    action TEXT,
    file_path TEXT,
    size_bytes INTEGER DEFAULT 0,
    engine_id TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT
);

CREATE TABLE IF NOT EXISTS traffic (
    date TEXT,
    engine_id TEXT,
    bytes_sent INTEGER DEFAULT 0,
    PRIMARY KEY (date, engine_id)
);
