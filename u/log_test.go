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
