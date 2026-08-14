package store

import (
	"testing"
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
