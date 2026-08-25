package eval

// HARNESS §3 fixture 驱动的 M1-M5 提取器测试（8/18 写策略修订口径）：
// inject_log 实际注入即写（suppressed 0/1），same_content/no_summary 不写表。

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func mustExec(t *testing.T, d *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := d.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func fixtureDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	mustExec(t, d, `CREATE TABLE inject_log (id TEXT PRIMARY KEY, agent TEXT NOT NULL, session_id TEXT NOT NULL, req_id TEXT NOT NULL, ts TEXT NOT NULL, hash TEXT NOT NULL, source TEXT NOT NULL DEFAULT '', segments_json TEXT NOT NULL DEFAULT '{}', chars INTEGER NOT NULL DEFAULT 0, suppressed INTEGER NOT NULL DEFAULT 0)`)
	mustExec(t, d, `CREATE TABLE discussion_log (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, role TEXT NOT NULL, source TEXT NOT NULL DEFAULT '', content TEXT NOT NULL, created_at TEXT NOT NULL, metadata TEXT DEFAULT '')`)
	mustExec(t, d, `CREATE TABLE events (id TEXT PRIMARY KEY, type TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, summary TEXT NOT NULL, created_at TEXT NOT NULL, consumed_by_agent INTEGER NOT NULL DEFAULT 0, processed_by_agent INTEGER NOT NULL DEFAULT 0)`)
	return d
}

func fixtureLog(t *testing.T) string {
	t.Helper()
	// 相对日期基准（bug-20260824-101846，Claude 8/24 建议修复）：原硬编码 8/14
	// 滑出 M4 近 7 天窗口后 fixture 全部失效。基准 = 2 天前（保证窗口内）。
	base := time.Now().AddDate(0, 0, -2)
	ts := func(hh, mm int) string {
		return time.Date(base.Year(), base.Month(), base.Day(), hh, mm, 0, 0, time.Local).Format("2006-01-02 15:04:05")
	}
	lines := []string{
		"[" + ts(10, 0) + "] [INJECT] inject agent=codex-cli session=sess-A req=r100-1 source=guidelines_only",
		"[" + ts(10, 0) + "] [INJECT] agent=codex-cli session=sess-A req=r100-1 hash=abc12345 goals=1 warnings=1 actions=0 file_total=2 guidelines=1 guide_del=0 chars=412",
		"[" + ts(11, 0) + "] [INJECT] agent=codex-cli session=sess-B req=r100-2 hash=def67890 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=180",
		"[" + ts(12, 0) + "] [INJECT] agent=codex-cli session=sess-C req=r100-3 hash=abc12346 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=120",
		"[" + ts(12, 30) + "] [INJECT] agent=codex-cli session=sess-F req=r100-7 hash=abc12347 goals=0 warnings=0 actions=0 file_total=0 guidelines=1 guide_del=0 chars=90",
		"[" + ts(13, 0) + "] [INJECT] agent=codex-cli session=sess-G req=r100-8 hash=abc12348 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=100",
		"[" + ts(10, 30) + "] [INJECT] inject agent=codex-cli session=sess-D req=r100-4 source=guidelines_only",
		"[" + ts(10, 30) + "] [INJECT] skip agent=codex-cli session=sess-D req=r100-4 reason=same_content hash=abc12345",
		"[" + ts(10, 31) + "] [INJECT] skip agent=codex-cli session=sess-E req=r100-5 reason=no_summary_data",
		"[" + ts(10, 32) + "] [INJECT] suppressed=2 reason=char_limit cap=800 agent=codex-cli session=sess-C req=r100-6 segments=file_cut:1 warn:1 act:0 goals:0 guide:0",
		"[" + ts(10, 33) + "] [INJECT] inject_log write_err=SQLITE_BUSY",
	}
	// 测试进程写临时库的失败噪音（os.TempDir() 特征）不计入生产 WriteErr
	tempDB := filepath.Join(os.TempDir(), "TestInjectSameContentStillInjectsBlockX", "001", "data", "pmai.db")
	lines = append(lines, fmt.Sprintf("["+ts(10, 33)+"] [INJECT] inject_log write_err=PMAI database not found: %s — run aipmc init first", tempDB))
	p := filepath.Join(t.TempDir(), "aipmc.log")
	var data string
	for _, l := range lines {
		data += l + "\n"
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// T1/T2/T4/T5/T7：综合 fixture 覆盖 M1-M5 与 8/18 修订写策略。
func TestBuildAttributionFixture(t *testing.T) {
	d := fixtureDB(t)
	// 相对日期基准（与 fixtureLog 一致：2 天前，保证近 7 天窗口内）
	base := time.Now().AddDate(0, 0, -2)
	ts := func(hh, mm int) string {
		return time.Date(base.Year(), base.Month(), base.Day(), hh, mm, 0, 0, time.Local).Format("2006-01-02T15:04:05")
	}
	// inject_log（8/18 修订：same_content/no_summary 不写表；char_limit 写表 suppressed=1）
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-1','codex-cli','sess-A','r100-1','`+ts(10, 0)+`','abc12345','','{"fileAssoc":["src/proxy.go","src/hook.go"],"warnings":["src/proxy.go 被多 session 修改"],"actionItems":[],"goals":["修 proxy 认证"],"guidelines":true}',412,0)`)
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-2','codex-cli','sess-B','r100-2','`+ts(11, 0)+`','def67890','','{"fileAssoc":["src/api/server.go"],"warnings":[],"actionItems":[],"goals":[],"guidelines":false}',180,0)`)
	// 8/18 修订：char_limit 裁剪请求写表 suppressed=1（原 T7 不写已废弃）
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-3','codex-cli','sess-C','r100-3','`+ts(12, 0)+`','abc12346','','{"fileAssoc":["src/x.go"],"warnings":[],"actionItems":[],"goals":[],"guidelines":false}',120,1)`)
	// 无文件关联的注入不进 M2 分母（口径：注入含 ≥1 个文件）
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-4','codex-cli','sess-F','r100-7','`+ts(12, 30)+`','abc12347','','{"fileAssoc":[],"warnings":[],"actionItems":[],"goals":[],"guidelines":true}',90,0)`)
	// M3 unknown 规则：warning 无路径 → unknown
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-5','codex-cli','sess-G','r100-8','`+ts(13, 0)+`','abc12348','','{"fileAssoc":["src/y.go"],"warnings":["注意编码规范"],"actionItems":[],"goals":[],"guidelines":false}',100,0)`)

	// discussion_log：注入后工具调用
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('d1','sess-A','assistant','codex-cli','edit proxy.go','`+ts(10, 1)+`','{"file_op":{"type":"edit","rel_path":"src/proxy.go"}}')`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('d2','sess-A','assistant','codex-cli','edit other.go','`+ts(10, 2)+`','{"file_op":{"type":"edit","rel_path":"src/other.go"}}')`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('d3','sess-B','assistant','codex-cli','read server.go','`+ts(11, 5)+`','{"file_op":{"type":"read","rel_path":"src/api/server.go"}}')`)

	// events（M4）：近 7 天 30 orphan + 5 tentative_link + 1 mcp_error
	now := time.Now()
	for i := 0; i < 30; i++ {
		mustExec(t, d, `INSERT INTO events VALUES (?, 'commit_orphan','commit','c','orphan','`+ts(12, 0)+`',0,0)`, "e-orphan-"+string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	for i := 0; i < 5; i++ {
		mustExec(t, d, `INSERT INTO events VALUES (?, 'tentative_link','commit','c','link','`+ts(12, 0)+`',0,0)`, "e-link-"+string(rune('a'+i)))
	}
	mustExec(t, d, `INSERT INTO events VALUES ('e-mcp','mcp_error','tool','x','err','`+ts(12, 0)+`',0,0)`)
	// 8 天前的 orphan 必须被 M4 时间窗排除（T3）
	mustExec(t, d, `INSERT INTO events VALUES ('e-old','commit_orphan','commit','c','old','`+now.AddDate(0,0,-8).Format("2006-01-02T15:04:05")+`',0,0)`)

	logFile := fixtureLog(t)
	since := time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, time.Local)
	rep, err := BuildAttribution(d, logFile, since)
	if err != nil {
		t.Fatalf("BuildAttribution: %v", err)
	}

	a := rep.ByAgent["codex-cli"]

	// M1a 对账（T2：no_summary 不参与；:148 行 vs 表行 1:1）
	if a.M1.Injected != 5 || a.M1.LogInject != 5 || a.M1.Reconcile != 1.0 {
		t.Errorf("M1a injected=%d log_inject=%d reconcile=%v, want 5/5/1.0",
			a.M1.Injected, a.M1.LogInject, a.M1.Reconcile)
	}
	if a.M1.SameContent != 1 {
		t.Errorf("M1 same_content = %d, want 1", a.M1.SameContent)
	}
	if a.M1.NoSummary != 1 {
		t.Errorf("M1 no_summary = %d, want 1（报告但排除）", a.M1.NoSummary)
	}
	// M1b 新鲜度 = injected/(injected+same_content)
	if a.M1.Freshness != 5.0/6.0 {
		t.Errorf("M1 freshness = %v, want %v", a.M1.Freshness, 5.0/6.0)
	}
	// 直接证据告警：write_err 计入报告顶层
	if rep.WriteErr != 1 {
		t.Errorf("WriteErr = %d, want 1（write_err 日志行）", rep.WriteErr)
	}

	// M2（T4：分层不合并；8/18 partial 新分层）
	// full 组：sess-A（proxy.go 命中）、sess-B（server.go 命中）、sess-G（无调用不命中）
	if a.M2.FullInject.Sessions != 3 || a.M2.FullInject.HitSessions != 2 {
		t.Errorf("M2 full_inject = %+v, want 3/2", a.M2.FullInject)
	}
	if a.M2.PartialInject.Sessions != 1 || a.M2.PartialInject.HitSessions != 0 {
		t.Errorf("M2 partial_inject = %+v, want 1/0（sess-C 无调用）", a.M2.PartialInject)
	}
	if a.M2.SameContentCtl.Sessions != 1 || a.M2.SameContentCtl.HitSessions != 0 {
		t.Errorf("M2 same_content_ctl = %+v, want 1/0", a.M2.SameContentCtl)
	}
	if a.M2.NoSummaryCtl.Sessions != 1 || a.M2.NoSummaryCtl.HitSessions != 0 {
		t.Errorf("M2 no_summary_ctl = %+v, want 1/0", a.M2.NoSummaryCtl)
	}
	// 无文件关联的 sess-F 不进 M2 分母（口径：注入含 ≥1 个文件）

	// M3（T5：可映射 1 条 proxy.go → sess-A edit → 未回避；unknown 1 条）
	if rep.M3.Mapped != 1 {
		t.Errorf("M3 mapped = %d, want 1", rep.M3.Mapped)
	}
	if rep.M3.Avoided != 0 {
		t.Errorf("M3 avoided = %d, want 0（sess-A 对 proxy.go 发生了 edit）", rep.M3.Avoided)
	}
	if rep.M3.Unknown != 1 {
		t.Errorf("M3 unknown = %d, want 1（无路径 warning）", rep.M3.Unknown)
	}

	// M4（T3：近 7 天窗口，8 天前 orphan 排除）
	if rep.M4.Total != 36 {
		t.Errorf("M4 total = %d, want 36（含 5 条 tentative_link，排除 8 天前 orphan）", rep.M4.Total)
	}
	if rep.M4.Noise != 31 {
		t.Errorf("M4 noise = %d, want 31（30 orphan + 1 mcp_error）", rep.M4.Noise)
	}
	wantRatio := 31.0 / 36.0
	if rep.M4.NoiseRatio != wantRatio {
		t.Errorf("M4 noise_ratio = %v, want %v", rep.M4.NoiseRatio, wantRatio)
	}

	// M5：:153 行 segments=file_cut:1 warn:1 act:0 goals:0 guide:0
	if rep.M5.SuppressedRequests != 1 {
		t.Errorf("M5 suppressed_requests = %d, want 1", rep.M5.SuppressedRequests)
	}
	if rep.M5.Segments.FileAssoc != 1 || rep.M5.Segments.Warnings != 1 {
		t.Errorf("M5 segments = %+v, want file_cut=1 warn=1", rep.M5.Segments)
	}
}

// T7 写策略：same_content/no_summary 不写 inject_log（对照组从日志侧重建）。
func TestAttributionWriteStrategy(t *testing.T) {
	d := fixtureDB(t)
	// 纯 skip 日志（无 :148 注入行）：模拟无任何注入的窗口
	logFile := filepath.Join(t.TempDir(), "aipmc.log")
	lines := "[2026-08-14 10:30:00] [INJECT] skip agent=codex-cli session=sess-D req=r100-4 reason=same_content hash=abc12345\n" +
		"[2026-08-14 10:31:00] [INJECT] skip agent=codex-cli session=sess-E req=r100-5 reason=no_summary_data\n"
	if err := os.WriteFile(logFile, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	since, _ := time.Parse("2006-01-02T15:04:05", "2026-08-14T00:00:00")
	rep, err := BuildAttribution(d, logFile, since)
	if err != nil {
		t.Fatalf("BuildAttribution: %v", err)
	}
	// 无 inject_log 行 → injected=0，但 same_content/no_summary 从日志侧计入
	a := rep.ByAgent["codex-cli"]
	if a.M1.Injected != 0 || a.M1.LogInject != 0 || a.M1.SameContent != 1 || a.M1.NoSummary != 1 {
		t.Errorf("M1 without table rows: injected=%d log_inject=%d same=%d nosum=%d, want 0/0/1/1",
			a.M1.Injected, a.M1.LogInject, a.M1.SameContent, a.M1.NoSummary)
	}
	if a.M1.Reconcile != 0 || a.M1.Freshness != 0 {
		t.Errorf("M1 reconcile=%v freshness=%v, want 0/0（无表行）", a.M1.Reconcile, a.M1.Freshness)
	}
}

// 对账失败（write_err 导致表行缺失）：M1a <1.0 应暴露观测断裂。
func TestM1ReconcileDetectsWriteLoss(t *testing.T) {
	d := fixtureDB(t)
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-1','codex-cli','sess-A','r100-1','2026-08-14T10:00:00','abc12345','','{"fileAssoc":["a.go"]}',100,0)`)
	// 日志有 2 条 :148 注入行，但表只有 1 行（1 次写库失败）
	p := filepath.Join(t.TempDir(), "aipmc.log")
	lines := "[2026-08-14 10:00:00] [INJECT] agent=codex-cli session=sess-A req=r100-1 hash=abc12345 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=100\n" +
		"[2026-08-14 10:05:00] [INJECT] agent=codex-cli session=sess-B req=r100-2 hash=abc12346 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=100\n" +
		"[2026-08-14 10:06:00] [INJECT] inject_log write_err=SQLITE_BUSY\n"
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	since, _ := time.Parse("2006-01-02T15:04:05", "2026-08-14T00:00:00")
	rep, err := BuildAttribution(d, p, since)
	if err != nil {
		t.Fatalf("BuildAttribution: %v", err)
	}
	a := rep.ByAgent["codex-cli"]
	if a.M1.LogInject != 2 || a.M1.Injected != 1 {
		t.Fatalf("log_inject=%d injected=%d, want 2/1", a.M1.LogInject, a.M1.Injected)
	}
	if a.M1.Reconcile != 0.5 {
		t.Errorf("reconcile = %v, want 0.5（观测断裂暴露）", a.M1.Reconcile)
	}
	if rep.WriteErr != 1 {
		t.Errorf("WriteErr = %d, want 1", rep.WriteErr)
	}
}

// M1a 对账窗口：inject_log 启用前的历史 :148 日志行不参与对账（无对应表行，
// 计入分母会造成系统性误报——8/18 实测 claude reconcile=0.005 根因）。
func TestM1ReconcileWindowExcludesPreEnableLogs(t *testing.T) {
	d := fixtureDB(t)
	// inject_log 最早行 = 10:00（观测层启用时间）
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-1','codex-cli','sess-A','r100-1','2026-08-14T10:00:00','abc12345','','{"fileAssoc":["a.go"]}',100,0)`)
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-2','codex-cli','sess-B','r100-2','2026-08-14T10:05:00','abc12346','','{"fileAssoc":["b.go"]}',100,0)`)
	// 日志 3 条 :148 注入行：09:00 在观测层启用前，10:00/10:05 启用后
	p := filepath.Join(t.TempDir(), "aipmc.log")
	lines := "[2026-08-14 09:00:00] [INJECT] agent=codex-cli session=sess-PRE req=r100-0 hash=abc12344 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=100\n" +
		"[2026-08-14 10:00:00] [INJECT] agent=codex-cli session=sess-A req=r100-1 hash=abc12345 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=100\n" +
		"[2026-08-14 10:05:00] [INJECT] agent=codex-cli session=sess-B req=r100-2 hash=abc12346 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=100\n"
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	since, _ := time.Parse("2006-01-02T15:04:05", "2026-08-14T00:00:00")
	rep, err := BuildAttribution(d, p, since)
	if err != nil {
		t.Fatalf("BuildAttribution: %v", err)
	}
	a := rep.ByAgent["codex-cli"]
	if a.M1.LogInject != 2 || a.M1.Injected != 2 {
		t.Fatalf("log_inject=%d injected=%d, want 2/2（启用前 09:00 行排除）", a.M1.LogInject, a.M1.Injected)
	}
	if a.M1.Reconcile != 1.0 {
		t.Errorf("reconcile = %v, want 1.0（启用前行排除后无观测断裂）", a.M1.Reconcile)
	}
}

// S4 核验项 4：人类可读输出（对齐 metrics.go printRow 风格）覆盖关键指标。
func TestFormatHumanCoversKeyMetrics(t *testing.T) {
	d := fixtureDB(t)
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-1','codex-cli','sess-A','r100-1','2026-08-14T10:00:00','abc12345','','{"fileAssoc":["a.go"]}',100,0)`)
	p := filepath.Join(t.TempDir(), "aipmc.log")
	lines := "[2026-08-14 10:00:00] [INJECT] agent=codex-cli session=sess-A req=r100-1 hash=abc12345 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=100\n"
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	since, _ := time.Parse("2006-01-02T15:04:05", "2026-08-14T00:00:00")
	rep, err := BuildAttribution(d, p, since)
	if err != nil {
		t.Fatalf("BuildAttribution: %v", err)
	}
	human := FormatHuman(rep)
	for _, want := range []string{"M1a", "M1b", "M2", "M3", "M4", "M5", "write_err", "codex-cli"} {
		if !strings.Contains(human, want) {
			t.Errorf("FormatHuman missing %q:\n%s", want, human)
		}
	}
}

// M2 注入组修正（8/18 攻击性审核问题 2）：多次注入不同文件集时，
// 早期注入文件仍计入命中（旧实现仅取末次注入文件集，漏早期注入）。
func TestM2EarlyInjectFilesStillCount(t *testing.T) {
	d := fixtureDB(t)
	// sess-A 两次注入：10:00 注入 a.go，11:00 注入 b.go；11:01 引用了 a.go（早期注入文件）
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-1','codex-cli','sess-A','r100-1','2026-08-14T10:00:00','h1','','{"fileAssoc":["a.go"]}',100,0)`)
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-2','codex-cli','sess-A','r100-2','2026-08-14T11:00:00','h2','','{"fileAssoc":["b.go"]}',100,0)`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('d1','sess-A','assistant','codex-cli','edit a.go','2026-08-14T11:01:00','{"file_op":{"type":"edit","rel_path":"a.go"}}')`)
	p := filepath.Join(t.TempDir(), "aipmc.log")
	lines := "[2026-08-14 10:00:00] [INJECT] agent=codex-cli session=sess-A req=r100-1 hash=h1 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=100\n" +
		"[2026-08-14 11:00:00] [INJECT] agent=codex-cli session=sess-A req=r100-2 hash=h2 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=100\n"
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	since, _ := time.Parse("2006-01-02T15:04:05", "2026-08-14T00:00:00")
	rep, err := BuildAttribution(d, p, since)
	if err != nil {
		t.Fatalf("BuildAttribution: %v", err)
	}
	a := rep.ByAgent["codex-cli"]
	if a.M2.FullInject.Sessions != 1 || a.M2.FullInject.HitSessions != 1 {
		t.Errorf("M2 full_inject = %+v, want 1/1（早期注入 a.go 的引用应计入命中）", a.M2.FullInject)
	}
}

// M2 对照组修正（8/18 攻击性审核问题 1）：same_content 对照组有注入历史时，
// 命中按「已注入过的文件」判定（基线=已见过），而非任意文件引用（活跃基线）。
func TestM2SameContentCtlUsesInjectHistory(t *testing.T) {
	d := fixtureDB(t)
	// sess-A 注入过 a.go（已见过）；10:30 same_content 抑制；10:31 引用了 b.go（非注入文件）
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-1','codex-cli','sess-A','r100-1','2026-08-14T10:00:00','h1','','{"fileAssoc":["a.go"]}',100,0)`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('d1','sess-A','assistant','codex-cli','edit b.go','2026-08-14T10:31:00','{"file_op":{"type":"edit","rel_path":"b.go"}}')`)
	p := filepath.Join(t.TempDir(), "aipmc.log")
	lines := "[2026-08-14 10:00:00] [INJECT] agent=codex-cli session=sess-A req=r100-1 hash=h1 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=100\n" +
		"[2026-08-14 10:30:00] [INJECT] skip agent=codex-cli session=sess-A req=r100-2 reason=same_content hash=h1\n"
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	since, _ := time.Parse("2006-01-02T15:04:05", "2026-08-14T00:00:00")
	rep, err := BuildAttribution(d, p, since)
	if err != nil {
		t.Fatalf("BuildAttribution: %v", err)
	}
	a := rep.ByAgent["codex-cli"]
	if a.M2.SameContentCtl.Sessions != 1 || a.M2.SameContentCtl.HitSessions != 0 {
		t.Errorf("M2 same_content_ctl = %+v, want 1/0（引用非注入文件 b.go 不应命中——对照组基线=已注入文件）", a.M2.SameContentCtl)
	}
	// 注记必须包含准实验说明（HARNESS §2 强制项）
	joined := strings.Join(rep.Annotations, " ")
	if !strings.Contains(joined, "准实验") {
		t.Errorf("Annotations 缺准实验注记: %v", rep.Annotations)
	}
}

// M2/M3 多格式兼容（8/18 Claude 审核 + codex 实测，P0）：生产 post_tool（codex/cursor）
// 无 file_op 键，写操作识别必须兼容 ① post_tool 平铺（rel_path/file_path + tool_name）
// ② file_op 嵌套旧格式 ③ 顶层 type 格式；read/bash/mcp 不得误判。
func TestWriteOpPathsMultiFormat(t *testing.T) {
	cases := []struct {
		name string
		md   string
		want []string
	}{
		{"codex post_tool Edit 顶层 rel_path", `{"_type":"post_tool","rel_path":"src/app.go","tool_name":"Edit","tool_input":{}}`, []string{"src/app.go"}},
		{"codex post_tool Edit 顶层 file_path", `{"_type":"post_tool","file_path":"/repo/a.go","tool_name":"Edit","tool_input":{"file_path":"/repo/a.go"}}`, []string{"/repo/a.go"}},
		{"cursor post_tool 无 tool_name tool_input.file_path", `{"_type":"post_tool","file_path":"/repo/b.go","hook_event_name":"postToolUse","tool_input":{"file_path":"/repo/b.go"}}`, []string{"/repo/b.go"}},
		{"cursor Write 工具", `{"_type":"post_tool","file_path":"c.go","hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{"file_path":"c.go"}}`, []string{"c.go"}},
		{"codex Create 工具", `{"_type":"post_tool","rel_path":"new.go","tool_name":"Create","tool_input":{}}`, []string{"new.go"}},
		{"file_op 嵌套旧格式", `{"file_op":{"type":"edit","rel_path":"src/old.go"}}`, []string{"src/old.go"}},
		{"顶层 type 格式", `{"type":"edit","file_path":"d.go"}`, []string{"d.go"}},
		{"read 工具不算写", `{"_type":"post_tool","rel_path":"r.go","tool_name":"Read","tool_input":{}}`, nil},
		{"bash 不算写", `{"_type":"post_tool","tool_name":"Bash","tool_input":{"command":"ls"}}`, nil},
		{"aipm mcp 不算写", `{"_type":"post_tool","tool_name":"mcp__aipm__aipm_list_tasks","tool_input":{}}`, nil},
	}
	for _, c := range cases {
		got := writeOpPaths(c.md)
		if len(got) != len(c.want) {
			t.Errorf("%s: writeOpPaths = %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: writeOpPaths[%d] = %q, want %q", c.name, i, got[i], c.want[i])
			}
		}
	}
}

// codex 写操作经 Bash 执行 apply_patch/sed -i/重定向（hook 已打标 source=bash_heuristic +
// type=edit 等写类型；read/stage/unverified 不得误判）——8/25 Claude 审核优化：
// 消费 hook 已有标记而非自写正则。
func TestWriteOpPathsReadsBashHeuristic(t *testing.T) {
	cases := []struct {
		name string
		md   string
		want []string
	}{
		{"bash_heuristic edit 单文件", `{"_type":"post_tool","tool_name":"Bash","source":"bash_heuristic","type":"edit","file_path":"eval/attribution.go","rel_path":"eval/attribution.go","tool_input":{"command":"apply_patch <<'PATCH'..."}}`, []string{"eval/attribution.go"}},
		{"bash_heuristic edit 多文件 rel_paths", `{"_type":"post_tool","tool_name":"Bash","source":"bash_heuristic","type":"edit","file_path":"main.go","rel_path":"main.go","rel_paths":["main.go","project/packets.go"],"tool_input":{"command":"apply_patch ..."}}`, []string{"main.go", "project/packets.go"}},
		{"bash_heuristic read 不算写", `{"_type":"post_tool","tool_name":"Bash","source":"bash_heuristic","type":"read","rel_path":"hook/bashpaths.go","tool_input":{"command":"sed -n '1,10p' hook/bashpaths.go"}}`, nil},
		{"bash_heuristic stage 不算写", `{"_type":"post_tool","tool_name":"Bash","source":"bash_heuristic","type":"stage","rel_path":"a.go","tool_input":{"command":"git add a.go"}}`, nil},
		{"bash_heuristic_unverified 排除", `{"_type":"post_tool","tool_name":"Bash","source":"bash_heuristic_unverified","type":"edit","rel_path":"a.go","tool_input":{"command":"apply_patch ..."}}`, nil},
		{"纯 bash 无打标不算写", `{"_type":"post_tool","tool_name":"Bash","tool_input":{"command":"rg foo"}}`, nil},
	}
	for _, c := range cases {
		got := writeOpPaths(c.md)
		if len(got) != len(c.want) {
			t.Errorf("%s: writeOpPaths = %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: writeOpPaths[%d] = %q, want %q", c.name, i, got[i], c.want[i])
			}
		}
	}
}

// M2 生产 post_tool 格式（codex/cursor 无 file_op 键）必须计入命中（P0 修复，原丢 90%）。
func TestM2PostToolFormatCounts(t *testing.T) {
	d := fixtureDB(t)
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-1','codex-cli','sess-A','r100-1','2026-08-14T10:00:00','h1','','{"fileAssoc":["a.go"]}',100,0)`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('d1','sess-A','assistant','codex-cli','edit a.go','2026-08-14T11:01:00','{"_type":"post_tool","rel_path":"a.go","hook_event_name":"PostToolUse","tool_name":"Edit","tool_input":{}}')`)
	p := filepath.Join(t.TempDir(), "aipmc.log")
	lines := "[2026-08-14 10:00:00] [INJECT] agent=codex-cli session=sess-A req=r100-1 hash=h1 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=100\n"
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	since, _ := time.Parse("2006-01-02T15:04:05", "2026-08-14T00:00:00")
	rep, err := BuildAttribution(d, p, since)
	if err != nil {
		t.Fatalf("BuildAttribution: %v", err)
	}
	a := rep.ByAgent["codex-cli"]
	if a.M2.FullInject.Sessions != 1 || a.M2.FullInject.HitSessions != 1 {
		t.Errorf("M2 full_inject = %+v, want 1/1（post_tool Edit a.go 应命中注入文件 a.go）", a.M2.FullInject)
	}
}

// M3 生产 post_tool 格式：warning 指向 a.go，post_tool Edit a.go → 未回避（原 LIMIT 5 +
// file_op 嵌套假设导致恒 false → 回避率虚高，P0 修复）。
func TestM3PostToolFormatNotAvoided(t *testing.T) {
	d := fixtureDB(t)
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-1','codex-cli','sess-A','r100-1','2026-08-14T10:00:00','h1','','{"fileAssoc":["a.go"],"warnings":["a.go 被多 session 修改"]}',100,0)`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('d1','sess-A','assistant','codex-cli','edit a.go','2026-08-14T10:01:00','{"_type":"post_tool","rel_path":"a.go","hook_event_name":"postToolUse","tool_name":"Edit","tool_input":{}}')`)
	p := filepath.Join(t.TempDir(), "aipmc.log")
	lines := "[2026-08-14 10:00:00] [INJECT] agent=codex-cli session=sess-A req=r100-1 hash=h1 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=100\n"
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	since, _ := time.Parse("2006-01-02T15:04:05", "2026-08-14T00:00:00")
	rep, err := BuildAttribution(d, p, since)
	if err != nil {
		t.Fatalf("BuildAttribution: %v", err)
	}
	if rep.M3.Mapped != 1 {
		t.Errorf("M3 mapped = %d, want 1", rep.M3.Mapped)
	}
	if rep.M3.Avoided != 0 {
		t.Errorf("M3 avoided = %d, want 0（post_tool Edit a.go 已写，不应回避）", rep.M3.Avoided)
	}
}

// M3 窗口语义（8/25 Claude 审核 C4）：窗口 = 注入 ts 至该 session 下一次注入 ts。
// 下一次注入之后对同一路径的写，不属于前一个 warning 的窗口 → 不改变回避判定。
func TestM3WindowExcludesWriteAfterNextInject(t *testing.T) {
	d := fixtureDB(t)
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-1','codex-cli','sess-A','r100-1','2026-08-14T10:00:00','h1','','{"fileAssoc":["a.go"],"warnings":["a.go 被多 session 修改"]}',100,0)`)
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-2','codex-cli','sess-A','r100-2','2026-08-14T10:30:00','h2','','{"fileAssoc":["b.go"],"warnings":[]}',100,0)`)
	// 10:40 的写发生在第二次注入之后 → 10:00 warning 窗口内无写 → 应回避
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('d1','sess-A','assistant','codex-cli','edit a.go','2026-08-14T10:40:00','{"_type":"post_tool","rel_path":"a.go","tool_name":"Edit","tool_input":{}}')`)
	p := filepath.Join(t.TempDir(), "aipmc.log")
	lines := "[2026-08-14 10:00:00] [INJECT] agent=codex-cli session=sess-A req=r100-1 hash=h1 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=100\n" +
		"[2026-08-14 10:30:00] [INJECT] agent=codex-cli session=sess-A req=r100-2 hash=h2 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=100\n"
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	since, _ := time.Parse("2006-01-02T15:04:05", "2026-08-14T00:00:00")
	rep, err := BuildAttribution(d, p, since)
	if err != nil {
		t.Fatalf("BuildAttribution: %v", err)
	}
	if rep.M3.Mapped != 1 || rep.M3.Avoided != 1 {
		t.Errorf("M3 mapped/avoided = %d/%d, want 1/1（10:00 warning 窗口至 10:30 截止，10:40 的写不算）", rep.M3.Mapped, rep.M3.Avoided)
	}
}

// D1 回归（8/25 Claude D1）：生产 fileAssoc 是注记串格式（"a.go → task-xxx (done, P0)"），
// 必须拆路径后与 post_tool 写操作匹配——旧精确查找恒 miss（真实库 241/241 注记串，M2 恒 0%）。
func TestM2AnnotationStringFileAssocMatches(t *testing.T) {
	d := fixtureDB(t)
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-1','codex-cli','sess-A','r100-1','2026-08-14T10:00:00','h1','','{"fileAssoc":["a.go → task-20260814-093315-284a5a (done, P0) task-20260814-093315-284a5a"],"warnings":null}',100,0)`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('d1','sess-A','assistant','codex-cli','edit a.go','2026-08-14T11:01:00','{"_type":"post_tool","rel_path":"a.go","hook_event_name":"PostToolUse","tool_name":"Edit","tool_input":{}}')`)
	p := filepath.Join(t.TempDir(), "aipmc.log")
	lines := "[2026-08-14 10:00:00] [INJECT] agent=codex-cli session=sess-A req=r100-1 hash=h1 goals=0 warnings=0 actions=0 file_total=1 guidelines=0 guide_del=0 chars=100\n"
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	since, _ := time.Parse("2006-01-02T15:04:05", "2026-08-14T00:00:00")
	rep, err := BuildAttribution(d, p, since)
	if err != nil {
		t.Fatalf("BuildAttribution: %v", err)
	}
	a := rep.ByAgent["codex-cli"]
	if a.M2.FullInject.Sessions != 1 || a.M2.FullInject.HitSessions != 1 {
		t.Errorf("M2 full_inject = %+v, want 1/1（注记串 fileAssoc 拆路径后应命中）", a.M2.FullInject)
	}
}

// D2 回归（8/25 Claude D2）：生产注入端路径风险提示在 actionItems（warnings 恒空），
// M3 数据源须并入 actionItems——否则分母恒空（真实库 274 行 warnings 0 非空）。
func TestM3ActionItemsAsWarningSource(t *testing.T) {
	d := fixtureDB(t)
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-1','codex-cli','sess-A','r100-1','2026-08-14T10:00:00','h1','','{"fileAssoc":null,"warnings":null,"actionItems":["⚠️ 8 个文件被多 session 修改：a.go, b.go\n  → aipm_create_task 为最活跃文件建 task"]}',100,0)`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('d1','sess-A','assistant','codex-cli','edit a.go','2026-08-14T10:01:00','{"_type":"post_tool","rel_path":"a.go","tool_name":"Edit","tool_input":{}}')`)
	p := filepath.Join(t.TempDir(), "aipmc.log")
	lines := "[2026-08-14 10:00:00] [INJECT] agent=codex-cli session=sess-A req=r100-1 hash=h1 goals=0 warnings=0 actions=0 file_total=0 guidelines=0 guide_del=0 chars=100\n"
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	since, _ := time.Parse("2006-01-02T15:04:05", "2026-08-14T00:00:00")
	rep, err := BuildAttribution(d, p, since)
	if err != nil {
		t.Fatalf("BuildAttribution: %v", err)
	}
	if rep.M3.Mapped != 1 {
		t.Errorf("M3 mapped = %d, want 1（actionItems 路径提示应计入可映射）", rep.M3.Mapped)
	}
	if rep.M3.Avoided != 0 {
		t.Errorf("M3 avoided = %d, want 0（post_tool Edit a.go 已写，不应回避）", rep.M3.Avoided)
	}
}

// E1 回归（8/25 Claude E1）：警告提取为 basename（pathInWarningRe 从 "⚠️ ... discussion_test.go, ..."
// 提取 "discussion_test.go"），真实写操作带目录（"discussion/discussion_test.go"）——
// sessionWrotePath 精确匹配恒 miss → 回避率恒 100%（假象）。basename 归一化后应命中。
func TestM3BasenameWarningMatchesDirWrite(t *testing.T) {
	d := fixtureDB(t)
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-1','codex-cli','sess-A','r100-1','2026-08-14T10:00:00','h1','','{"fileAssoc":null,"warnings":null,"actionItems":["⚠️ 8 个文件被多 session 修改：discussion_test.go, store_test.go\n  → aipm_create_task 为最活跃文件建 task"]}',100,0)`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('d1','sess-A','assistant','codex-cli','edit discussion_test.go','2026-08-14T10:01:00','{"_type":"post_tool","file_path":"discussion/discussion_test.go","tool_name":"Edit","tool_input":{}}')`)
	p := filepath.Join(t.TempDir(), "aipmc.log")
	lines := "[2026-08-14 10:00:00] [INJECT] agent=codex-cli session=sess-A req=r100-1 hash=h1 goals=0 warnings=0 actions=0 file_total=0 guidelines=0 guide_del=0 chars=100\n"
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	since, _ := time.Parse("2006-01-02T15:04:05", "2026-08-14T00:00:00")
	rep, err := BuildAttribution(d, p, since)
	if err != nil {
		t.Fatalf("BuildAttribution: %v", err)
	}
	if rep.M3.Mapped != 1 {
		t.Errorf("M3 mapped = %d, want 1", rep.M3.Mapped)
	}
	if rep.M3.Avoided != 0 {
		t.Errorf("M3 avoided = %d, want 0（basename 警告 vs 带目录写路径应命中，回避率恒 1 是匹配断链假象）", rep.M3.Avoided)
	}
}
