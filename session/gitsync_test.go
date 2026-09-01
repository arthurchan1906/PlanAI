package session

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseGitLogRealLayout 回归测试：真实 git log --format=%H|%s|%aI --name-only
// 布局（8/31 实测）——文件列表后直接紧跟下一 commit header，没有空行分隔。
// 旧实现把下一 header 误 append 为上一 commit 的文件（污染 files_json），
// 并让下一 commit 的文件行被当 header 丢弃。
func TestParseGitLogRealLayout(t *testing.T) {
	// 来自真实仓库输出：0dd3f57(fix ui,cred) 4 文件 → 61d4355(ci release) — 无空行分隔
	out := strings.Join([]string{
		"0dd3f5783a3efc919be079adea09fb8cfaf34617|fix(ui,cred): 修复凭据 set 静默失败与弹窗表单残留|2026-08-31T19:42:33+08:00",
		"",
		"api/config.go",
		"frontend/dist/index.html",
		"frontend/src/components/ModelRegistryEditor.jsx",
		"frontend/src/views/SettingsView.jsx",
		"61d435517615ebb380fbe668790de09448d21091|ci(release): 触发模式修正为 [0-9]*|2026-08-31T17:58:08+08:00",
		"",
		".github/workflows/release.yml",
		"a70125a6bc4f7ac7c32ff4e95c90c23f2d896845|fix(p0-1,p0-4a): 按 Claude 复核意见落地|2026-08-31T17:56:10+08:00",
		"",
	}, "\n")

	gitCommits := parseGitLog(out)
	if len(gitCommits) != 3 {
		t.Fatalf("want 3 commits, got %d: %+v", len(gitCommits), gitCommits)
	}

	// 第 1 个 commit：4 个真实文件，绝不能被 61d4355 的 header 污染
	wantA := gitCommit{
		hash:  "0dd3f5783a3efc919be079adea09fb8cfaf34617",
		title: "fix(ui,cred): 修复凭据 set 静默失败与弹窗表单残留",
		date:  "2026-08-31T19:42:33+08:00",
		files: []string{
			"api/config.go",
			"frontend/dist/index.html",
			"frontend/src/components/ModelRegistryEditor.jsx",
			"frontend/src/views/SettingsView.jsx",
		},
	}
	if !reflect.DeepEqual(gitCommits[0], wantA) {
		t.Errorf("commit A mismatch:\n got %+v\nwant %+v", gitCommits[0], wantA)
	}

	// 第 2 个 commit：其 header 不再被吞、文件正确归属
	wantB := gitCommit{
		hash:  "61d435517615ebb380fbe668790de09448d21091",
		title: "ci(release): 触发模式修正为 [0-9]*",
		date:  "2026-08-31T17:58:08+08:00",
		files: []string{".github/workflows/release.yml"},
	}
	if !reflect.DeepEqual(gitCommits[1], wantB) {
		t.Errorf("commit B mismatch:\n got %+v\nwant %+v", gitCommits[1], wantB)
	}

	// 第 3 个 commit：无文件的块也正确闭合（不由空行残留导致粘连）
	if gitCommits[2].hash != "a70125a6bc4f7ac7c32ff4e95c90c23f2d896845" || len(gitCommits[2].files) != 0 {
		t.Errorf("commit C mismatch: %+v", gitCommits[2])
	}
}

// TestParseGitLogMergeNoFiles merge/空 commit（无文件列表，header 后直接下一 header）
// 不产生污染、不粘连。
func TestParseGitLogMergeNoFiles(t *testing.T) {
	out := strings.Join([]string{
		"1111111111111111111111111111111111111111|Merge remote-tracking branch 'origin/main'|2026-08-26T23:36:27+08:00",
		"2222222222222222222222222222222222222222|docs(guide): 落实品牌统一|2026-08-26T18:02:24+08:00",
		"",
		"README.md",
	}, "\n")

	gitCommits := parseGitLog(out)
	if len(gitCommits) != 2 {
		t.Fatalf("want 2 commits, got %d: %+v", len(gitCommits), gitCommits)
	}
	if len(gitCommits[0].files) != 0 {
		t.Errorf("merge commit must have no files, got %+v", gitCommits[0].files)
	}
	want := []string{"README.md"}
	if !reflect.DeepEqual(gitCommits[1].files, want) {
		t.Errorf("second commit files mismatch: %+v", gitCommits[1].files)
	}
}

// TestParseGitLogNoTrailingNewline 输出无尾随换行（cmd.Output 常见）也能正确解析。
func TestParseGitLogNoTrailingNewline(t *testing.T) {
	out := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|commit A|2026-08-31T10:00:00+08:00\n\nfile.go"
	gitCommits := parseGitLog(out)
	if len(gitCommits) != 1 {
		t.Fatalf("want 1 commit, got %d: %+v", len(gitCommits), gitCommits)
	}
	if len(gitCommits[0].files) != 1 || gitCommits[0].files[0] != "file.go" {
		t.Errorf("files mismatch: %+v", gitCommits[0])
	}
}

// TestParseGitLogTitleVariants 覆盖 title/date 各形态（表驱动）：
// 标题含 "|"、空标题、无 date。旧实现用 SplitN(line,"|",3) 从第一个 "|" 切，
// 会把标题后半段误当 date；修复后从最右 "|" 切，title/date 各保真。
func TestParseGitLogTitleVariants(t *testing.T) {
	const h = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cases := []struct {
		name    string
		header  string // 完整 header 行（hash|title|date）
		wantTit string
		wantDat string
	}{
		{
			name:    "title 含多个 pipe",
			header:  h + "|fix(a|b): 标题里带 | 的提交|2026-09-01T10:00:00+08:00",
			wantTit: "fix(a|b): 标题里带 | 的提交",
			wantDat: "2026-09-01T10:00:00+08:00",
		},
		{
			name:    "空标题",
			header:  h + "||2026-09-01T10:00:00+08:00",
			wantTit: "",
			wantDat: "2026-09-01T10:00:00+08:00",
		},
		{
			name:    "无 date",
			header:  h + "|chore: 只有标题没有日期",
			wantTit: "chore: 只有标题没有日期",
			wantDat: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := strings.Join([]string{tc.header, "", "a.go", "b.go"}, "\n")
			gitCommits := parseGitLog(out)
			if len(gitCommits) != 1 {
				t.Fatalf("want 1 commit, got %d: %+v", len(gitCommits), gitCommits)
			}
			c := gitCommits[0]
			if c.hash != h {
				t.Errorf("hash mismatch: %q", c.hash)
			}
			if c.title != tc.wantTit {
				t.Errorf("title mismatch: got %q want %q", c.title, tc.wantTit)
			}
			if c.date != tc.wantDat {
				t.Errorf("date mismatch: got %q want %q", c.date, tc.wantDat)
			}
			if len(c.files) != 2 || c.files[0] != "a.go" || c.files[1] != "b.go" {
				t.Errorf("files mismatch: %+v", c.files)
			}
		})
	}
}
