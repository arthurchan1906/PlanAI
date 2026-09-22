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

// 8/28：日志类 --since——把绝对时间解析为本地时间作日志扫描下界（E 线复测前置）。
func TestParseAbsoluteSince(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // 期望格式"2006-01-02 15:04:05"；空=期望解析失败
	}{
		{"ISO-T", "2026-08-28T13:01:00", "2026-08-28 13:01:00"},
		{"space", "2026-08-28 13:01:00", "2026-08-28 13:01:00"},
		{"date-only", "2026-08-28", "2026-08-28 00:00:00"},
		{"garbage", "not-a-time", ""},
		{"all", "all", ""},
	}
	for _, c := range cases {
		ts, ok := parseAbsoluteSince(c.in)
		if c.want == "" {
			if ok {
				t.Errorf("%s: expected parse fail, got %v", c.name, ts)
			}
			continue
		}
		if !ok {
			t.Errorf("%s: expected parse ok for %q", c.name, c.in)
			continue
		}
		if ts.Format("2006-01-02 15:04:05") != c.want {
			t.Errorf("%s: got %v want %v", c.name, ts.Format("2006-01-02 15:04:05"), c.want)
		}
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

// C3 回归（bug-20260922-102406-70f863）：分母必须是实际注入数，不含 same_content 去重跳过。
// 旧公式 supChar/(supTotal+skipTotal) 会把「每次注入都被 800 字符硬裁剪」稀释成达标绿灯。
func TestSuppressedRateExcludesDedupSkips(t *testing.T) {
	// 2 次注入全部被裁剪 + 10 次去重跳过 → 真实 100%；旧公式 2/(2+10) ≈ 16.7%。
	if got := suppressedRate(2, 2); got != 1.0 {
		t.Errorf("suppressedRate(2,2) = %v, want 1.0", got)
	}
	if old := 2.0 / float64(2+10); old >= 0.30 {
		t.Fatalf("前提失效：旧公式 %.3f 不再呈现为达标，回归价值消失", old)
	}
	// 线上真实数据（2026-09-22 实测）：5926 次注入，5926 次被裁剪。
	// 旧公式 5926/21403 = 27.7% ✅；新公式 100.0% ❌。
	if got := suppressedRate(5926, 5926); got != 1.0 {
		t.Errorf("suppressedRate(5926,5926) = %v, want 1.0", got)
	}
}

func TestSuppressedRatePartialAndZeroDenom(t *testing.T) {
	if got := suppressedRate(3, 4); got != 0.75 {
		t.Errorf("suppressedRate(3,4) = %v, want 0.75", got)
	}
	if got := suppressedRate(0, 0); got != 0 {
		t.Errorf("suppressedRate(0,0) = %v, want 0（零分母不得 NaN）", got)
	}
	if got := suppressedRate(5, 0); got != 0 {
		t.Errorf("suppressedRate(5,0) = %v, want 0", got)
	}
}

// C3 语义分界：8/18 起 suppressed=reason=char_limit 才只在实际注入后产出；
// 更早的旧实现把未注入请求的抑制也算进来（8/12-8/14 段实测 suppressed>注入），
// 故旧行不得计入 C3 区间。
func TestInC3EraBoundary(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"[2026-08-17 23:59:59] [INJECT] suppressed=1 reason=char_limit cap=800", false},
		{"[2026-08-18 00:00:00] [INJECT] suppressed=1 reason=char_limit cap=800", true},
		{"[2026-09-22 10:10:20] [INJECT] suppressed=9 reason=char_limit cap=800", true},
		{"[16:38:44] [INJECT] suppressed=1 reason=char_limit cap=800", false},
		{"[2026-08-12 16:06:27] [INJECT] agent=codex goals=3 chars=707", false},
	}
	for _, c := range cases {
		if got := inC3Era(c.line); got != c.want {
			t.Errorf("inC3Era(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}
