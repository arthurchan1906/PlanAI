package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InstallPostCommitHook installs a git post-commit hook that calls
// `aipmc hook-post-commit` after every git commit. If a hook already
// exists, it appends the aipmc call rather than overwriting.
func InstallPostCommitHook() error {
	gitDir, err := findGitDir()
	if err != nil {
		return fmt.Errorf("not in a git repository (or any parent): %w", err)
	}

	hookPath := filepath.Join(gitDir, "hooks", "post-commit")
	aipmcLine := "aipmc hook-post-commit"

	// Check if already installed
	if data, err := os.ReadFile(hookPath); err == nil {
		if strings.Contains(string(data), aipmcLine) {
			fmt.Printf("post-commit hook already installed at %s\n", hookPath)
			return nil
		}
		// Append to existing hook
		f, err := os.OpenFile(hookPath, os.O_APPEND|os.O_WRONLY, 0755)
		if err != nil {
			return fmt.Errorf("cannot append to %s: %w", hookPath, err)
		}
		defer f.Close()
		fmt.Fprintf(f, "\n# aipmc: record commits to PM system\n%s\n", aipmcLine)
		fmt.Printf("appended aipmc to existing post-commit hook at %s\n", hookPath)
		return nil
	}

	// Create new hook
	script := fmt.Sprintf("#!/bin/sh\n# aipmc: record commits to PM system\n%s\n", aipmcLine)
	if err := os.WriteFile(hookPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", hookPath, err)
	}
	fmt.Printf("installed post-commit hook at %s\n", hookPath)
	return nil
}

// UninstallPostCommitHook removes aipmc lines from the post-commit hook.
// If only aipmc lines remain, the entire file is removed.
func UninstallPostCommitHook() error {
	gitDir, err := findGitDir()
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	hookPath := filepath.Join(gitDir, "hooks", "post-commit")
	data, err := os.ReadFile(hookPath)
	if err != nil {
		fmt.Printf("no post-commit hook found at %s\n", hookPath)
		return nil
	}

	lines := strings.Split(string(data), "\n")
	var filtered []string
	for _, l := range lines {
		if !strings.Contains(l, "aipmc") {
			filtered = append(filtered, l)
		}
	}

	// If nothing left but shebang or empty, remove the file
	content := strings.TrimSpace(strings.Join(filtered, "\n"))
	if content == "" || content == "#!/bin/sh" {
		os.Remove(hookPath)
		fmt.Printf("removed post-commit hook at %s\n", hookPath)
		return nil
	}

	if err := os.WriteFile(hookPath, []byte(strings.Join(filtered, "\n")+"\n"), 0755); err != nil {
		return fmt.Errorf("cannot update %s: %w", hookPath, err)
	}
	fmt.Printf("removed aipmc from post-commit hook at %s\n", hookPath)
	return nil
}

// findGitDir finds the .git directory by walking up from CWD.
func findGitDir() (string, error) {
	cwd, _ := os.Getwd()
	for dir := cwd; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		gitPath := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitPath); err == nil && info.IsDir() {
			return gitPath, nil
		}
	}
	return "", fmt.Errorf(".git not found")
}
