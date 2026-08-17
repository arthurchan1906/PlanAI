package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	// Relative timestamps: ListActiveSessions filters to the last 24h, so
	// hard-coded dates go stale as the clock advances. Anchor on now and
	// derive both the rows and the asserted window from the same values.
	now := time.Now().Truncate(time.Minute)
	tA1 := now.Add(-2 * time.Hour)
	tA2 := tA1.Add(1 * time.Minute)
	tB1 := now.Add(-1 * time.Hour)
	tB2 := tB1.Add(5 * time.Minute)
	fmtAt := func(t time.Time) string { return t.Format("2006-01-02T15:04:05") }
	insert(sidA, "user", "codex-cli", "修复代理 token 认证", fmtAt(tA1))
	insert(sidA, "assistant", "codex-cli", "已修复并提交", fmtAt(tA2))
	insert(sidB, "user", "claude-code", "审核 L0/L1 代码", fmtAt(tB1))
	insert(sidB, "assistant", "claude-code", "审核通过", fmtAt(tB2))

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

	// 4. Activity window with clock times (derived from the inserted rows).
	wantWindow := tA1.Format("15:04") + " ~ " + tA2.Format("15:04")
	if !strings.Contains(out, wantWindow) {
		t.Errorf("missing activity window %q:\n%s", wantWindow, out)
	}
}
