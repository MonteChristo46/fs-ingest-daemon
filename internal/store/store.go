package store

// Package store handles all database interactions using SQLite.
// It manages the state of files (PENDING vs UPLOADED) and tracks file metadata (size, mod_time).
// This persistence layer ensures the daemon is resilient to restarts.

import (
	"database/sql"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// FileStatus represents the processing state of a file.
type FileStatus string

const (
	StatusPending         FileStatus = "PENDING"          // File is ready for upload (paired or orphan)
	StatusUploaded        FileStatus = "UPLOADED"         // File confirmed uploaded
	StatusAwaitingPartner FileStatus = "AWAITING_PARTNER" // File detected, waiting for sidecar/data
	StatusOrphan          FileStatus = "ORPHAN"           // Partner did not arrive in time
)

// FileRecord represents a row in the 'files' table.
type FileRecord struct {
	ID          int64
	Path        string
	Size        int64
	ModTime     time.Time
	Status      FileStatus
	UploadedAt  sql.NullTime
	PartnerPath sql.NullString
}

// Store wraps the SQL database connection.
type Store struct {
	db *sql.DB
}

// NewStore initializes the SQLite database connection and runs migrations.
func NewStore(dbPath string) (*Store, error) {
	// WAL mode is much better for concurrent access
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}

	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate creates the necessary tables and indexes if they don't exist.
func (s *Store) migrate() error {
	query := `
	CREATE TABLE IF NOT EXISTS files (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		path TEXT NOT NULL UNIQUE,
		size INTEGER NOT NULL,
		mod_time DATETIME NOT NULL,
		status TEXT NOT NULL,
		uploaded_at DATETIME,
		partner_path TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_status_mod_time ON files(status, mod_time);
	`
	_, err := s.db.Exec(query)
	// Check if partner_path column exists (migration for existing db)
	if err == nil {
		_, err = s.db.Exec("ALTER TABLE files ADD COLUMN partner_path TEXT;")
		if err != nil {
			// Ignore error if column likely already exists
			// In a real app we'd check PRAGMA table_info
			return nil
		}
	}
	return err
}

// RegisterFile handles the detection of a new file and attempts to pair it.
func (s *Store) RegisterFile(path string, size int64, modTime time.Time, isMeta bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Determine partner path
	var partnerPath string
	if isMeta {
		// If I am .json, my partner is the base file (remove .json suffix)
		// Assuming strict naming convention: file.png.json -> file.png
		partnerPath = path[:len(path)-5]
	} else {
		// If I am data, my partner is .json
		partnerPath = path + ".json"
	}

	// Check if partner exists in DB
	// We only care if the partner is currently tracked (Wait or Pending)
	// If partner is already UPLOADED, we might process this as an orphan or re-upload?
	// For now, let's look for any record of the partner.
	var partnerID int64
	var partnerStatus FileStatus
	err = tx.QueryRow("SELECT id, status FROM files WHERE path = ?", partnerPath).Scan(&partnerID, &partnerStatus)

	if err == sql.ErrNoRows {
		// Partner not found -> I am waiting.
		query := `
		INSERT INTO files (path, size, mod_time, status, partner_path)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			size = excluded.size,
			mod_time = excluded.mod_time,
			status = ?,
			partner_path = ?;
		`
		// Reset status to AWAITING_PARTNER even if it was previously something else (re-ingest)
		_, err = tx.Exec(query, path, size, modTime, StatusAwaitingPartner, partnerPath, StatusAwaitingPartner, partnerPath)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		// Partner found!
		// Logic:
		// 1. Update ME to PENDING.
		// 2. Update PARTNER to PENDING (if it was waiting).

		// Insert/Update ME
		queryMe := `
		INSERT INTO files (path, size, mod_time, status, partner_path)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			size = excluded.size,
			mod_time = excluded.mod_time,
			status = ?,
			partner_path = ?;
		`
		_, err = tx.Exec(queryMe, path, size, modTime, StatusPending, partnerPath, StatusPending, partnerPath)
		if err != nil {
			return err
		}

		// Update PARTNER
		// We force it to PENDING so the ingester picks it up.
		queryPartner := `UPDATE files SET status = ? WHERE id = ?`
		_, err = tx.Exec(queryPartner, StatusPending, partnerID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// MarkOrphans checks for files that have been waiting too long and marks them as orphans.
func (s *Store) MarkOrphans(timeout time.Duration) error {
	deadline := time.Now().Add(-timeout)
	query := `
	UPDATE files
	SET status = ?
	WHERE status = ? AND mod_time < ?
	`
	_, err := s.db.Exec(query, StatusOrphan, StatusAwaitingPartner, deadline)
	return err
}

// AddOrUpdateFile inserts a new file or updates an existing one.
// Deprecated: Use RegisterFile for pairing logic.
func (s *Store) AddOrUpdateFile(path string, size int64, modTime time.Time) error {
	return s.RegisterFile(path, size, modTime, false)
}

// MarkUploaded updates the status of a file to UPLOADED and sets the uploaded_at timestamp.
func (s *Store) MarkUploaded(path string) error {
	query := `
	UPDATE files 
	SET status = ?, uploaded_at = ?
	WHERE path = ?;
	`
	_, err := s.db.Exec(query, StatusUploaded, time.Now(), path)
	return err
}

// GetTotalSize returns the sum of the size of all tracked files.
func (s *Store) GetTotalSize() (int64, error) {
	query := `SELECT COALESCE(SUM(size), 0) FROM files`
	var size int64
	err := s.db.QueryRow(query).Scan(&size)
	return size, err
}

// GetPruneCandidates returns a list of files that are safe to delete (Status=UPLOADED).
// Files are returned in order of Modification Time (oldest first).
func (s *Store) GetPruneCandidates(limit int) ([]FileRecord, error) {
	query := `
	SELECT id, path, size, mod_time, status, uploaded_at, partner_path
	FROM files
	WHERE status = ?
	ORDER BY mod_time ASC
	LIMIT ?
	`
	rows, err := s.db.Query(query, StatusUploaded, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []FileRecord
	for rows.Next() {
		var f FileRecord
		err := rows.Scan(&f.ID, &f.Path, &f.Size, &f.ModTime, &f.Status, &f.UploadedAt, &f.PartnerPath)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, f)
	}
	return candidates, nil
}

// RemoveFile deletes a file record from the database.
func (s *Store) RemoveFile(path string) error {
	query := `DELETE FROM files WHERE path = ?`
	_, err := s.db.Exec(query, path)
	return err
}

// GetPendingFiles returns a list of files waiting to be uploaded.
// This now includes both PENDING (paired) and ORPHAN files.
func (s *Store) GetPendingFiles(limit int) ([]FileRecord, error) {
	query := `
	SELECT id, path, size, mod_time, status, uploaded_at, partner_path
	FROM files
	WHERE status IN (?, ?)
	ORDER BY mod_time ASC
	LIMIT ?
	`
	rows, err := s.db.Query(query, StatusPending, StatusOrphan, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		err := rows.Scan(&f.ID, &f.Path, &f.Size, &f.ModTime, &f.Status, &f.UploadedAt, &f.PartnerPath)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}
