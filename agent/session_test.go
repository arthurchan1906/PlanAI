package agent

import "testing"

func TestIsValidSessionID(t *testing.T) {
	valid := []string{
		"s-20260814-101530-1a2b3c",
		"abc123",
		"s-20260101-000000-000000",
	}
	for _, id := range valid {
		if !IsValidSessionID(id) {
			t.Errorf("IsValidSessionID(%q) = false, want true", id)
		}
	}

	invalid := []string{
		"",
		"../etc/passwd",
		"..",
		".",
		"a/b",
		"a\\b",
		"..%2Fetc",
		"a b",
		"a.b",
		"/abs/path",
		"s-20260814-101530-1a2b3c.json",
		"C:\\windows\\file",
	}
	for _, id := range invalid {
		if IsValidSessionID(id) {
			t.Errorf("IsValidSessionID(%q) = true, want false", id)
		}
	}
}
