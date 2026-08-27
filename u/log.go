package u

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	logMu      sync.Mutex
	logFile    *os.File
	logLogger  *log.Logger
	logProject string
)

const (
	maxLogBytes  = 20 << 20 // 20MB — 超过即归档，保持单文件可快速 grep
	keepArchives = 7        // 保留最近 7 份归档
)

func initSharedLogger() {
	if logLogger != nil {
		return
	}
	// 测试进程隔离（8/18 M1 对账实测）：测试直接调用注入路径会污染生产日志
	// （:148 分母 / write_err）。proxy 包 TestMain 设 AIPMC_LOG=off 整体屏蔽。
	if os.Getenv("AIPMC_LOG") == "off" {
		logLogger = log.New(io.Discard, "", 0)
		return
	}
	// Try project's .pmai directory first
	dir := ".pmai"
	if pmaiDir := pmaiRuntimeDir(); pmaiDir != "" {
		dir = pmaiDir
	}
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		// r19388（8/27）：静默 fallback 会掩盖日志丢失——显式告警到 stderr。
		fmt.Fprintf(os.Stderr, "[aipmc-log] WARN cannot create %s: %v — fallback stderr\n", logsDir, err)
		logLogger = log.New(os.Stderr, "[aipmc] ", 0)
		return
	}

	path := filepath.Join(logsDir, "aipmc.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		// r19388（8/27）：显式告警，不再静默降级。
		fmt.Fprintf(os.Stderr, "[aipmc-log] WARN cannot open %s: %v — fallback stderr\n", path, err)
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

// SetLogProject tags subsequent LogShared lines with the given project name.
// Each serve process serves exactly one project; the proxy/hooks leave it empty
// because their lines span projects (documented limitation, per-request
// attribution is v2 scope).
func SetLogProject(project string) {
	logMu.Lock()
	defer logMu.Unlock()
	logProject = project
}

// sanitizeLog replaces bytes that break BSD grep / file(1) heuristics:
// invalid UTF-8 sequences and C0/C1 control chars (incl. NEL U+0085) become '?'.
func sanitizeLog(s string) string {
	s = strings.ToValidUTF8(s, "?")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			b.WriteRune(r)
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
			b.WriteRune('?')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// maybeRotate archives the log file once it exceeds maxLogBytes and prunes
// old archives. Multi-process safe: it re-checks the size at the path after
// checking the open fd, so a process that lost the rename race skips rotation.
func maybeRotate() {
	if logFile == nil {
		return
	}
	// r19388（8/27）加固：外部进程删除/重建日志文件（rm 而非 rename）时，
	// 本进程 fd 仍指向已删除 inode——写入进黑洞（8/26 18:09→8/27 08:58
	// 整段晚间日志 0 行而库有 290 行活动的疑似根因）。每次写前比对
	// 路径当前 inode，不一致（被替换/删除）即重新打开路径跟随新文件。
	if st, err := os.Stat(logFile.Name()); err == nil {
		if fi, err2 := logFile.Stat(); err2 == nil && !os.SameFile(fi, st) {
			reopenLogFile()
		}
	} else if os.IsNotExist(err) {
		// 路径已被删除 → 重新创建（reopen 的 OpenFile 带 O_CREATE）。
		reopenLogFile()
	}
	fi, err := logFile.Stat()
	if err != nil || fi.Size() < maxLogBytes {
		return
	}
	// Another process may have rotated the path out from under us: our fd
	// still points at the old inode (now an archive), while the path is a new
	// small file. Reopen to follow the current file instead of writing into
	// the archive forever (P1, Claude review 8/14).
	if st, err := os.Stat(logFile.Name()); err != nil || st.Size() < maxLogBytes {
		reopenLogFile()
		return
	}
	archive := logFile.Name() + "." + time.Now().Format("20060102_150405")
	logFile.Close()
	os.Rename(logFile.Name(), archive)
	reopenLogFile()
	pruneArchives(filepath.Dir(logFile.Name()))
}

func reopenLogFile() {
	if logFile != nil {
		logFile.Close() // rotate 分支已先关闭；重复 Close 无害
	}
	f, err := os.OpenFile(logFile.Name(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logFile = nil
		logLogger = log.New(os.Stderr, "[aipmc] ", 0)
		return
	}
	logFile = f
	logLogger = log.New(f, "", 0)
}

func pruneArchives(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, "aipmc.log.*"))
	if err != nil || len(matches) <= keepArchives {
		return
	}
	sort.Strings(matches)
	for _, m := range matches[:len(matches)-keepArchives] {
		os.Remove(m)
	}
}

// LogShared writes a tagged log line to ~/.aipmc/logs/aipmc.log.
// tag is a short component label like "PIPELINE", "INJECT", "LLM".
// 8/12 起带日期前缀 [2006-01-02 15:04:05]（此前仅 [15:04:05]），
// 跨天排障无需再靠上下文猜日期；metrics --window 依赖该日期过滤。
// 8/14 起：写入口自动按大小归档（20MB，保留 7 份）、清洗非法 UTF-8/C1
// 控制字节，且 serve 进程的行带 project=<name> 标签（SetLogProject 注入）。
func LogShared(tag string, format string, args ...any) {
	initSharedLogger()
	if logLogger == nil {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	maybeRotate()
	ts := time.Now().Format("2006-01-02 15:04:05")
	msg := sanitizeLog(fmt.Sprintf(format, args...))
	if logProject != "" {
		logLogger.Printf("[%s] [%s] %s project=%s", ts, tag, msg, logProject)
	} else {
		logLogger.Printf("[%s] [%s] %s", ts, tag, msg)
	}
}
