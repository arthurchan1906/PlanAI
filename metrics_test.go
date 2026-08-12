package main

import "testing"

func TestParseLogTimestamp(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		wantDate bool
		wantOK   bool
	}{
		{"new format with date", "[2026-08-12 10:05:01] [MCP] tool=x", true, true},
		{"old format no date", "[15:04:05] [LLM] agent=x", false, true},
		{"garbage timestamp", "[not-a-time] [LLM] agent=x", false, false},
		{"no bracket", "plain line", false, false},
		{"empty line", "", false, false},
	}
	for _, c := range cases {
		ts, hasDate, ok := parseLogTimestamp(c.line)
		if ok != c.wantOK {
			t.Errorf("%s: ok = %v, want %v", c.name, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if hasDate != c.wantDate {
			t.Errorf("%s: hasDate = %v, want %v (ts=%v)", c.name, hasDate, c.wantDate, ts)
		}
		if c.wantDate && ts.Year() != 2026 {
			t.Errorf("%s: ts year = %d, want 2026", c.name, ts.Year())
		}
	}
	if ts, _, ok := parseLogTimestamp("[2026-08-12 10:05:01] [MCP] tool=x"); !ok || ts.Format("2006-01-02 15:04:05") != "2026-08-12 10:05:01" {
		t.Errorf("new-format parse mismatch: %v %v", ts, ok)
	}
	if _, hasDate, ok := parseLogTimestamp("[15:04:05] x"); !ok || hasDate {
		t.Errorf("old-format should parse ok with hasDate=false, got ok=%v hasDate=%v", ok, hasDate)
	}
}
