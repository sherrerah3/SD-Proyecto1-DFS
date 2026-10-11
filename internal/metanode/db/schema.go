package db

import "database/sql"

// InitSchema creates the tables required by the MetaNode.
// The users and sessions tables are assumed to be created by the
// existing authentication database initialization.
func InitSchema(database *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS entries (
			entry_id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			parent_id INTEGER,
			name TEXT NOT NULL,
			kind TEXT NOT NULL CHECK (kind IN ('directory', 'file')),
			created_at INTEGER NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(user_id),
			FOREIGN KEY (parent_id) REFERENCES entries(entry_id)
		)`,

		`CREATE UNIQUE INDEX IF NOT EXISTS idx_entries_root
			ON entries(user_id)
			WHERE parent_id IS NULL`,

		`CREATE UNIQUE INDEX IF NOT EXISTS idx_entries_children
			ON entries(user_id, parent_id, name)
			WHERE parent_id IS NOT NULL`,

		`CREATE TABLE IF NOT EXISTS uploads (
			upload_id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			entry_id INTEGER,
			status TEXT NOT NULL
				CHECK (status IN ('pending', 'committed', 'failed')),
			created_at INTEGER NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(user_id),
			FOREIGN KEY (entry_id) REFERENCES entries(entry_id)
		)`,

		`CREATE TABLE IF NOT EXISTS blocks (
			block_id TEXT PRIMARY KEY,
			entry_id INTEGER NOT NULL,
			upload_id TEXT,
			block_index INTEGER NOT NULL,
			version INTEGER NOT NULL DEFAULT 1,
			size INTEGER NOT NULL DEFAULT 0,
			checksum TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			FOREIGN KEY (entry_id) REFERENCES entries(entry_id),
			FOREIGN KEY (upload_id) REFERENCES uploads(upload_id),
			UNIQUE(entry_id, version, block_index)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_blocks_entry
			ON blocks(entry_id)`,

		`CREATE TABLE IF NOT EXISTS datanodes (
			datanode_id TEXT PRIMARY KEY,
			address TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			last_seen INTEGER NOT NULL DEFAULT 0
		)`,

		`CREATE TABLE IF NOT EXISTS block_replicas (
			replica_id INTEGER PRIMARY KEY AUTOINCREMENT,
			block_id TEXT NOT NULL,
			datanode_id TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at INTEGER NOT NULL,
			FOREIGN KEY (block_id) REFERENCES blocks(block_id),
			FOREIGN KEY (datanode_id) REFERENCES datanodes(datanode_id),
			UNIQUE(block_id, datanode_id)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_block_replicas_block
			ON block_replicas(block_id)`,

		`CREATE INDEX IF NOT EXISTS idx_entries_parent
			ON entries(user_id, parent_id)`,
	}

	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			return err
		}
	}

	return nil
}
