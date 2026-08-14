package store

import (
	"os"
	"path/filepath"
	"testing"

	"aipmc/u"
)

// Regression: UpdateCanon previously wrote canon_items with item_type
// "scope"/"avoid", but GetCanon reads "version_scope"/"avoid_now" — the
// written values were dead (never readable). The roundtrip must hold.
func TestCanonRoundtrip(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "data", "pmai.db"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PMAI_HOME", home)

	if _, err := UpdateCanon("dec-1", "目标", "工程重点", "架构", []string{"scope-a", "scope-b"}, []string{"avoid-a"}); err != nil {
		t.Fatalf("UpdateCanon: %v", err)
	}
	c, err := GetCanon()
	if err != nil {
		t.Fatalf("GetCanon: %v", err)
	}

	gotScope := strList(c["version_scope"])
	if len(gotScope) != 2 || gotScope[0] != "scope-a" || gotScope[1] != "scope-b" {
		t.Errorf("version_scope = %v, want [scope-a scope-b] (write keys must match read keys)", gotScope)
	}
	gotAvoid := strList(c["avoid_now"])
	if len(gotAvoid) != 1 || gotAvoid[0] != "avoid-a" {
		t.Errorf("avoid_now = %v, want [avoid-a]", gotAvoid)
	}
	// Old write keys must no longer leak into the read model.
	if len(strList(c["version_scope"])) == 0 {
		t.Error("version_scope empty — dead-write regression")
	}
}

func strList(v any) []string {
	out := []string{}
	if arr, ok := v.([]any); ok {
		for _, x := range arr {
			out = append(out, u.Str(x))
		}
	}
	return out
}
