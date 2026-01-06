package store

import (
	"database/sql"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type FileStatus string

const (
	StatusPending  FileStatus = "PENDING"
	StatusUploaded FileStatus = "UPLOADED"
)

type FileRecord struct {
	ID         int64
	Path       string
	Size       int64
	ModTime    time.Time
	Status     FileStatus
	UploadedAt sql.NullTime
}

type Store struct {
	db *sql.DB
}

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

func (s *Store) Close() error {
	return s.db.Close()
}

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

func (s *Store) AddOrUpdateFile(path string, size int64, modTime time.Time) error {
	// If file exists, update size/mod_time and reset status to PENDING if it changed?
	// For now, assume if we detect it again (write), we re-queue it.
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

func (s *Store) MarkUploaded(path string) error {
	query := `
	UPDATE files 
	SET status = ?, uploaded_at = ?
	WHERE path = ?;
	`
	_, err := s.db.Exec(query, StatusUploaded, time.Now(), path)
	return err
}

func (s *Store) GetTotalSize() (int64, error) {
	query := `SELECT COALESCE(SUM(size), 0) FROM files`
	var size int64
	err := s.db.QueryRow(query).Scan(&size)
	return size, err
}

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

func (s *Store) RemoveFile(path string) error {
	query := `DELETE FROM files WHERE path = ?`
	_, err := s.db.Exec(query, path)
	return err
}

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
