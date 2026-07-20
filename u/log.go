package u

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	logMu      sync.Mutex
	logFile    *os.File
	logLogger  *log.Logger
)

func initSharedLogger() {
	if logLogger != nil {
		return
	}
	// Try project's .pmai directory first
	dir := ".pmai"
	if pmaiDir := pmaiRuntimeDir(); pmaiDir != "" {
		dir = pmaiDir
	}
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		// Fallback to stderr if we can't create the log directory
		logLogger = log.New(os.Stderr, "[aipmc] ", 0)
		return
	}

	path := filepath.Join(logsDir, "aipmc.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		// Fallback to stderr if we can't open the log file
		logLogger = log.New(os.Stderr, "[aipmc] ", 0)
		return
	}
	logFile = f
	logLogger = log.New(f, "", 0)
}

func pmaiRuntimeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".aipmc")
}

// LogShared writes a tagged log line to .pmai/logs/aipmc.log.
// tag is a short component label like "PIPELINE", "INJECT", "LLM".
func LogShared(tag string, format string, args ...any) {
	initSharedLogger()
	if logLogger == nil {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	ts := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	logLogger.Printf("[%s] [%s] %s", ts, tag, msg)
}
