package store

// Package store handles all database interactions using SQLite.
// It manages the state of files (PENDING vs UPLOADED) and tracks file metadata (size, mod_time).
// This persistence layer ensures the daemon is resilient to restarts.

import (
	"database/sql"
	"strings"
	"time"

	_ "modernc.org/sqlite"
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
	// modernc.org/sqlite uses "sqlite" as driver name
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL")
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
func (s *Store) RegisterFile(path string, size int64, modTime time.Time, isMeta bool, expectSidecar bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var partnerID int64
	var partnerStatus FileStatus
	var partnerPath string
	var foundPartner bool

	if !isMeta {
		// I am an image (data). My partner MUST be my filename + .json
		// e.g. /path/to/img.png -> /path/to/img.png.json
		partnerPath = path + ".json"
	} else {
		// I am metadata (.json). My partner MUST be my filename without .json
		// e.g. /path/to/img.png.json -> /path/to/img.png
		partnerPath = strings.TrimSuffix(path, ".json")
	}

	// Check if my partner is already in the DB
	err = tx.QueryRow("SELECT id, status FROM files WHERE path = ?", partnerPath).Scan(&partnerID, &partnerStatus)
	if err == nil {
		foundPartner = true
	} else if err != sql.ErrNoRows {
		return err
	}

	if !foundPartner {
		// Partner not found -> I am waiting.
		// If I am an image: partner_path is known (calculated above).
		// If I am meta: partner_path is unknown (I can't guess the image extension), so we leave it NULL/empty.

		var pp sql.NullString
		if partnerPath != "" {
			pp.String = partnerPath
			pp.Valid = true
		}

		// Determine initial status based on configuration
		initialStatus := StatusAwaitingPartner
		if !isMeta && !expectSidecar {
			initialStatus = StatusPending
		}

		query := `
		INSERT INTO files (path, size, mod_time, status, partner_path)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			size = excluded.size,
			mod_time = excluded.mod_time,
			status = ?,
			partner_path = ?;
		`
		// Reset status to initialStatus even if it was previously something else (re-ingest)
		_, err = tx.Exec(query, path, size, modTime, initialStatus, pp, initialStatus, pp)
		if err != nil {
			return err
		}
	} else {
		// Partner found!
		// Logic:
		// 1. Update ME to PENDING. Set my partner_path to the found partner.
		// 2. Update PARTNER to PENDING. Ensure their partner_path is ME.

		// Insert/Update ME
		// Note: We always have partnerPath set here.
		// If !isMeta (Image), we calculated partnerPath.
		// If isMeta (Json), we retrieved partnerPath from the Image's record.
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
		// CRITICAL: We also update partner_path.
		// If Image arrived first: It set partner_path = Meta.json. (Correct)
		// If Meta arrived first: It set partner_path = NULL. We MUST update it to Image.path (ME).
		queryPartner := `UPDATE files SET status = ?, partner_path = ? WHERE id = ?`
		_, err = tx.Exec(queryPartner, StatusPending, path, partnerID)
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
	return s.RegisterFile(path, size, modTime, false, true)
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
