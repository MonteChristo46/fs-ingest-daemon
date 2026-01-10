package logger

// Package logger provides a unified logging interface for the daemon.
// It wraps the system service logger (provided by kardianos/service) and a standard file logger.
// This ensures logs are captured both in the OS service manager (e.g., Event Viewer, Syslog)
// and in a local text file for easy debugging.

import (
	"log"

	"github.com/kardianos/service"
)

// CompositeLogger writes logs to both the system service logger and a file.
type CompositeLogger struct {
	svcLogger  service.Logger // The OS service logger
	fileLogger *log.Logger    // The local file logger
}

// New creates a new instance of CompositeLogger.
func New(svcLogger service.Logger, fileLogger *log.Logger) *CompositeLogger {
	return &CompositeLogger{
		svcLogger:  svcLogger,
		fileLogger: fileLogger,
	}
}

// Error writes an error message to both loggers.
func (l *CompositeLogger) Error(v ...interface{}) error {
	l.fileLogger.Println(append([]interface{}{"ERROR:"}, v...)...)
	if l.svcLogger != nil {
		return l.svcLogger.Error(v...)
	}
	return nil
}

// Warning writes a warning message to both loggers.
func (l *CompositeLogger) Warning(v ...interface{}) error {
	l.fileLogger.Println(append([]interface{}{"WARNING:"}, v...)...)
	if l.svcLogger != nil {
		return l.svcLogger.Warning(v...)
	}
	return nil
}

// Info writes an info message to both loggers.
func (l *CompositeLogger) Info(v ...interface{}) error {
	l.fileLogger.Println(append([]interface{}{"INFO:"}, v...)...)
	if l.svcLogger != nil {
		return l.svcLogger.Info(v...)
	}
	return nil
}

// Errorf writes a formatted error message to both loggers.
func (l *CompositeLogger) Errorf(format string, a ...interface{}) error {
	l.fileLogger.Printf("ERROR: "+format, a...)
	if l.svcLogger != nil {
		return l.svcLogger.Errorf(format, a...)
	}
	return nil
}

// Warningf writes a formatted warning message to both loggers.
func (l *CompositeLogger) Warningf(format string, a ...interface{}) error {
	l.fileLogger.Printf("WARNING: "+format, a...)
	if l.svcLogger != nil {
		return l.svcLogger.Warningf(format, a...)
	}
	return nil
}

// Infof writes a formatted info message to both loggers.
func (l *CompositeLogger) Infof(format string, a ...interface{}) error {
	l.fileLogger.Printf("INFO: "+format, a...)
	if l.svcLogger != nil {
		return l.svcLogger.Infof(format, a...)
	}
	return nil
}
