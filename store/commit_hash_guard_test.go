package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 反馈 #40：record_commit 不校验 hash 是否真是仓库里的对象，agent 编出来的
// 40 位 hash（与真实提交共享前 12 位）会作为第二条记录入库，去重因此失效，
// 账本里也多了一笔不存在的提交。
func TestCanonicalCommitHash(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用")
	}
	repo := t.TempDir()
	env := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+repo,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "seed")
	head := run("rev-parse", "HEAD")

	got, err := canonicalCommitHash(repo, head)
	if err != nil || got != head {
		t.Fatalf("完整 hash 应原样返回: got %q err %v", got, err)
	}
	// 短前缀必须规范化成完整 SHA —— 这正是 hash 去重能命中 hook 记录的前提。
	if got, err = canonicalCommitHash(repo, head[:9]); err != nil || got != head {
		t.Fatalf("短 hash 应规范化为完整 SHA: got %q err %v", got, err)
	}

	// EncryptDrive 反馈 #40 的实例：前 12 位正确、尾部是编的。
	fake := head[:12] + "b5b4c4b8b6ba4aa0f1c6f7e7b0f4"
	if len(fake) != 40 {
		fake = head[:12] + strings.Repeat("0", 28)
	}
	if fake == head {
		t.Fatal("fixture 构造失败：fake 与真实 hash 相同")
	}
	_, err = canonicalCommitHash(repo, fake)
	if err == nil {
		t.Fatal("仓库中不存在的 hash 必须被拒绝")
	}
	for _, want := range []string{"不存在", "rev-parse HEAD", fake} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误信息缺少 %q: %v", want, err)
		}
	}

	// 非 git 目录 / 未指定项目 → 不阻断（保持旧行为，不误伤非 git 场景）。
	plain := t.TempDir()
	if got, err = canonicalCommitHash(plain, fake); err != nil || got != fake {
		t.Errorf("非 git 目录应放行: got %q err %v", got, err)
	}
	t.Setenv("PMAI_HOME", plain+"/.pmai")
	if got, err = canonicalCommitHash("", fake); err != nil || got != fake {
		t.Errorf("projectPath 为空且非仓库时应放行: got %q err %v", got, err)
	}
}
