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
