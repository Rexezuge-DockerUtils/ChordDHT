package logging

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
)

type Level int32

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var currentLevel atomic.Int32

func init() {
	currentLevel.Store(int32(LevelInfo))
}

func ParseLevel(value string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return LevelDebug, nil
	case "", "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("log level must be one of debug, info, warn, error")
	}
}

func NormalizeLevel(value string) (string, error) {
	level, err := ParseLevel(value)
	if err != nil {
		return "", err
	}
	return level.String(), nil
}

func SetLevel(value string) error {
	level, err := ParseLevel(value)
	if err != nil {
		return err
	}
	currentLevel.Store(int32(level))
	return nil
}

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "info"
	}
}

func Debugf(format string, args ...any) {
	logf(LevelDebug, "DEBUG", format, args...)
}

func Infof(format string, args ...any) {
	logf(LevelInfo, "INFO", format, args...)
}

func Warnf(format string, args ...any) {
	logf(LevelWarn, "WARN", format, args...)
}

func Errorf(format string, args ...any) {
	logf(LevelError, "ERROR", format, args...)
}

func logf(level Level, label, format string, args ...any) {
	if int32(level) < currentLevel.Load() {
		return
	}
	log.Printf(label+" "+format, args...)
}
