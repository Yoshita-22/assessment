
package database

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// initDB opens the SQLite database, configures it,
// creates the required table and indexes.
func InitDB() (*sql.DB, error) {

	// Open or create the SQLite database file.
	db, err := sql.Open("sqlite", "campaign.db")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// SQLite allows multiple readers but serializes writes.
	// Keep one connection for this small assignment.
	db.SetMaxOpenConns(1)

	// Verify that the database connection works.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Enable Write-Ahead Logging for better read/write behavior.
	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Wait briefly if the database is busy.
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	// Create the events table if it does not already exist.
	createTable := `
	CREATE TABLE IF NOT EXISTS events (
		event_id TEXT PRIMARY KEY,
		campaign_id TEXT NOT NULL,
		contact_id TEXT NOT NULL,
		type TEXT NOT NULL CHECK (
			type IN ('sent', 'delivered', 'opened', 'clicked')
		),
		timestamp TEXT NOT NULL,
		metadata TEXT,
		received_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`

	if _, err := db.Exec(createTable); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create events table: %w", err)
	}

	// Index campaign_id because campaign statistics
	// and event retrieval filter using this column.
	createCampaignIndex := `
	CREATE INDEX IF NOT EXISTS idx_events_campaign_id
	ON events(campaign_id);
	`

	if _, err := db.Exec(createCampaignIndex); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create campaign index: %w", err)
	}

	// Composite index for campaign-specific chronological queries.
	createTimestampIndex := `
	CREATE INDEX IF NOT EXISTS idx_events_campaign_timestamp
	ON events(campaign_id, timestamp);
	`

	if _, err := db.Exec(createTimestampIndex); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create timestamp index: %w", err)
	}

	fmt.Println("SQLite database initialized successfully")

	return db, nil
}
