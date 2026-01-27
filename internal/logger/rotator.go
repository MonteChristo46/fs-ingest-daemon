package logger

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ensure we implement io.WriteCloser
var _ io.WriteCloser = (*LogRotator)(nil)

// LogRotator writes to a log file and rotates it when it reaches a certain size.
type LogRotator struct {
	// Config
	Filename   string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool

	// Internal
	size int64
	file *os.File
	mu   sync.Mutex
}

// Write writes data to the log file, rotating if necessary.
func (l *LogRotator) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	writeLen := int64(len(p))
	if writeLen > l.max() {
		return 0, fmt.Errorf("write length %d exceeds max file size %d", writeLen, l.max())
	}

	if l.file == nil {
		if err = l.openExistingOrNew(len(p)); err != nil {
			return 0, err
		}
	}

	if l.size+writeLen > l.max() {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}

	n, err = l.file.Write(p)
	l.size += int64(n)
	return n, err
}

// Close closes the file.
func (l *LogRotator) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.close()
}

func (l *LogRotator) close() error {
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// openExistingOrNew opens the existing log file or creates a new one.
// It also checks the size of the existing file.
func (l *LogRotator) openExistingOrNew(writeLen int) error {
	info, err := os.Stat(l.Filename)
	if os.IsNotExist(err) {
		return l.openNew()
	}
	if err != nil {
		return fmt.Errorf("error getting log file info: %s", err)
	}

	if info.Size()+int64(writeLen) >= l.max() {
		return l.rotate()
	}

	file, err := os.OpenFile(l.Filename, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return l.openNew() // Try to open new if append fails (e.g. permission changed?)
	}

	l.file = file
	l.size = info.Size()
	return nil
}

// openNew opens a new log file, truncating if it exists (though strictly we rely on rotation logic).
func (l *LogRotator) openNew() error {
	err := os.MkdirAll(filepath.Dir(l.Filename), 0755)
	if err != nil {
		return fmt.Errorf("can't make directories for new logfile: %s", err)
	}

	name := l.Filename
	mode := os.FileMode(0644)
	info, err := os.Stat(name)
	if err == nil {
		mode = info.Mode()
		// If it exists and we're calling openNew, it implies we want to clear or start fresh,
		// but openNew is mostly called by rotate or when file is missing.
		// Standard file creation:
	}

	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("can't open new logfile: %s", err)
	}
	l.file = f
	l.size = 0
	return nil
}

// rotate closes the current file, renames it, and opens a new one.
func (l *LogRotator) rotate() error {
	if err := l.close(); err != nil {
		return err
	}

	// Rename the existing file before creating a new one
	_, err := os.Stat(l.Filename)
	if err == nil {
		backupName := l.backupName()
		if err := os.Rename(l.Filename, backupName); err != nil {
			return fmt.Errorf("failed to rename log file: %s", err)
		}

		// Run post-rotation tasks (compression, cleanup) in background
		go l.postRotate(backupName)
	}

	// Now open fresh file
	if err := l.openNew(); err != nil {
		return err
	}

	return nil
}

func (l *LogRotator) backupName() string {
	dir := filepath.Dir(l.Filename)
	filename := filepath.Base(l.Filename)
	ext := filepath.Ext(filename)
	prefix := filename[:len(filename)-len(ext)]
	timestamp := time.Now().Format("2006-01-02T15-04-05.000")
	return filepath.Join(dir, fmt.Sprintf("%s-%s%s", prefix, timestamp, ext))
}

func (l *LogRotator) max() int64 {
	if l.MaxSizeMB == 0 {
		return int64(10 * 1024 * 1024) // Default 10MB
	}
	return int64(l.MaxSizeMB) * int64(1024*1024)
}

// postRotate handles compression and cleanup of old files.
func (l *LogRotator) postRotate(filename string) {
	if l.Compress {
		if err := compressLogFile(filename); err != nil {
			// In a real logger, we might want to log this error to stderr?
			// or just ignore.
			// fmt.Fprintf(os.Stderr, "failed to compress log: %v\n", err)
		} else {
			// Remove the uncompressed source
			os.Remove(filename)
			filename = filename + ".gz" // Update for cleanup check
		}
	}

	l.cleanup()
}

func (l *LogRotator) cleanup() {
	if l.MaxBackups == 0 && l.MaxAgeDays == 0 {
		return
	}

	files, err := l.oldLogFiles()
	if err != nil {
		return
	}

	// Delete by age
	var remaining []logInfo
	if l.MaxAgeDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -l.MaxAgeDays)
		for _, f := range files {
			if f.timestamp.Before(cutoff) {
				os.Remove(f.path)
			} else {
				remaining = append(remaining, f)
			}
		}
		files = remaining
	}

	// Delete by count (MaxBackups)
	// We want to keep the newest ones.
	// files are sorted by timestamp (oldest first).
	if l.MaxBackups > 0 && len(files) > l.MaxBackups {
		filesToDelete := len(files) - l.MaxBackups
		for i := 0; i < filesToDelete; i++ {
			os.Remove(files[i].path)
		}
	}
}

type logInfo struct {
	timestamp time.Time
	path      string
}

func (l *LogRotator) oldLogFiles() ([]logInfo, error) {
	files, err := os.ReadDir(filepath.Dir(l.Filename))
	if err != nil {
		return nil, err
	}

	var logFiles []logInfo
	base := filepath.Base(l.Filename)
	ext := filepath.Ext(base)
	prefix := base[:len(base)-len(ext)]

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		// Check prefix
		if !strings.HasPrefix(name, prefix+"-") {
			continue
		}
		// Check extension (log or log.gz)
		if !strings.HasSuffix(name, ext) && !strings.HasSuffix(name, ext+".gz") {
			continue
		}

		// Extract timestamp
		// Format: prefix-YYYY-MM-DDTHH-MM-SS.000.log(.gz)
		// Removing prefix-
		tsStr := name[len(prefix)+1:]
		// Find where the extension starts
		dot := strings.Index(tsStr, ".")
		if dot == -1 {
			continue
		}
		// The timestamp part is before the dot? No, .log is extension.
		// Wait, format is 2006-01-02T15-04-05.000
		// so it has dots.
		// The ext is .log.
		// If compressed, .log.gz.

		// Let's strip known suffixes.
		tsPart := tsStr
		if strings.HasSuffix(tsPart, ".gz") {
			tsPart = tsPart[:len(tsPart)-3]
		}
		if strings.HasSuffix(tsPart, ext) {
			tsPart = tsPart[:len(tsPart)-len(ext)]
		}

		t, err := time.Parse("2006-01-02T15-04-05.000", tsPart)
		if err == nil {
			logFiles = append(logFiles, logInfo{timestamp: t, path: filepath.Join(filepath.Dir(l.Filename), name)})
		}
	}

	sort.Slice(logFiles, func(i, j int) bool {
		return logFiles[i].timestamp.Before(logFiles[j].timestamp)
	})

	return logFiles, nil
}

func compressLogFile(src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	dst := src + ".gz"
	gzf, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer gzf.Close()

	zw := gzip.NewWriter(gzf)
	defer zw.Close()

	if _, err := io.Copy(zw, f); err != nil {
		return err
	}
	return nil
}
