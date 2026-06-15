package cursor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	pmdb "aipmc/db"
	"aipmc/paths"
)

const cursorHookJS = `'use strict';

const { spawnSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const CONFIG_PATH = path.join(__dirname, 'aipm-config.json');
const PROJECT_ROOT = path.join(__dirname, '..', '..');

function readStdin() {
  let buf = fs.readFileSync(0);
  if (buf.length >= 3 && buf[0] === 0xef && buf[1] === 0xbb && buf[2] === 0xbf) {
    buf = buf.subarray(3);
  }
  return buf;
}

function loadConfig() {
  try {
    return JSON.parse(fs.readFileSync(CONFIG_PATH, 'utf8'));
  } catch {
    return { command: 'aipmc', args: ['hook-cursor'] };
  }
}

function defaultResponse(eventName) {
  switch (eventName) {
    case 'beforeSubmitPrompt':
      return '{"continue":true}\n';
    case 'preToolUse':
    case 'beforeShellExecution':
    case 'beforeMCPExecution':
    case 'beforeReadFile':
    case 'subagentStart':
      return '{"permission":"allow"}\n';
    default:
      return '';
  }
}

function main() {
  const input = readStdin();
  let eventName = '';
  try {
    eventName = JSON.parse(input.toString('utf8')).hook_event_name || '';
  } catch (_) {}

  const cfg = loadConfig();
  const command = cfg.command || 'aipmc';
  const args = Array.isArray(cfg.args) ? cfg.args : ['hook-cursor'];

  try {
    const result = spawnSync(command, args, {
      input,
      cwd: PROJECT_ROOT,
      maxBuffer: 10 * 1024 * 1024,
      windowsHide: true,
    });

    const stdout = (result.stdout ? result.stdout.toString('utf8') : '').trim();
    if (stdout) {
      process.stdout.write(stdout.endsWith('\n') ? stdout : stdout + '\n');
    } else {
      process.stdout.write(defaultResponse(eventName));
    }
  } catch (_) {
    process.stdout.write(defaultResponse(eventName));
  }

  process.exit(0);
}

main();
`

var cursorHookEvents = []string{
	"beforeSubmitPrompt",
	"afterAgentResponse",
	"afterAgentThought",
	"postToolUse",
	"postToolUseFailure",
	"afterFileEdit",
	"afterShellExecution",
	"stop",
}

// SetupHooks installs Cursor project hooks (.cursor/hooks.json + aipm-hook.js).
func SetupHooks(_ string) error {
	runtimeDir, err := pmdb.RuntimeDir()
	if err != nil {
		return fmt.Errorf("find runtime dir: %w", err)
	}
	projectRoot := filepath.Dir(runtimeDir)
	cursorDir := filepath.Join(projectRoot, ".cursor")
	hooksDir := filepath.Join(cursorDir, "hooks")
	hooksPath := filepath.Join(cursorDir, "hooks.json")
	scriptPath := filepath.Join(hooksDir, "aipm-hook.js")
	configPath := filepath.Join(hooksDir, "aipm-config.json")

	cfg := map[string]any{}
	if data, err := os.ReadFile(hooksPath); err == nil && len(data) > 0 {
		json.Unmarshal(data, &cfg)
	}
	if _, ok := cfg["version"]; !ok {
		cfg["version"] = 1
	}
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("create hooks dir: %w", err)
	}
	if err := os.WriteFile(scriptPath, []byte(cursorHookJS), 0644); err != nil {
		return fmt.Errorf("write hook script: %w", err)
	}

	// Cursor hook subprocesses do not inherit the shell PATH; use absolute binary path.
	aipmcBin := paths.RunningBinaryPath()
	configData, _ := json.MarshalIndent(map[string]any{
		"command": aipmcBin,
		"args":    []string{"hook-cursor"},
	}, "", "  ")
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("write hook config: %w", err)
	}

	nodePath, err := resolveNodePath()
	if err != nil {
		return err
	}
	hookCmd := shellQuote(nodePath) + " .cursor/hooks/aipm-hook.js"
	makeEntry := func() []any {
		return []any{map[string]any{"command": hookCmd, "timeout": 30}}
	}
	for _, evt := range cursorHookEvents {
		hooks[evt] = makeEntry()
	}
	cfg["hooks"] = hooks

	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(hooksPath, data, 0644); err != nil {
		return fmt.Errorf("write hooks.json: %w", err)
	}
	fmt.Printf("  ✅ Cursor hook script → %s\n", scriptPath)
	fmt.Printf("  ✅ Cursor hook runtime → node=%s aipmc=%s\n", nodePath, aipmcBin)
	fmt.Printf("  ✅ Cursor hooks configured → %s (%d events)\n", hooksPath, len(cursorHookEvents))
	return nil
}

func resolveNodePath() (string, error) {
	if lp, err := exec.LookPath("node"); err == nil {
		return filepath.ToSlash(lp), nil
	}
	for _, c := range []string{
		filepath.Join(os.Getenv("ProgramFiles"), "nodejs", "node.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "nodejs", "node.exe"),
		filepath.Join(os.Getenv("LocalAppData"), "Programs", "nodejs", "node.exe"),
	} {
		if c == "" || strings.HasSuffix(c, string(filepath.Separator)) {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return filepath.ToSlash(c), nil
		}
	}
	return "", fmt.Errorf("node not found — install Node.js or add it to PATH")
}
