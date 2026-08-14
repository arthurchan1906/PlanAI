package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	pmdb "aipmc/db"
)

// Regression/feature: user messages must auto-register the session's current
// status ("what is this agent working on") in agent_status, and
// ListActiveSessions must expose it joined with activity stats.
func TestAgentStatusAutoRegisterAndListSessions(t *testing.T) {
	setupDailyDB(t)
	// Bootstrap schema once (EnsureSchema runs on first Open).
	if _, err := ReadDiscussions(ReadDiscussionsOpts{LastN: 5}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	sid := "session-a"
	if _, err := LogDiscussion(sid, "user", "codex-cli", "修复 proxy token 认证", ""); err != nil {
		t.Fatalf("log user: %v", err)
	}
	if _, err := LogDiscussion(sid, "assistant", "codex-cli", "开始分析", ""); err != nil {
		t.Fatalf("log assistant: %v", err)
	}

	sessions, err := ListActiveSessions("", "codex-cli", "2026-01-01T00:00:00", 10)
	if err != nil {
		t.Fatalf("ListActiveSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 active session, got %d: %+v", len(sessions), sessions)
	}
	s := sessions[0]
	if s.SessionID != sid {
		t.Errorf("session id = %q, want %q", s.SessionID, sid)
	}
	if s.Status != "修复 proxy token 认证" {
		t.Errorf("status = %q, want the latest user prompt", s.Status)
	}
	if s.UserPromptCount != 1 || s.SubstantiveCount != 1 {
		t.Errorf("counts wrong: user=%d substantive=%d, want 1/1", s.UserPromptCount, s.SubstantiveCount)
	}
}

// Explicit status updates must win over the auto-registered prompt, and a
// stale auto-registration must not overwrite a fresher explicit status.
func TestUpdateAgentStatusExplicitWins(t *testing.T) {
	setupDailyDB(t)
	if _, err := ReadDiscussions(ReadDiscussionsOpts{LastN: 5}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	sid := "session-b"
	if _, err := LogDiscussion(sid, "user", "codex-cli", "自动登记的 prompt", ""); err != nil {
		t.Fatalf("log user: %v", err)
	}
	if err := UpdateAgentStatus("codex-cli", sid, "显式声明：正在写 L2 聚类", ""); err != nil {
		t.Fatalf("UpdateAgentStatus: %v", err)
	}
	// A newer user message must not clobber the explicit declaration.
	if _, err := LogDiscussion(sid, "user", "codex-cli", "后续 prompt", ""); err != nil {
		t.Fatalf("log second user: %v", err)
	}

	sessions, err := ListActiveSessions("", "codex-cli", "2026-01-01T00:00:00", 10)
	if err != nil {
		t.Fatalf("ListActiveSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Status != "显式声明：正在写 L2 聚类" {
		t.Errorf("explicit status lost: %+v", sessions)
	}
}

// Session filter must scope read_discussions to one peer session.
func TestReadDiscussionsSessionFilter(t *testing.T) {
	setupDailyDB(t)
	if _, err := ReadDiscussions(ReadDiscussionsOpts{LastN: 5}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	if _, err := LogDiscussion("sess-1", "user", "codex-cli", "a 在做 A", ""); err != nil {
		t.Fatalf("log sess-1: %v", err)
	}
	if _, err := LogDiscussion("sess-2", "user", "codex-cli", "b 在做 B", ""); err != nil {
		t.Fatalf("log sess-2: %v", err)
	}

	rows, err := ReadDiscussions(ReadDiscussionsOpts{Source: "codex-cli", SessionID: "sess-1", LastN: 10})
	if err != nil {
		t.Fatalf("ReadDiscussions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row for sess-1, got %d: %+v", len(rows), rows)
	}
	if rows[0]["content"] != "a 在做 A" {
		t.Errorf("wrong row content: %v", rows[0]["content"])
	}
}

// Cross-project regression (审核 #1): ListActiveSessions(project_path=...) must
// read "recent prompts" from the target project's DB, not the cwd project's.
func TestListActiveSessionsCrossProjectPrompts(t *testing.T) {
	setupDailyDB(t)
	if _, err := ReadDiscussions(ReadDiscussionsOpts{LastN: 5}); err != nil {
		t.Fatalf("bootstrap cwd: %v", err)
	}

	// Build a second "project" DB with its own discussion + status.
	proj := filepath.Join(t.TempDir(), "EncryptDrive")
	if err := os.MkdirAll(filepath.Join(proj, ".pmai", "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".pmai", "data", "pmai.db"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	d, err := pmdb.OpenProject(proj)
	if err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	d.Close()

	db, err := openOrCurrentDB(proj)
	if err != nil {
		t.Fatalf("openOrCurrentDB: %v", err)
	}
	defer db.Close()
	sid := "ed-session-1"
	if _, err := db.Exec(`INSERT INTO discussion_log (id, session_id, role, source, content, created_at) VALUES (?, ?, 'user', 'codex-cli', ?, ?)`,
		"disc-ed-1", sid, "在 EncryptDrive 修同步 bug", "2026-08-14T10:00:00"); err != nil {
		t.Fatalf("insert discussion: %v", err)
	}
	if err := UpdateAgentStatus("codex-cli", sid, "在 EncryptDrive 修同步 bug", proj); err != nil {
		t.Fatalf("UpdateAgentStatus: %v", err)
	}

	sessions, err := ListActiveSessions(proj, "codex-cli", "2026-01-01T00:00:00", 10)
	if err != nil {
		t.Fatalf("ListActiveSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session in target project, got %d: %+v", len(sessions), sessions)
	}
	if len(sessions[0].UserPrompts) != 1 || sessions[0].UserPrompts[0] != "在 EncryptDrive 修同步 bug" {
		t.Errorf("cross-project prompt read failed: %+v", sessions[0].UserPrompts)
	}
}

// CountExplicitStatuses must report how many registered sessions declared
// their status via aipm_update_status vs auto-registered user prompts.
func TestCountExplicitStatuses(t *testing.T) {
	setupDailyDB(t)
	if _, err := ReadDiscussions(ReadDiscussionsOpts{LastN: 5}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// Two auto-registered sessions (explicit=0), one explicit declaration.
	for i, sid := range []string{"auto-1", "auto-2"} {
		if _, err := LogDiscussion(sid, "user", "codex-cli", fmt.Sprintf("处理任务 %d", i), ""); err != nil {
			t.Fatalf("log user %s: %v", sid, err)
		}
	}
	if err := UpdateAgentStatus("claude-code", "explicit-1", "审核 L0/L1 代码", ""); err != nil {
		t.Fatalf("UpdateAgentStatus: %v", err)
	}

	exp, total, err := CountExplicitStatuses("")
	if err != nil {
		t.Fatalf("CountExplicitStatuses: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if exp != 1 {
		t.Errorf("explicit = %d, want 1", exp)
	}
}
