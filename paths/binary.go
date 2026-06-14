package paths

import (
	"os"
	"os/exec"
	"path/filepath"
)

// RunningBinaryPath returns the absolute path of the currently running aipmc binary.
func RunningBinaryPath() string {
	bin, err := os.Executable()
	if err != nil {
		bin = os.Args[0]
		if !filepath.IsAbs(bin) {
			bin, _ = filepath.Abs(bin)
		}
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}
	return filepath.ToSlash(bin)
}

// ConfigCommand returns the command string to store in hook/MCP config.
// Uses the portable name "aipmc" when PATH resolves to the same binary; otherwise
// the absolute path of the running executable (never a stale hardcoded path).
func ConfigCommand() string {
	bin := RunningBinaryPath()
	if lp, err := exec.LookPath("aipmc"); err == nil {
		lpReal, err1 := filepath.EvalSymlinks(lp)
		binReal, err2 := filepath.EvalSymlinks(bin)
		if err1 == nil && err2 == nil && lpReal == binReal {
			return "aipmc"
		}
	}
	return bin
}
