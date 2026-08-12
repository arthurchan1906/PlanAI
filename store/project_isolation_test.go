package store

import (
	"os"
	"path/filepath"
	"testing"

	pmdb "aipmc/db"
	"aipmc/u"
)

// TestProjectIsolation verifies the *For store variants route to each
// project's own DB without relying on the process cwd — the pipeline no longer
// mutates cwd, so this isolation must come from the explicit projectPath.
func TestProjectIsolation(t *testing.T) {
	root := t.TempDir()
	projA := filepath.Join(root, "A")
	projB := filepath.Join(root, "B")
	for _, p := range []string{projA, projB} {
		if err := os.MkdirAll(filepath.Join(p, ".pmai", "data"), 0755); err != nil {
			t.Fatal(err)
		}
		// pmdb.OpenProject requires the db file to already exist.
		if err := os.WriteFile(filepath.Join(p, ".pmai", "data", "pmai.db"), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := StoreGitCommit(projA, "commit-a", "aaaa", "2026-08-12T00:00:00Z", []string{"a.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := StoreGitCommit(projB, "commit-b", "bbbb", "2026-08-12T00:00:00Z", []string{"b.go"}); err != nil {
		t.Fatal(err)
	}

	ca, err := ListCommitsFor(projA, "", "", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ca) != 1 || u.Str(ca[0]["title"]) != "commit-a" {
		t.Fatalf("projA sees %d commits (want 1 'commit-a')", len(ca))
	}

	cb, err := ListCommitsFor(projB, "", "", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(cb) != 1 || u.Str(cb[0]["title"]) != "commit-b" {
		t.Fatalf("projB sees %d commits (want 1 'commit-b')", len(cb))
	}
}

// TestProjectIsolationGraphEdges verifies graph-edge writes route to each
// project's DB via CreateGraphEdgeFor, so the multi-project pipeline no longer
// leaks file_touch/same_session edges into the home project's database.
func TestProjectIsolationGraphEdges(t *testing.T) {
	root := t.TempDir()
	projA := filepath.Join(root, "A")
	projB := filepath.Join(root, "B")
	for _, p := range []string{projA, projB} {
		if err := os.MkdirAll(filepath.Join(p, ".pmai", "data"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, ".pmai", "data", "pmai.db"), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := CreateGraphEdgeFor(projA, "session", "sess-1", "file_touch", "commit", "c-1", 0.5, nil); err != nil {
		t.Fatal(err)
	}
	// Same edge written into project B must not appear in A.
	if err := CreateGraphEdgeFor(projB, "session", "sess-1", "file_touch", "commit", "c-1", 0.9, nil); err != nil {
		t.Fatal(err)
	}

	edgesA, err := ListGraphEdgesFor(projA, "sess-1", "", "file_touch")
	if err != nil {
		t.Fatal(err)
	}
	if len(edgesA) != 1 || u.Str(edgesA[0]["target_id"]) != "c-1" {
		t.Fatalf("projA edges = %v, want exactly 1 file_touch to c-1", edgesA)
	}
	if w, ok := edgesA[0]["weight"].(float64); !ok || w != 0.5 {
		t.Errorf("projA edge weight = %v, want 0.5 (not project B's 0.9)", edgesA[0]["weight"])
	}
}

// TestProjectIsolationEntityAndDiscussion verifies EntityExistsFor and
// UpdateDiscussionSessionIDFor stay inside their own project's DB.
func TestProjectIsolationEntityAndDiscussion(t *testing.T) {
	root := t.TempDir()
	projA := filepath.Join(root, "A")
	projB := filepath.Join(root, "B")
	for _, p := range []string{projA, projB} {
		if err := os.MkdirAll(filepath.Join(p, ".pmai", "data"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, ".pmai", "data", "pmai.db"), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Commit exists only in A.
	cm, err := StoreGitCommit(projA, "commit-a", "aaaa", "2026-08-12T00:00:00Z", []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	cid := u.Str(cm["id"])
	if !EntityExistsFor(projA, "commit", cid) {
		t.Errorf("EntityExistsFor(projA) should find %s", cid)
	}
	if EntityExistsFor(projB, "commit", cid) {
		t.Errorf("EntityExistsFor(projB) must not see projA's commit %s", cid)
	}

	// Orphan discussion row in A gets its session_id backfilled only in A.
	dbA, err := pmdb.OpenProject(projA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dbA.Exec("INSERT INTO discussion_log (id, source, session_id, role, content, created_at) VALUES ('disc-1', 'codex-cli', '', 'user', 'hi', '2026-08-12T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	dbA.Close()

	if err := UpdateDiscussionSessionIDFor(projA, "disc-1", "sess-9"); err != nil {
		t.Fatal(err)
	}
	if err := UpdateDiscussionSessionIDFor(projB, "disc-1", "WRONG"); err != nil {
		t.Fatal(err)
	}

	dbA2, err := pmdb.OpenProject(projA)
	if err != nil {
		t.Fatal(err)
	}
	defer dbA2.Close()
	var sid string
	if err := dbA2.QueryRow("SELECT session_id FROM discussion_log WHERE id='disc-1'").Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if sid != "sess-9" {
		t.Errorf("projA session_id = %q, want sess-9 (update in projB must not leak)", sid)
	}
}
