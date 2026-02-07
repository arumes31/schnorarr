-- Table to track sync engine health/reliability
CREATE TABLE IF NOT EXISTS engine_stats (
    engine_id TEXT PRIMARY KEY,
    success_count INTEGER DEFAULT 0,
    error_count INTEGER DEFAULT 0,
    last_error_msg TEXT
);
