package u

import (
	"os"
	"path/filepath"
	"testing"
)

// 8/14 回归（Claude 审核 P1）：多进程并发旋转时，输家进程的 fd 仍指向旧
// inode（已变为归档），必须 reopen 跟随新文件——否则会永久写入归档、被
// metrics 漏统计，且归档可能在保留轮次后被 prune 删除。
func TestMaybeRotateFollowsNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aipmc.log")

	// 本进程 fd 指向已超阈值的旧文件。
	big := make([]byte, maxLogBytes)
	if err := os.WriteFile(path, big, 0644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	logFile = f

	// 另一进程已把路径旋转走：旧 inode 变为归档，路径指向新小文件。
	if err := os.Rename(path, path+".20260814_test"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}

	maybeRotate()
	if logFile == nil {
		t.Fatal("maybeRotate 把 logFile 置为 nil")
	}
	if logFile.Name() != path {
		t.Fatalf("logFile 指向 %s，应跟随新文件 %s", logFile.Name(), path)
	}
	if _, err := logFile.WriteString("follow\n"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "follow\n" {
		t.Fatalf("当前文件内容 %q，应为 follow 行", got)
	}
	archive, err := os.ReadFile(path + ".20260814_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(archive) != maxLogBytes {
		t.Fatalf("归档内容被改动：len=%d，应为 %d", len(archive), maxLogBytes)
	}
}

// r19388（8/27）回归：外部进程删除日志文件（rm 而非 rename）且当前文件
// 未超阈值时，fd 仍指向已删除 inode——写入进黑洞。修复：每次写前比对
// 路径 inode，不一致即 reopen 跟随新文件（8/26 晚间日志 0 行的疑似根因）。
func TestMaybeRotateReopensAfterExternalDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aipmc.log")

	if err := os.WriteFile(path, []byte("existing\n"), 0644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	logFile = f

	// 模拟外部进程 rm 文件（小文件，未超 maxLogBytes——原 maybeRotate 直接 return 的场景）。
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	maybeRotate()
	if logFile == nil {
		t.Fatal("maybeRotate 把 logFile 置为 nil")
	}
	// reopen 的 OpenFile 带 O_CREATE：路径被重建，后续写入落在新文件而非黑洞。
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("路径未被重建: %v", err)
	}
	if _, err := logFile.WriteString("after-delete\n"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "after-delete\n" {
		t.Fatalf("重建后文件内容 %q，应为 after-delete 行（旧句柄写入必须落到新文件）", got)
	}
}

// r19388 二次加固（Claude 8/27 15:43 审核）：fallback 不能只写 stderr——
// 根因场景恰是 stderr 被后台进程丢弃，告警写 stderr 等于没写。多路 writer
// 必须把降级期日志落到 os.TempDir() 下的固定文件（第二物理路径）。
func TestFallbackLogWriterWritesSecondPath(t *testing.T) {
	path := fallbackLogPath()
	// 清理测试残留
	_ = os.Remove(path)

	w := fallbackLogWriter()
	msg := "fallback-probe-write\n"
	if _, err := w.Write([]byte(msg)); err != nil {
		t.Fatalf("fallback write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fallback 文件未写入: %v", err)
	}
	if string(got) != msg {
		t.Fatalf("fallback 内容 %q, want %q", got, msg)
	}
	// 清理测试产物，避免污染 /tmp
	_ = os.Remove(path)
}

// r19388 二次加固：正常路径打开成功时不应创建 fallback 文件（仅降级期使用）。
func TestNormalPathNoFallbackFile(t *testing.T) {
	path := fallbackLogPath()
	_ = os.Remove(path)
	// 触发一次正常 init（AIPMC_LOG 非 off 且 ~/.aipmc/logs 可写时走正常路径）；
	// 若 logLogger 已初始化则跳过（幂等保护）。
	if logLogger == nil && os.Getenv("AIPMC_LOG") != "off" {
		initSharedLogger()
	}
	if _, err := os.Stat(path); err == nil {
		// 正常路径不应创建 fallback 文件；但若本进程此前已进入过 fallback
		// 状态则文件可能已存在——仅当 logFile 非 nil（正常状态）时断言。
		if logFile != nil {
			t.Fatalf("正常路径不应创建 fallback 文件 %s", path)
		}
	}
}
