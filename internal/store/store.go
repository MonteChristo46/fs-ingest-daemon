package store

// Package store handles all database interactions using SQLite.
// It manages the state of files (PENDING vs UPLOADED) and tracks file metadata (size, mod_time).
// This persistence layer ensures the daemon is resilient to restarts.

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// FileStatus represents the processing state of a file.
type FileStatus string

const (
	StatusPending         FileStatus = "PENDING"          // File is ready for upload (paired or orphan)
	StatusProcessing      FileStatus = "PROCESSING"       // File atomically claimed by a worker
	StatusUploaded        FileStatus = "UPLOADED"         // File confirmed uploaded
	StatusAwaitingPartner FileStatus = "AWAITING_PARTNER" // File detected, waiting for sidecar/data
	StatusOrphan          FileStatus = "ORPHAN"           // Partner did not arrive in time
	StatusFailed          FileStatus = "FAILED"           // Upload permanently failed after max retries
)

// DefaultMaxRetriesDefault is the fallback if no value is provided.
const DefaultMaxRetriesDefault = 3

// FileRecord represents a row in the 'files' table.
type FileRecord struct {
	ID               int64
	Path             string
	FileHash         string
	OriginalFilename sql.NullString
	Size             int64
	ModTime          time.Time
	Status           FileStatus
	RetryCount       int
	MaxRetries       int
	LastError        sql.NullString
	UploadedAt       sql.NullTime
	PartnerPath      sql.NullString
}

// Store wraps the SQL database connection.
type Store struct {
	db                *sql.DB
	quotaExceeded     atomic.Bool
	defaultMaxRetries int
}

// SetQuotaExceeded updates the quota exceeded state.
func (s *Store) SetQuotaExceeded(exceeded bool) {
	s.quotaExceeded.Store(exceeded)
}

// IsQuotaExceeded returns the current quota exceeded state.
func (s *Store) IsQuotaExceeded() bool {
	return s.quotaExceeded.Load()
}

// NewStore initializes the SQLite database connection and runs migrations.
func NewStore(dbPath string, defaultMaxRetries int) (*Store, error) {
	// modernc.org/sqlite uses "sqlite" as driver name.
	// We use a single connection to avoid "database is locked" errors with writers.
	// SQLite handles serialization internally, but database/sql connection pool
	// can sometimes be too aggressive.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Set connection limits
	db.SetMaxOpenConns(1)

	// Enable WAL mode and busy timeout for better concurrency handling
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA auto_vacuum=INCREMENTAL;"); err != nil {
		db.Close()
		return nil, err
	}

	if defaultMaxRetries <= 0 {
		defaultMaxRetries = DefaultMaxRetriesDefault
	}
	s := &Store{db: db, defaultMaxRetries: defaultMaxRetries}
	if err := s.migrate(); err != nil {
		db.Close()
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
	// Initial table creation (old schema — will be migrated below if needed)
	query := `
	CREATE TABLE IF NOT EXISTS files (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		path TEXT NOT NULL UNIQUE,
		size INTEGER NOT NULL,
		mod_time DATETIME NOT NULL,
		status TEXT NOT NULL DEFAULT 'PENDING'
			CHECK(status IN ('PENDING','PROCESSING','UPLOADED','AWAITING_PARTNER','ORPHAN','FAILED')),
		retry_count INTEGER NOT NULL DEFAULT 0,
		max_retries INTEGER NOT NULL DEFAULT 3,
		last_error TEXT,
		uploaded_at DATETIME,
		partner_path TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_status_mod_time ON files(status, mod_time);
	CREATE INDEX IF NOT EXISTS idx_partner_path ON files(partner_path);
	`
	_, err := s.db.Exec(query)
	if err != nil {
		return err
	}
	// Migration: add columns for databases created before v0.10.0
	_, _ = s.db.Exec("ALTER TABLE files ADD COLUMN retry_count INTEGER NOT NULL DEFAULT 0;")
	_, _ = s.db.Exec("ALTER TABLE files ADD COLUMN max_retries INTEGER NOT NULL DEFAULT 3;")
	_, _ = s.db.Exec("ALTER TABLE files ADD COLUMN last_error TEXT;")
	_, _ = s.db.Exec("DROP INDEX IF EXISTS idx_status_mod_time;")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_status_mod_time ON files(status, mod_time);")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_partner_path ON files(partner_path);")

	// Migration v0.11.0: add file_hash and original_filename columns,
	// remove UNIQUE constraint from path, add UNIQUE on file_hash.
	_, _ = s.db.Exec("ALTER TABLE files ADD COLUMN file_hash TEXT;")
	_, _ = s.db.Exec("ALTER TABLE files ADD COLUMN original_filename TEXT;")

	// Check if the old UNIQUE constraint on path still exists (auto-index).
	var hasPathUnique int
	_ = s.db.QueryRow(
		"SELECT COUNT(*) FROM pragma_index_list('files') WHERE name = 'sqlite_autoindex_files_1'",
	).Scan(&hasPathUnique)

	if hasPathUnique > 0 {
		// Recreate the table without UNIQUE on path and with UNIQUE on file_hash.
		_, _ = s.db.Exec("DROP TABLE IF EXISTS files_new")
		_, err = s.db.Exec(`
			CREATE TABLE files_new (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				path TEXT NOT NULL,
				file_hash TEXT NOT NULL UNIQUE,
				original_filename TEXT,
				size INTEGER NOT NULL,
				mod_time DATETIME NOT NULL,
				status TEXT NOT NULL DEFAULT 'PENDING'
					CHECK(status IN ('PENDING','PROCESSING','UPLOADED','AWAITING_PARTNER','ORPHAN','FAILED')),
				retry_count INTEGER NOT NULL DEFAULT 0,
				max_retries INTEGER NOT NULL DEFAULT 3,
				last_error TEXT,
				uploaded_at DATETIME,
				partner_path TEXT
			)
		`)
		if err != nil {
			return err
		}

		// Copy existing data, generating a legacy hash for rows without one.
		_, err = s.db.Exec(`
			INSERT INTO files_new (id, path, file_hash, original_filename, size, mod_time, status, retry_count, max_retries, last_error, uploaded_at, partner_path)
			SELECT id, path, COALESCE(file_hash, 'legacy_' || id), COALESCE(original_filename, ''), size, mod_time, status, retry_count, max_retries, last_error, uploaded_at, partner_path FROM files
		`)
		if err != nil {
			return err
		}

		// Swap tables.
		_, _ = s.db.Exec("DROP TABLE files")
		_, err = s.db.Exec("ALTER TABLE files_new RENAME TO files")
		if err != nil {
			return err
		}

		// Recreate indexes.
		_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_status_mod_time ON files(status, mod_time);")
		_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_partner_path ON files(partner_path);")
	}

	return nil
}

// RegisterFile handles the detection of a new file and attempts to pair it.
// fileHash is a unique SHA256(content + timestamp) that serves as the DB's unique key.
// originalFilename is the original base name of the file (used in API requests).
func (s *Store) RegisterFile(path string, size int64, modTime time.Time, isMeta bool, expectSidecar bool, fileHash string, originalFilename string) error {
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
		// I am an image (data).
		// Strict/Double Extension: img.png -> img.png.json
		doubleExtPartner := path + ".json"
		// Single Extension: img.png -> img.json
		singleExtPartner := strings.TrimSuffix(path, filepath.Ext(path)) + ".json"

		// Check if either partner exists (pick the latest match if multiple).
		err = tx.QueryRow("SELECT id, status, path FROM files WHERE path = ? OR path = ? ORDER BY id DESC LIMIT 1", doubleExtPartner, singleExtPartner).Scan(&partnerID, &partnerStatus, &partnerPath)
		if err == nil {
			foundPartner = true
		} else if err != sql.ErrNoRows {
			return err
		}

		// If not found, we default to waiting for the Double Extension partner (Standard),
		// but we will accept the Single Extension partner if it arrives later (handled in the isMeta block).
		if !foundPartner {
			partnerPath = doubleExtPartner
		}

	} else {
		// I am metadata (.json).
		// Double Extension: img.png.json -> img.png
		// Single Extension: img.json -> img.png (or img.jpg, etc.)
		base := strings.TrimSuffix(path, ".json")

		// SQLite GLOB or LIKE. LIKE is case insensitive by default in SQLite for ASCII.
		// We look for path = base OR path LIKE base + ".%"
		// Pick the latest match if multiple.
		query := `SELECT id, status, path FROM files WHERE path = ? OR path LIKE ? ORDER BY id DESC LIMIT 1`
		err = tx.QueryRow(query, base, base+".%").Scan(&partnerID, &partnerStatus, &partnerPath)
		if err == nil {
			foundPartner = true
		} else if err != sql.ErrNoRows {
			return err
		}

		// If not found, we don't know the partner path (could be .png, .jpg).
		// So we leave partnerPath empty/null.
	}

	of := sql.NullString{String: originalFilename, Valid: originalFilename != ""}

	if !foundPartner {
		// Partner not found -> I am waiting.
		// If I am an image: partner_path is set to doubleExtPartner (default).
		// If I am meta: partner_path is unknown (NULL).

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
		INSERT INTO files (path, file_hash, original_filename, size, mod_time, status, partner_path, max_retries)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_hash) DO UPDATE SET
			path = excluded.path,
			size = excluded.size,
			mod_time = excluded.mod_time,
			status = ?,
			partner_path = ?,
			original_filename = excluded.original_filename,
			retry_count = 0,
			last_error = NULL,
			max_retries = ?;
		`
		// Reset status and retry state on re-registration
		_, err = tx.Exec(query, path, fileHash, of, size, modTime, initialStatus, pp, s.defaultMaxRetries, initialStatus, pp, s.defaultMaxRetries)
		if err != nil {
			return err
		}
	} else {
		// Partner found!
		// Logic:
		// 1. Update ME to PENDING. Set my partner_path to the found partner.
		// 2. Update PARTNER to PENDING. Ensure their partner_path is ME.

		// Insert/Update ME
		queryMe := `
		INSERT INTO files (path, file_hash, original_filename, size, mod_time, status, partner_path, max_retries)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_hash) DO UPDATE SET
			path = excluded.path,
			size = excluded.size,
			mod_time = excluded.mod_time,
			status = ?,
			partner_path = ?,
			original_filename = excluded.original_filename,
			retry_count = 0,
			last_error = NULL,
			max_retries = ?;
		`
		_, err = tx.Exec(queryMe, path, fileHash, of, size, modTime, StatusPending, partnerPath, s.defaultMaxRetries, StatusPending, partnerPath, s.defaultMaxRetries)
		if err != nil {
			return err
		}

		// Update PARTNER
		// We force it to PENDING so the ingester picks it up.
		// CRITICAL: We also update partner_path.
		// This is vital for the Single Extension case:
		// If Image was waiting for img.png.json, but img.json (ME) arrived and claimed it,
		// we MUST update Image's partner_path to img.json (ME).
		// Also reset partner retry state since the file is ready again.
		queryPartner := `UPDATE files SET status = ?, partner_path = ?, retry_count = 0, last_error = NULL WHERE id = ?`
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
	return s.RegisterFile(path, size, modTime, false, true, "", "")
}

// MarkUploaded updates the status of a file to UPLOADED and sets the uploaded_at timestamp.
// Accepts any non-terminal status to handle direct marking (tests, orphan json, etc.).
func (s *Store) MarkUploaded(path string) error {
	_, err := s.db.Exec(
		`UPDATE files SET status = ?, uploaded_at = ? WHERE path = ? AND status != ?`,
		StatusUploaded, time.Now(), path, StatusUploaded,
	)
	return err
}

// MarkForRetry increments retry_count and moves the file back to PENDING for re-claim.
// If retry_count >= max_retries, the file is marked as FAILED instead.
func (s *Store) MarkForRetry(path string, lastErr string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var retryCount, maxRetries int
	err = tx.QueryRow("SELECT retry_count, max_retries FROM files WHERE path = ?", path).Scan(&retryCount, &maxRetries)
	if err != nil {
		return err
	}

	newRetryCount := retryCount + 1
	if newRetryCount >= maxRetries {
		_, err = tx.Exec(
			`UPDATE files SET status = ?, retry_count = ?, last_error = ? WHERE path = ?`,
			StatusFailed, newRetryCount, lastErr, path,
		)
	} else {
		_, err = tx.Exec(
			`UPDATE files SET status = ?, retry_count = ?, last_error = ? WHERE path = ?`,
			StatusPending, newRetryCount, lastErr, path,
		)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// MarkFailed sets a file's status to FAILED permanently.
func (s *Store) MarkFailed(path string, lastErr string) error {
	_, err := s.db.Exec(
		`UPDATE files SET status = ?, last_error = ? WHERE path = ?`,
		StatusFailed, lastErr, path,
	)
	return err
}

// ClaimPendingFiles atomically claims files for processing.
// It updates matching rows to PROCESSING and returns the claimed records.
// Only files with status IN (PENDING, ORPHAN) and retry_count < max_retries are eligible.
func (s *Store) ClaimPendingFiles(limit int) ([]FileRecord, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Atomically mark rows as PROCESSING and return them via RETURNING
	query := `
	UPDATE files
	SET status = ?
	WHERE id IN (
		SELECT id FROM files
		WHERE status IN (?, ?)
			AND retry_count < max_retries
		ORDER BY mod_time ASC
		LIMIT ?
	)
	RETURNING id, path, file_hash, original_filename, size, mod_time, status, retry_count, max_retries, last_error, uploaded_at, partner_path
	`
	rows, err := tx.Query(query, StatusProcessing, StatusPending, StatusOrphan, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		err := rows.Scan(&f.ID, &f.Path, &f.FileHash, &f.OriginalFilename, &f.Size, &f.ModTime, &f.Status, &f.RetryCount, &f.MaxRetries, &f.LastError, &f.UploadedAt, &f.PartnerPath)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, nil
	}

	return files, tx.Commit()
}

// ResetStaleProcessingFiles resets any files stuck in PROCESSING back to PENDING.
// Should be called once on daemon startup to recover from crashes.
func (s *Store) ResetStaleProcessingFiles() error {
	res, err := s.db.Exec(
		`UPDATE files SET status = ?, retry_count = 0, last_error = NULL WHERE status = ?`,
		StatusPending, StatusProcessing,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		// Logging is done by the caller
	}
	return nil
}

// GetPendingFilesNoClaim returns pending/unclaimed files without changing their state.
// Used by the pruner as a last-resort safety valve when quota is exceeded.
func (s *Store) GetPendingFilesNoClaim(limit int) ([]FileRecord, error) {
	query := `
	SELECT id, path, file_hash, original_filename, size, mod_time, status, retry_count, max_retries, last_error, uploaded_at, partner_path
	FROM files
	WHERE status = ?
	ORDER BY mod_time ASC
	LIMIT ?
	`
	rows, err := s.db.Query(query, StatusPending, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		err := rows.Scan(&f.ID, &f.Path, &f.FileHash, &f.OriginalFilename, &f.Size, &f.ModTime, &f.Status, &f.RetryCount, &f.MaxRetries, &f.LastError, &f.UploadedAt, &f.PartnerPath)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// GetTTLPruneCandidates returns completed files (UPLOADED or FAILED) with mod_time
// older than the given cutoff. Ordered by mod_time ASC for oldest-first eviction.
func (s *Store) GetTTLPruneCandidates(limit int, cutoff time.Time) ([]FileRecord, error) {
	query := `
	SELECT id, path, file_hash, original_filename, size, mod_time, status, retry_count, max_retries, last_error, uploaded_at, partner_path
	FROM files
	WHERE status IN (?, ?) AND mod_time < ?
	ORDER BY mod_time ASC
	LIMIT ?
	`
	rows, err := s.db.Query(query, StatusUploaded, StatusFailed, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		err := rows.Scan(&f.ID, &f.Path, &f.FileHash, &f.OriginalFilename, &f.Size, &f.ModTime, &f.Status, &f.RetryCount, &f.MaxRetries, &f.LastError, &f.UploadedAt, &f.PartnerPath)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// IncrementalVacuum releases a small number of freelist pages back to the OS.
// Only effective when auto_vacuum is set to INCREMENTAL.
func (s *Store) IncrementalVacuum(pages int) error {
	if pages < 1 {
		pages = 1
	}
	_, err := s.db.Exec("PRAGMA incremental_vacuum(" + fmt.Sprintf("%d", pages) + ");")
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
	SELECT id, path, file_hash, original_filename, size, mod_time, status, retry_count, max_retries, last_error, uploaded_at, partner_path
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
		err := rows.Scan(&f.ID, &f.Path, &f.FileHash, &f.OriginalFilename, &f.Size, &f.ModTime, &f.Status, &f.RetryCount, &f.MaxRetries, &f.LastError, &f.UploadedAt, &f.PartnerPath)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, f)
	}
	return candidates, nil
}

// RemoveFile deletes a file record from the database.
// It also clears any references to this file in the partner_path column of other records.
func (s *Store) RemoveFile(path string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Unlink any files that reference this path as their partner
	// This prevents "ghost partners" where a file waits for a non-existent partner.
	queryUnlink := `UPDATE files SET partner_path = NULL WHERE partner_path = ?`
	if _, err := tx.Exec(queryUnlink, path); err != nil {
		return err
	}

	// 2. Delete the file record itself
	queryDelete := `DELETE FROM files WHERE path = ?`
	if _, err := tx.Exec(queryDelete, path); err != nil {
		return err
	}

	return tx.Commit()
}
