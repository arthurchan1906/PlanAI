package hook

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"aipmc/store"
	"aipmc/u"
)

// recordBugHintThreshold：变更文件数达到该值视为「大变更」，用于触发 record_bug 提示。
// MVP 阈值（8 文件），待累积数据后校准（P0 ① 落地项）。
const recordBugHintThreshold = 8

// ProcessPostCommitHook handles git post-commit events.
// Called via: aipmc hook post-commit
// Reads the latest commit from git and records it into the PM system
// immediately, bypassing the 30-minute GITSYNC cycle.
func ProcessPostCommitHook() {
	projectPath, _ := os.Getwd()

	// Get latest commit info from git
	hash := gitLog1("%H")   // full commit hash
	title := gitLog1("%s")  // subject line
	date := gitLog1("%cI")  // committer date (ISO 8601)
	files := gitChangedFiles()

	if hash == "" || title == "" {
		u.LogShared("HOOK", "hook=post-commit status=ERR reason=no_git_data")
		return
	}

	commit, err := store.StoreGitCommit(projectPath, title, hash, date, files)
	if err != nil {
		u.LogShared("HOOK", "hook=post-commit status=ERR commit=%s err=%v", u.Prefix(hash, 8), err)
		return
	}

	u.LogShared("HOOK", "hook=post-commit status=OK commit=%s files=%d title=%s",
		u.Prefix(hash, 8), len(files), u.TruncateStr(title, 60))
	fmt.Fprintf(os.Stderr, "aipmc: recorded commit %s (%d files)\n", u.Prefix(hash, 12), len(files))

	// P0 ①（8/31）：高置信自动绑定——commit 在 StoreGitCommit 落库时 task_id=""
	// 靠 agent 补绑 aipm_record_commit；此处用确定性规则（消息含 task 引用 / 文件
	// 唯一命中 in_progress 任务）自动绑，直接降低 commit_orphan。约束 A/B：只做
	// 中性记录绑定，不注入检测结论、不替 agent 决策（低置信留空）。
	if commitID, _ := commit["id"].(string); commitID != "" {
		autoBindCommit(projectPath, commitID, title, files)
	}

	// P0 ①（8/28）：确定性代码钩子——大变更 + 近期无 bug 记录 → 提示 record_bug。
	// 触发是「提示/引导」而非替 agent 自动记录（避免假阳性，agent 自行判断），
	// 呼应 ED 实证：agent 修完 bug 常漏 record_bug。走 stderr + [HOOK] 日志（非注入，
	// 故不被 compaction 吃掉）。
	if shouldHintRecordBug(len(files), recentBugRecorded()) {
		u.LogShared("HOOK", "hook=post-commit status=BUG_HINT commit=%s files=%d action=record_bug_hint",
			u.Prefix(hash, 8), len(files))
		fmt.Fprintf(os.Stderr, "aipmc: 检测到大变更 (%d files) 且近期无 bug 记录——若本次修复了 bug，建议用 aipm_record_bug 记录\n", len(files))
	}
}

// taskRefPattern 匹配 task-YYYYMMDD-HHMMSS-xxxxxx（task 的确定性 ID 形态）。
var taskRefPattern = regexp.MustCompile(`task-\d{8}-\d{6}-[0-9a-f]{6}`)

// resolveAutoBindTask 判定 post-commit 高置信绑定目标（纯函数，便于测试）。
// 规则（约束 A/B，8/26 accepted）：
//  1. commit 标题含 task 引用（task-...）→ 直接命中（确定性、无语义）。
//  2. commit 触碰文件**唯一**命中某 in_progress 任务的 graph 关联 → 命中。
//     命中 0 个或多个任务 → 不确定，返回 ""（不替 agent 决策）。
//  3. 均不满足 → ""（留空）。
func resolveAutoBindTask(title string, files []string, taskFiles map[string][]string) string {
	if id := taskRefPattern.FindString(title); id != "" {
		return id
	}
	return uniqueTaskByFiles(files, taskFiles)
}

// uniqueTaskByFiles 返回恰好一个候选任务（commit 文件唯一命中），否则 ""。
func uniqueTaskByFiles(files []string, taskFiles map[string][]string) string {
	if len(files) == 0 || len(taskFiles) == 0 {
		return ""
	}
	candidates := map[string]bool{}
	for _, f := range files {
		for tid, tfs := range taskFiles {
			for _, tf := range tfs {
				if tf == f {
					candidates[tid] = true
					break
				}
			}
		}
	}
	if len(candidates) == 1 {
		for tid := range candidates {
			return tid
		}
	}
	return ""
}

// autoBindCommit 在 post-commit 中执行高置信自动绑定。绑定成功记 [HOOK] BIND、
// 失败记 BIND_ERR；低置信（无匹配）静默跳过（不追加提示噪音，避免重复引导）。
func autoBindCommit(projectPath, commitID, title string, files []string) {
	taskID := resolveAutoBindTask(title, files, store.ListTaskFileAssoc("in_progress"))
	if taskID == "" {
		u.LogShared("HOOK", "hook=post-commit status=NO_BIND commit=%s reason=no_high_confidence", u.Prefix(commitID, 8))
		return
	}
	code, err := store.BackfillCommitTask(projectPath, commitID, taskID, title, files)
	if err != nil {
		u.LogShared("HOOK", "hook=post-commit status=BIND_ERR commit=%s task=%s err=%v", u.Prefix(commitID, 8), taskID, err)
		return
	}
	u.LogShared("HOOK", "hook=post-commit status=BIND code=%d commit=%s task=%s action=auto_bind", code, u.Prefix(commitID, 8), taskID)
}

// shouldHintRecordBug 纯函数：大变更（≥ 阈值变更文件）且近期无 bug 记录 → 应提示。
func shouldHintRecordBug(fileCount int, recentBug bool) bool {
	return fileCount >= recordBugHintThreshold && !recentBug
}

// recentBugRecorded 报告当前项目近 7 天是否已有 bug 记录（确定性、无语义）。
func recentBugRecorded() bool {
	bugs, err := store.ListBugs("", "", "", 1, 0)
	if err != nil || len(bugs) == 0 {
		return false
	}
	created, _ := bugs[0]["created_at"].(string)
	if created == "" {
		return false
	}
	// created_at 为 ISO 8601 字符串（u.NowISO），可直接字典序比较。
	since := time.Now().Add(-7 * 24 * time.Hour).Format("2006-01-02T15:04:05")
	return created >= since
}

// gitLog1 runs `git log -1 --format=<format>` and returns the output.
func gitLog1(format string) string {
	cmd := exec.Command("git", "log", "-1", "--format="+format)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitChangedFiles returns the list of files changed in the latest commit.
func gitChangedFiles() []string {
	cmd := exec.Command("git", "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f = strings.TrimSpace(f)
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}
