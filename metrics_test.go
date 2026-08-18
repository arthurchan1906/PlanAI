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

// HARNESS M1（8/18 修正）：inject_coverage 分母排除 no_summary_data。
// 原实现分母含 injNoSum → 覆盖率被稀释（metrics.go:503 注释/代码不一致）。
func TestInjectCoverageExcludesNoSummary(t *testing.T) {
	// 有 1 次 no_summary（无数据可注）不应拉低覆盖率
	rate, denom := injectCoverage(10, 0, 5, 100)
	if denom != 15 {
		t.Fatalf("denom = %d, want 15 (排除 no_summary)", denom)
	}
	if rate != 1.0 {
		t.Fatalf("rate = %v, want 1.0", rate)
	}
}

func TestInjectCoverageZeroDenom(t *testing.T) {
	rate, denom := injectCoverage(0, 0, 0, 0)
	if denom != 0 || rate != 0 {
		t.Fatalf("zero denom: rate=%v denom=%d, want 0/0", rate, denom)
	}
}

func TestInjectCoveragePartial(t *testing.T) {
	rate, denom := injectCoverage(6, 2, 2, 0)
	if denom != 10 {
		t.Fatalf("denom = %d, want 10", denom)
	}
	if rate != 1.0 { // 6+2+2 = 10/10
		t.Fatalf("rate = %v, want 1.0", rate)
	}
}
