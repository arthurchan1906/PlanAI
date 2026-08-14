package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pmdb "aipmc/db"
	"aipmc/store"
)

// TestExecuteSessionsCommand verifies the &aipmc-model sessions board renders
// distinguishing info (source, short id, status, activity window) and marks
// the caller's own session.
func TestExecuteSessionsCommand(t *testing.T) {
	// Build an isolated "project" DB with two agent sessions.
	proj := filepath.Join(t.TempDir(), "DemoProj")
	if err := os.MkdirAll(filepath.Join(proj, ".pmai", "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".pmai", "data", "pmai.db"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if d, err := pmdb.OpenProject(proj); err != nil {
		t.Fatalf("OpenProject: %v", err)
	} else {
		d.Close()
	}

	db, err := pmdb.OpenProject(proj)
	if err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	defer db.Close()

	// Session A: the caller (codex, own session), with a substantive prompt.
	// Session B: another agent (claude), with status declared via UpdateAgentStatus.
	sidA := "aaa-session-a-0001"
	sidB := "bbb-session-b-0002"
	insert := func(sid, role, source, content, at string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO discussion_log (id, session_id, role, source, content, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			"disc-"+at+strings.ReplaceAll(sid, "-", ""), sid, role, source, content, at); err != nil {
			t.Fatalf("insert discussion: %v", err)
		}
	}
	insert(sidA, "user", "codex-cli", "修复代理 token 认证", "2026-08-14T10:00:00")
	insert(sidA, "assistant", "codex-cli", "已修复并提交", "2026-08-14T10:01:00")
	insert(sidB, "user", "claude-code", "审核 L0/L1 代码", "2026-08-14T11:00:00")
	insert(sidB, "assistant", "claude-code", "审核通过", "2026-08-14T11:05:00")

	if err := store.UpdateAgentStatus("claude-code", sidB, "审核 L0/L1 代码（显式声明）", proj); err != nil {
		t.Fatalf("UpdateAgentStatus: %v", err)
	}

	out := executeSessionsCommand("codex", sidA, proj)

	// 1. Board header + both sessions listed with source and short id.
	if !strings.Contains(out, "活跃 Agent 会话") {
		t.Errorf("missing board header:\n%s", out)
	}
	if !strings.Contains(out, "codex-cli") || !strings.Contains(out, "claude-code") {
		t.Errorf("missing sources:\n%s", out)
	}
	if !strings.Contains(out, "aaa-session-a") {
		t.Errorf("missing short id for session A:\n%s", out)
	}

	// 2. Caller's own session is marked.
	if !strings.Contains(out, "当前会话（你）") {
		t.Errorf("missing current-session marker:\n%s", out)
	}

	// 3. Status shown: explicit declaration for B, recent prompt for A.
	if !strings.Contains(out, "审核 L0/L1 代码") {
		t.Errorf("missing status content:\n%s", out)
	}
	if !strings.Contains(out, "修复代理 token 认证") {
		t.Errorf("missing recent-prompt fallback:\n%s", out)
	}

	// 4. Activity window with clock times.
	if !strings.Contains(out, "10:00 ~ 10:01") {
		t.Errorf("missing activity window:\n%s", out)
	}
}
