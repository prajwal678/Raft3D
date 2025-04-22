package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarning
	LevelError
)

var levelNames = map[LogLevel]string{
	LevelDebug:   "DEBUG",
	LevelInfo:    "INFO",
	LevelWarning: "WARN",
	LevelError:   "ERROR",
}

type Logger struct {
	nodeID       string
	fileHandle   *os.File
	consoleLevel LogLevel
	fileLevel    LogLevel
}

func NewLogger(nodeID, logDir string, consoleLevel, fileLevel LogLevel) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %v", err)
	}

	logFile := filepath.Join(logDir, fmt.Sprintf("%s.log", nodeID))
	fileHandle, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %v", err)
	}

	return &Logger{
		nodeID:       nodeID,
		fileHandle:   fileHandle,
		consoleLevel: consoleLevel,
		fileLevel:    fileLevel,
	}, nil
}

func (l *Logger) log(level LogLevel, prefix string, format string, args []interface{}, isEssential bool) {
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	message := fmt.Sprintf(format, args...)
	logLine := fmt.Sprintf("[%s] [%s] [%s] %s\n", timestamp, l.nodeID, prefix, message)

	if level >= l.fileLevel {
		l.fileHandle.WriteString(logLine)
		l.fileHandle.Sync()
	}

	if level >= l.consoleLevel {
		if isEssential {
			// Essential messages are always green for emphasis
			fmt.Printf("\033[1;32m%s\033[0m", logLine) // bright green
		} else {
			switch level {
			case LevelDebug:
				fmt.Printf("\033[0;34m%s\033[0m", logLine) // dim blue
			case LevelInfo:
				fmt.Printf("%s", logLine) // normal white
			case LevelWarning:
				fmt.Printf("\033[1;33m%s\033[0m", logLine) // bright yellow
			case LevelError:
				fmt.Printf("\033[1;31m%s\033[0m", logLine) // bright red
			}
		}
	}
}

func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(LevelDebug, "DEBUG", format, args, false)
}

func (l *Logger) Info(format string, args ...interface{}) {
	l.log(LevelInfo, "INFO", format, args, false)
}

func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(LevelWarning, "WARN", format, args, false)
}

func (l *Logger) Error(format string, args ...interface{}) {
	l.log(LevelError, "ERROR", format, args, false)
}

// Essential methods - these log with green highlighting for important messages
func (l *Logger) DebugEssential(format string, args ...interface{}) {
	l.log(LevelDebug, "DEBUG", format, args, true)
}

func (l *Logger) InfoEssential(format string, args ...interface{}) {
	l.log(LevelInfo, "INFO", format, args, true)
}

func (l *Logger) WarnEssential(format string, args ...interface{}) {
	l.log(LevelWarning, "WARN", format, args, true)
}

func (l *Logger) ErrorEssential(format string, args ...interface{}) {
	l.log(LevelError, "ERROR", format, args, true)
}

func (l *Logger) Close() error {
	return l.fileHandle.Close()
}
