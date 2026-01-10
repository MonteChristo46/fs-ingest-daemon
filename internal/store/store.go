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
	StatusPending  FileStatus = "PENDING"  // File detected but not yet successfully uploaded
	StatusUploaded FileStatus = "UPLOADED" // File confirmed uploaded
)

// FileRecord represents a row in the 'files' table.
type FileRecord struct {
	ID         int64
	Path       string
	Size       int64
	ModTime    time.Time
	Status     FileStatus
	UploadedAt sql.NullTime
}

// Store wraps the SQL database connection.
type Store struct {
	db *sql.DB
}

// NewStore initializes the SQLite database connection and runs migrations.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath)
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
		uploaded_at DATETIME
	);
	CREATE INDEX IF NOT EXISTS idx_status_mod_time ON files(status, mod_time);
	`
	_, err := s.db.Exec(query)
	return err
}

// AddOrUpdateFile inserts a new file or updates an existing one.
// If the file already exists, it updates size/mod_time and resets status to PENDING
// to ensure modified files are re-uploaded.
func (s *Store) AddOrUpdateFile(path string, size int64, modTime time.Time) error {
	query := `
	INSERT INTO files (path, size, mod_time, status)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(path) DO UPDATE SET
		size = excluded.size,
		mod_time = excluded.mod_time,
		status = ?;
	`
	_, err := s.db.Exec(query, path, size, modTime, StatusPending, StatusPending)
	return err
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
	SELECT id, path, size, mod_time, status, uploaded_at
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
		err := rows.Scan(&f.ID, &f.Path, &f.Size, &f.ModTime, &f.Status, &f.UploadedAt)
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
func (s *Store) GetPendingFiles(limit int) ([]FileRecord, error) {
	query := `
	SELECT id, path, size, mod_time, status, uploaded_at
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
		err := rows.Scan(&f.ID, &f.Path, &f.Size, &f.ModTime, &f.Status, &f.UploadedAt)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}
