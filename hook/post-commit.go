package hook

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"aipmc/store"
	"aipmc/u"
)

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

	_, err := store.StoreGitCommit(projectPath, title, hash, date, files)
	if err != nil {
		u.LogShared("HOOK", "hook=post-commit status=ERR commit=%s err=%v", u.Prefix(hash, 8), err)
		return
	}

	u.LogShared("HOOK", "hook=post-commit status=OK commit=%s files=%d title=%s",
		u.Prefix(hash, 8), len(files), u.TruncateStr(title, 60))
	fmt.Fprintf(os.Stderr, "aipmc: recorded commit %s (%d files)\n", u.Prefix(hash, 12), len(files))
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
