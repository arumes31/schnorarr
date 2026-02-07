-- Migration to add engine_id to traffic table if it doesn't exist
-- SQLite doesn't support easy ALTER TABLE for primary keys, so we recreate if needed.

-- This script is designed to be idempotent when run through the Go migration logic.
-- We check for the column existence in Go, then run this if needed.

PRAGMA foreign_keys=OFF;

CREATE TABLE traffic_new (
    date TEXT,
    engine_id TEXT,
    bytes_sent INTEGER DEFAULT 0,
    PRIMARY KEY (date, engine_id)
);

-- Copy data from old table if it exists
INSERT INTO traffic_new (date, engine_id, bytes_sent)
SELECT date, 'legacy', bytes_sent FROM traffic;

DROP TABLE traffic;
ALTER TABLE traffic_new RENAME TO traffic;

PRAGMA foreign_keys=ON;
