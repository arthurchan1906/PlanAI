package eval

// HARNESS §3 fixture 驱动的 M1-M5 提取器测试（8/18 写策略修订口径）：
// inject_log 实际注入即写（suppressed 0/1），same_content/no_summary 不写表。

import (
	"database/sql"
	"os"
	"path/filepath"
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
	lines := []string{
		"[2026-08-14 10:30:00] [INJECT] skip agent=codex-cli session=sess-D req=r100-4 reason=same_content hash=abc12345",
		"[2026-08-14 10:31:00] [INJECT] skip agent=codex-cli session=sess-E req=r100-5 reason=no_summary_data",
		"[2026-08-14 10:32:00] [INJECT] suppressed=2 reason=char_limit cap=800 agent=codex-cli session=sess-C req=r100-6 segments=file_cut:1 warn:1 act:0 goals:0 guide:0",
	}
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
	// inject_log（8/18 修订：same_content/no_summary 不写表；char_limit 写表 suppressed=1）
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-1','codex-cli','sess-A','r100-1','2026-08-14T10:00:00','abc12345','','{"fileAssoc":["src/proxy.go","src/hook.go"],"warnings":["src/proxy.go 被多 session 修改"],"actionItems":[],"goals":["修 proxy 认证"],"guidelines":true}',412,0)`)
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-2','codex-cli','sess-B','r100-2','2026-08-14T11:00:00','def67890','','{"fileAssoc":["src/api/server.go"],"warnings":[],"actionItems":[],"goals":[],"guidelines":false}',180,0)`)
	// 8/18 修订：char_limit 裁剪请求写表 suppressed=1（原 T7 不写已废弃）
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-3','codex-cli','sess-C','r100-3','2026-08-14T12:00:00','abc12346','','{"fileAssoc":["src/x.go"],"warnings":[],"actionItems":[],"goals":[],"guidelines":false}',120,1)`)
	// 无文件关联的注入不进 M2 分母（口径：注入含 ≥1 个文件）
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-4','codex-cli','sess-F','r100-7','2026-08-14T12:30:00','abc12347','','{"fileAssoc":[],"warnings":[],"actionItems":[],"goals":[],"guidelines":true}',90,0)`)
	// M3 unknown 规则：warning 无路径 → unknown
	mustExec(t, d, `INSERT INTO inject_log VALUES ('inj-5','codex-cli','sess-G','r100-8','2026-08-14T13:00:00','abc12348','','{"fileAssoc":["src/y.go"],"warnings":["注意编码规范"],"actionItems":[],"goals":[],"guidelines":false}',100,0)`)

	// discussion_log：注入后工具调用
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('d1','sess-A','assistant','codex-cli','edit proxy.go','2026-08-14T10:01:00','{"file_op":{"type":"edit","rel_path":"src/proxy.go"}}')`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('d2','sess-A','assistant','codex-cli','edit other.go','2026-08-14T10:02:00','{"file_op":{"type":"edit","rel_path":"src/other.go"}}')`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('d3','sess-B','assistant','codex-cli','read server.go','2026-08-14T11:05:00','{"file_op":{"type":"read","rel_path":"src/api/server.go"}}')`)

	// events（M4）：近 7 天 30 orphan + 5 tentative_link + 1 mcp_error
	now := time.Now()
	for i := 0; i < 30; i++ {
		mustExec(t, d, `INSERT INTO events VALUES (?, 'commit_orphan','commit','c','orphan','2026-08-14T12:00:00',0,0)`, "e-orphan-"+string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	for i := 0; i < 5; i++ {
		mustExec(t, d, `INSERT INTO events VALUES (?, 'tentative_link','commit','c','link','2026-08-14T12:00:00',0,0)`, "e-link-"+string(rune('a'+i)))
	}
	mustExec(t, d, `INSERT INTO events VALUES ('e-mcp','mcp_error','tool','x','err','2026-08-14T12:00:00',0,0)`)
	// 8 天前的 orphan 必须被 M4 时间窗排除（T3）
	mustExec(t, d, `INSERT INTO events VALUES ('e-old','commit_orphan','commit','c','old','`+now.AddDate(0,0,-8).Format("2006-01-02T15:04:05")+`',0,0)`)

	logFile := fixtureLog(t)
	since, _ := time.Parse("2006-01-02T15:04:05", "2026-08-14T00:00:00")
	rep, err := BuildAttribution(d, logFile, since)
	if err != nil {
		t.Fatalf("BuildAttribution: %v", err)
	}

	a := rep.ByAgent["codex-cli"]

	// M1（T2：no_summary 不进分母；same_content 进分母）
	if a.M1.Injected != 5 { // inj-1..5 全部写表（含 suppressed=1、无文件行、M3 unknown 行）
		t.Errorf("M1 injected = %d, want 5", a.M1.Injected)
	}
	if a.M1.SameContent != 1 {
		t.Errorf("M1 same_content = %d, want 1", a.M1.SameContent)
	}
	if a.M1.NoSummary != 1 {
		t.Errorf("M1 no_summary = %d, want 1（报告但排除）", a.M1.NoSummary)
	}
	if a.M1.Denominator != 6 || a.M1.Coverage != 5.0/6.0 {
		t.Errorf("M1 denom=%d cov=%v, want 6/%v", a.M1.Denominator, a.M1.Coverage, 5.0/6.0)
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
	logFile := fixtureLog(t)
	since, _ := time.Parse("2006-01-02T15:04:05", "2026-08-14T00:00:00")
	rep, err := BuildAttribution(d, logFile, since)
	if err != nil {
		t.Fatalf("BuildAttribution: %v", err)
	}
	// 无 inject_log 行 → injected=0，但 same_content/no_summary 从日志侧计入
	a := rep.ByAgent["codex-cli"]
	if a.M1.Injected != 0 || a.M1.SameContent != 1 || a.M1.NoSummary != 1 {
		t.Errorf("M1 without table rows: injected=%d same=%d nosum=%d, want 0/1/1",
			a.M1.Injected, a.M1.SameContent, a.M1.NoSummary)
	}
	if a.M1.Denominator != 1 || a.M1.Coverage != 0 {
		t.Errorf("M1 denom=%d cov=%v, want 1/0", a.M1.Denominator, a.M1.Coverage)
	}
}
