package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	pmdb "aipmc/db"
)

func setupDailyDB(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "data", "pmai.db"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PMAI_HOME", home)
}

// Regression: UpsertDaily used to read (GetDailyNote on a separate connection,
// swallowing errors) and write (INSERT OR REPLACE, swallowing errors) outside
// any transaction. Two concurrent appends could both read the empty note and
// the second write silently dropped the first goroutine's items.
func TestAppendDailyNoteConcurrentNoLostUpdate(t *testing.T) {
	setupDailyDB(t)
	date := "2026-08-14"
	// Bootstrap schema + WAL once (production DBs are initialized by aipmc init;
	// concurrent first-opens would race on the WAL journal-mode switch).
	if _, err := GetDailyNote(date); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, batch := range [][]string{{"a", "b"}, {"c", "d"}} {
		batch := batch
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := AppendDailyNote(date, map[string][]string{"completed": batch})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("AppendDailyNote: %v", err)
		}
	}

	got, err := GetDailyNote(date)
	if err != nil {
		t.Fatalf("GetDailyNote: %v", err)
	}
	items, ok := got["completed"].([]any)
	if !ok {
		t.Fatalf("completed not []any: %T", got["completed"])
	}
	if len(items) != 4 {
		t.Fatalf("completed = %v, want all 4 items (lost update)", items)
	}
	seen := map[string]bool{}
	for _, it := range items {
		seen[it.(string)] = true
	}
	for _, want := range []string{"a", "b", "c", "d"} {
		if !seen[want] {
			t.Errorf("missing %q in %v", want, items)
		}
	}
}

// Regression: GetDailyNote used to return an empty default for ANY scan error,
// which made UpsertDaily silently overwrite the note when the read failed.
// Only a missing row (sql.ErrNoRows) may map to the empty note.
func TestGetDailyNotePropagatesReadErrors(t *testing.T) {
	setupDailyDB(t)
	d, err := pmdb.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec("DROP TABLE daily_notes"); err != nil {
		t.Fatal(err)
	}
	d.Close()

	if _, err := GetDailyNote("2026-08-14"); err == nil {
		t.Fatal("expected error when table is missing, got nil")
	}
}
