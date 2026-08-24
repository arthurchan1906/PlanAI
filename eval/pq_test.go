package eval

// P0a1a T1-T5 测试：用 SPEC §4.1 冻结口径数据（c0ad2534 事件边界 + 25 次纠偏构成）断言。

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

func pqDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	mustExec(t, d, `CREATE TABLE discussion_log (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, role TEXT NOT NULL, source TEXT NOT NULL DEFAULT '', content TEXT NOT NULL, created_at TEXT NOT NULL, metadata TEXT DEFAULT '')`)
	mustExec(t, d, `CREATE TABLE commits (id TEXT PRIMARY KEY, title TEXT NOT NULL, summary TEXT NOT NULL, branch TEXT NOT NULL, commit_hash TEXT NOT NULL, task_id TEXT, decision_id TEXT, status TEXT NOT NULL, test_status TEXT NOT NULL, review_status TEXT NOT NULL, files_json TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, evidence_summary TEXT NOT NULL DEFAULT '', review_notes TEXT NOT NULL DEFAULT '')`)
	mustExec(t, d, `CREATE TABLE bugs (id TEXT PRIMARY KEY, title TEXT NOT NULL, description TEXT NOT NULL, severity TEXT NOT NULL, status TEXT NOT NULL, commit_id TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', files TEXT NOT NULL DEFAULT '', root_cause TEXT NOT NULL DEFAULT '', fix TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '')`)
	return d
}

func pqTurn(id, content, created string) Turn {
	ts, _ := time.Parse("2006-01-02T15:04:05", created)
	return Turn{UserMsg: content, UserMsgID: id, Start: ts, End: ts}
}

func pqRec(tool, cmd, created string) Record {
	ts, _ := time.Parse("2006-01-02T15:04:05", created)
	return Record{Role: "assistant", Content: "", Tool: ToolRecord{Tool: tool, Command: cmd}, CreatedAt: ts}
}

// ── T1 时段边界 ──

func TestFrozenEventsTable(t *testing.T) {
	if len(C0ad2534FrozenEvents) != 9 {
		t.Fatalf("冻结事件表 = %d 条, want 9（§4.1 判定依据表）", len(C0ad2534FrozenEvents))
	}
	// 关键锚点抽查：首条消息 / 根因 / 修复 commit / 新 bug
	got := map[string]bool{}
	for _, e := range C0ad2534FrozenEvents {
		got[e.Ts] = true
	}
	for _, want := range []string{
		"2026-06-23T13:51:53", "2026-06-24T11:04:12",
		"2026-06-24T11:37:15", "2026-06-24T11:48:14", "2026-06-24T11:50:08",
	} {
		if !got[want] {
			t.Errorf("冻结事件表缺 %s", want)
		}
	}
}

func TestBuildSessionBoundary(t *testing.T) {
	d := pqDB(t)
	// 模拟 c0ad2534：6/23 13:51 首条 user → 跨夜 6/24 00:00 休眠 → 6/24 09:11 纠偏
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('u1','s','user','codex-cli','用户报 bug','2026-06-23T13:51:53','')`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('u2','s','user','codex-cli','继续测试','2026-06-23T14:20:00','')`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('u3','s','user','codex-cli','方向不对 重新审视','2026-06-24T09:11:46','')`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('a1','s','assistant','codex-cli','build','2026-06-24T10:00:00','')`)

	b, err := BuildSessionBoundary(d, "s")
	if err != nil {
		t.Fatal(err)
	}
	if b.Start.Format("2006-01-02T15:04:05") != "2026-06-23T13:51:53" {
		t.Errorf("start = %s, want 2026-06-23T13:51:53", b.Start)
	}
	if b.FirstUserID != "u1" {
		t.Errorf("first_user_id = %s, want u1", b.FirstUserID)
	}
	if len(b.SleepRanges) != 1 {
		t.Fatalf("sleep_ranges = %d, want 1（跨夜休眠）", len(b.SleepRanges))
	}
	if b.SleepRanges[0].Start.Format("2006-01-02") != "2026-06-23" || b.SleepRanges[0].End.Format("2006-01-02") != "2026-06-24" {
		t.Errorf("sleep range = %s → %s, want 6/23 → 6/24", b.SleepRanges[0].Start, b.SleepRanges[0].End)
	}
}

// ── T2 关联核对 ──

func TestLinkFixCommitPartial(t *testing.T) {
	d := pqDB(t)
	mustExec(t, d, `INSERT INTO commits VALUES ('c1','fix: PalV2 跨平台兼容 - header sizeof 对齐 + BLE 大块写分块','','main','d628b7aaa1b2cdc5c6f40027d7edd0463e5fe743','','','draft','passed','pending','[]','2026-06-24T11:48:14','2026-06-24T11:48:14','','')`)
	mustExec(t, d, `INSERT INTO bugs VALUES ('b1','iOS PalV2 密友跨平台不兼容：header sizeof 差1字节 + BLE 大块写截断导致 detail 解密失败','','major','resolved','','2026-06-24T11:43:37','2026-06-24T11:43:37','','','','','')`)

	cl, err := LinkFixCommitByHash(d, "d628b7a")
	if err != nil {
		t.Fatal(err)
	}
	if cl.Fallback != "partial" {
		t.Errorf("fallback = %s, want partial（bug.commit_id 为空 → 标题关键词 ≥2 命中）", cl.Fallback)
	}
	if cl.BugID != "b1" {
		t.Errorf("bug_id = %s, want b1", cl.BugID)
	}
	if cl.Weak {
		t.Error("weak = true, want false")
	}
	if cl.CreatedAt.Format("2006-01-02T15:04:05") != "2026-06-24T11:48:14" {
		t.Errorf("created_at = %s, want 2026-06-24T11:48:14", cl.CreatedAt)
	}
}

func TestLinkFixCommitNone(t *testing.T) {
	d := pqDB(t)
	mustExec(t, d, `INSERT INTO commits VALUES ('c1','chore: bump version','','main','abc123','','','draft','passed','pending','[]','2026-06-24T11:48:14','2026-06-24T11:48:14','','')`)
	cl, err := LinkFixCommitByHash(d, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if cl.Fallback != "none" || !cl.Weak {
		t.Errorf("fallback/weak = %s/%v, want none/true（弱 ground truth）", cl.Fallback, cl.Weak)
	}
}

// ── T3 反馈识别 ──

func TestClassifyUserText(t *testing.T) {
	cases := []struct {
		content string
		class   int
	}{
		{"你有没有思考问题就直接修改了", 1},                      // ① CJK
		{"[ED-DEBUG] key=value\n[LV-DIAG] a=b", 2}, // ② 纯结构化（连字符模块名）
		{"<task-notification> job done", 4},        // ④ 系统通知
		{"Failed to install: compile fail", 5},     // ⑤ 无 CJK 手动输入
		{"go on", 5},                               // ⑤ 命令/确认
		{"继续测试", 1},
	}
	for _, c := range cases {
		if got := classifyUserText(c.content).Class; got != c.class {
			t.Errorf("classifyUserText(%q) = %d, want %d", c.content, got, c.class)
		}
	}
}

func TestMatchKeywords(t *testing.T) {
	m := matchKeywords("你之前查的方向不对 重新审视一下")
	if len(m.Correction) < 2 {
		t.Errorf("correction = %v, want ≥2 命中（之前/查/方向不对/重新审视）", m.Correction)
	}
	m = matchKeywords("继续 可以 好")
	if len(m.Progress) < 2 {
		t.Errorf("progress = %v, want ≥2 命中（继续/可以/好）", m.Progress)
	}
}

func TestRecognizeFeedbackTwoLevel(t *testing.T) {
	turns := []Turn{
		pqTurn("d1", "之前你说的方向不对", "2026-06-23T14:00:00"),
		pqTurn("d2", "[VaultFlatten] loadAssets count=1", "2026-06-23T14:01:00"), // ② 存疑
		pqTurn("d3", "<task-notification> sync", "2026-06-23T14:02:00"),          // ④ 排除
		pqTurn("d4", "go on", "2026-06-23T14:03:00"),                             // ⑤ 介入非纠偏
		pqTurn("d5", "继续", "2026-06-23T14:04:00"),                                // 推进
	}
	_, counts := RecognizeFeedback(turns, false)
	if counts.Correction != 1 {
		t.Errorf("纠偏 = %d, want 1", counts.Correction)
	}
	if counts.Intervention != 3 { // d1 + d4 + d5
		t.Errorf("介入 = %d, want 3", counts.Intervention)
	}
	if counts.Suspicious != 1 {
		t.Errorf("存疑 = %d, want 1", counts.Suspicious)
	}
	if counts.Injection != 1 {
		t.Errorf("注入排除 = %d, want 1", counts.Injection)
	}
}

// ── T4 检索三分类 ──

func TestGitHistoryCmd(t *testing.T) {
	cases := map[string]bool{
		"git log --oneline -5":    true,
		"git blame src/a.go":      true,
		"git grep FIXME":          true,
		"git show d628b7a --stat": true,
		"git show HEAD:src/a.go":  false, // 当前态
		"git status":              false,
		"git diff":                false,
	}
	for cmd, want := range cases {
		if got := gitHistoryCmd(cmd); got != want {
			t.Errorf("gitHistoryCmd(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestCountRetrieval(t *testing.T) {
	turn := Turn{Records: []Record{
		pqRec("mcp_aipm_search", "aipm_search_context", "2026-06-24T09:10:00"), // 自发
		pqRec("mcp_aipm_trace", "aipm_trace_context", "2026-06-24T09:12:00"),   // 纠偏后 20min → 被动
		pqRec("mcp_aipm_read", "aipm_read_discussions", "2026-06-24T09:13:00"), // 例行
		pqRec("mcp_aipm_get", "aipm_get_task", "2026-06-24T09:14:00"),          // 状态读取不计
	}}
	cands := []FeedbackCandidate{
		{Ts: mustTs("2026-06-24T09:11:46"), Kind: KindCorrection},
	}
	st := CountRetrieval([]Turn{turn}, cands)
	if st.Spontaneous != 1 || st.Passive != 1 || st.Routine != 1 {
		t.Errorf("retrieval = %+v, want 自发=1 被动=1 例行=1", st)
	}
}

func mustTs(s string) time.Time {
	ts, _ := time.Parse("2006-01-02T15:04:05", s)
	return ts
}

// ── T5 死循环候选 ──

func TestIsRealBuild(t *testing.T) {
	cases := map[string]bool{
		"xcodebuild build -project x.xcodeproj":       true,
		"go build ./...":                              true,
		"cat DerivedData/App/Build/Products/a.app/..": false, // 噪声：DerivedData Build 路径
		"xcodebuild build 2>&1 | tee build5.log":      false, // 噪声：build5 版本号
		"cat Logs/Build/build.log":                    false, // 噪声：构建日志读取
		"echo hello":                                  false,
	}
	for cmd, want := range cases {
		if got := isRealBuild(cmd); got != want {
			t.Errorf("isRealBuild(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestFindDeadloops(t *testing.T) {
	// §4.1 小时表 + T5 校准（8/24）：15h/16h 盲试正样本 → 候选（edit/根因不排除）；
	// 11h 修复验证期负样本 → 靠 commit 信号排除（11:48 在 11h 桶内）
	mk := func(ts string, n int, kind string) []Record {
		var out []Record
		for i := 0; i < n; i++ {
			tool := map[string]string{"build": "bash", "edit": "edit"}[kind]
			cmd := map[string]string{"build": "go build ./...", "edit": ""}[kind]
			out = append(out, pqRec(tool, cmd, ts))
		}
		return out
	}
	turns := []Turn{
		{Records: mk("2026-06-23T15:10:00", 20, "build")},
		{Records: mk("2026-06-23T16:10:00", 15, "build")},
		{Records: mk("2026-06-24T11:10:00", 11, "build")},
		{Records: mk("2026-06-24T11:20:00", 3, "edit")}, // 修复执行（edit 不再排除）
	}
	commitTs := []time.Time{mustTs("2026-06-24T11:48:14")} // fix commit → 排除 11h
	cands := FindDeadloops(turns, nil, nil, commitTs, DefaultDeadloopParams())
	if len(cands) != 3 {
		t.Fatalf("死循环候选 = %d, want 3（15h/16h 候选 + 11h near-miss）", len(cands))
	}
	var excluded int
	for _, c := range cands {
		if c.Builds < 10 {
			t.Errorf("候选 build = %d, want ≥10", c.Builds)
		}
		if c.Excluded {
			excluded++
			if c.Reason == "" {
				t.Errorf("排除候选缺 Reason: %+v", c)
			}
		}
	}
	if excluded != 1 {
		t.Errorf("排除候选 = %d, want 1（11h 经 commit 排除）", excluded)
	}
}

// ── Claude 审核修复测试（2026-08-24）──

func TestLinkFixCommitTimeWindow(t *testing.T) {
	d := pqDB(t)
	// commit 与 bug 创建 ±1h 内 + files 有交集 → time_window fallback
	mustExec(t, d, `INSERT INTO commits VALUES ('c1','chore: 其他主题','','main','abc123def','','','draft','passed','pending','["src/proxy.go","src/hook.go"]','2026-06-24T11:48:14','2026-06-24T11:48:14','','')`)
	mustExec(t, d, `INSERT INTO bugs VALUES ('b1','无关标题','','major','open','','2026-06-24T11:10:00','2026-06-24T11:10:00','','src/proxy.go','','','')`)

	cl, err := LinkFixCommitByHash(d, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if cl.Fallback != "time_window" {
		t.Errorf("fallback = %s, want time_window（commit ±1h 且 files 交集）", cl.Fallback)
	}
	if cl.BugID != "b1" || cl.Weak {
		t.Errorf("bug_id/weak = %s/%v, want b1/false", cl.BugID, cl.Weak)
	}
}

func TestLinkFixCommitTimeWindowNoMatch(t *testing.T) {
	d := pqDB(t)
	mustExec(t, d, `INSERT INTO commits VALUES ('c1','chore: 其他主题','','main','abc123','','','draft','passed','pending','["src/proxy.go"]','2026-06-24T11:48:14','2026-06-24T11:48:14','','')`)
	// 超 1h + 无文件交集 → 落 none（弱 ground truth）
	mustExec(t, d, `INSERT INTO bugs VALUES ('b1','无关标题','','major','open','','2026-06-24T09:00:00','2026-06-24T09:00:00','','src/other.go','','','')`)
	cl, err := LinkFixCommitByHash(d, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if cl.Fallback != "none" || !cl.Weak {
		t.Errorf("fallback/weak = %s/%v, want none/true", cl.Fallback, cl.Weak)
	}
}

func TestRecognizeFeedbackModernChannel(t *testing.T) {
	turns := []Turn{
		pqTurn("d1", "之前的方向不对", "2026-08-19T15:09:00"),                         // ① CJK 纠偏仍计数
		pqTurn("d2", "Failed to install: compile fail", "2026-08-19T15:10:00"), // ⑤ 现代通道 → 候选不计数
		pqTurn("d3", "go on", "2026-08-19T15:11:00"),                           // ⑤ 同上
	}
	_, counts := RecognizeFeedback(turns, true)
	if counts.Correction != 1 {
		t.Errorf("纠偏 = %d, want 1", counts.Correction)
	}
	if counts.Intervention != 1 {
		t.Errorf("介入 = %d, want 1（现代通道⑤不直接计入）", counts.Intervention)
	}
	if counts.ManualCandidates != 2 {
		t.Errorf("manual_candidates = %d, want 2（P1 L2 确认后回填）", counts.ManualCandidates)
	}
	// legacy 通道：⑤ 直接计入介入
	_, legacy := RecognizeFeedback(turns, false)
	if legacy.Intervention != 3 {
		t.Errorf("legacy 介入 = %d, want 3", legacy.Intervention)
	}
}

func TestFindDeadloopsSignals(t *testing.T) {
	// 15h 桶：build 20 + fail 3 + user 2 + 零自发 → 候选；16h 桶含根因定位文本 → 记录不排除
	mk := func(ts string, n int, kind string, fail int) []Record {
		var out []Record
		for i := 0; i < n; i++ {
			tool := map[string]string{"build": "bash", "edit": "edit"}[kind]
			cmd := map[string]string{"build": "go build ./...", "edit": ""}[kind]
			r := pqRec(tool, cmd, ts)
			if kind == "build" && i < fail {
				ec := 1
				r.Tool.ExitCode = &ec
			}
			out = append(out, r)
		}
		return out
	}
	rc := pqRec("bash", "go build ./...", "2026-06-23T16:30:00")
	rc.Role = "assistant"
	rc.Content = "## 根因已确认：APDU 分块"
	turns := []Turn{
		pqTurn("u1", "继续", "2026-06-23T15:30:00"),
		pqTurn("u2", "看看", "2026-06-23T15:45:00"),
		{Records: mk("2026-06-23T15:10:00", 20, "build", 3)},
		{Records: mk("2026-06-23T16:10:00", 12, "build", 0)},
		{Records: []Record{rc}},
	}
	cands := FindDeadloops(turns, nil, nil, nil, DefaultDeadloopParams())
	if len(cands) != 2 {
		t.Fatalf("候选 = %d, want 2（15h/16h 均候选，根因文本只记录不排除）", len(cands))
	}
	var c15, c16 *DeadloopCandidate
	for i := range cands {
		if cands[i].Start.Hour() == 15 {
			c15 = &cands[i]
		}
		if cands[i].Start.Hour() == 16 {
			c16 = &cands[i]
		}
	}
	if c15 == nil || c15.Excluded || c15.Builds != 20 || c15.Fails != 3 || c15.UserMsgs != 2 {
		t.Errorf("15h 候选 = %+v, want build=20 fail=3 user=2 未排除", c15)
	}
	if c16 == nil || c16.Excluded || c16.RootCause != 1 {
		t.Errorf("16h 根因文本应记录不排除: %+v", c16)
	}
}

func TestFindDeadloopsPassiveExcludes(t *testing.T) {
	// 17h 纠偏响应实证：被动检索>0 → 非盲试，不构成候选
	mk := func(ts string, n int) []Record {
		var out []Record
		for i := 0; i < n; i++ {
			out = append(out, pqRec("bash", "go build ./...", ts))
		}
		return out
	}
	turns := []Turn{
		{Records: mk("2026-06-23T17:10:00", 12)},
	}
	passive := []time.Time{mustTs("2026-06-23T17:15:00")}
	cands := FindDeadloops(turns, nil, passive, nil, DefaultDeadloopParams())
	if len(cands) != 0 {
		t.Errorf("被动检索>0 不应构成候选: %+v", cands)
	}
}

// ── T6 目标锚定（P0a1b）──

// 规格实证输入（§9.3）：01a013f3 8/19 15:09 用户原话 vs 4b41ba8 commit 标题。
const (
	anchorMsg15_09 = "还有一个问题 从第三方应用选择图片或者文件 然后在打开方式中选择资云集 没有跳转资云集的界面 自己手动点击打开资云集才能看到有数据导入"
	anchorClaim4b41ba8 = "fix: 第三方「打开方式」直传文件不跳转/不导入 — SceneDelegate 转发 URL + 外部文件队列暂存"
)

func TestExtractHighFreqWords15_09(t *testing.T) {
	got := extractHighFreqWords(anchorMsg15_09, 2)
	if len(got) != 2 {
		t.Fatalf("高频场景词 = %d 个, want 2（资云集×3/打开×2）：%v", len(got), got)
	}
	byWord := map[string]int{}
	for _, w := range got {
		byWord[w.Word] = w.Count
	}
	if byWord["资云集"] != 3 {
		t.Errorf("资云集 = %d, want 3（规格 C2 实证）", byWord["资云集"])
	}
	if byWord["打开"] != 2 {
		t.Errorf("打开 = %d, want 2（打开方式 + 打开资云集，规格表述打开方式×2）", byWord["打开"])
	}
	// 停用词不得混入：选择×2 是动词
	if _, ok := byWord["选择"]; ok {
		t.Errorf("停用词「选择」混入高频词: %v", got)
	}
}

func TestExtractClaimWords4b41ba8(t *testing.T) {
	got := extractClaimWords(anchorClaim4b41ba8)
	want := []string{"第三方", "打开方式", "直传", "文件", "不跳转", "不导入"}
	if len(got) != len(want) {
		t.Fatalf("声称对象词 = %v, want %v（六词分法，直传文件拆分）", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("声称对象词[%d] = %s, want %s（全量 %v）", i, got[i], want[i], got)
		}
	}
}

// 验收③目标锚定 = 负样本验证（存在性）：15:09 vs 4b41ba8 已知对准，不误报为错位。
func TestAnchoringNegativeSampleAligned(t *testing.T) {
	target := AnchorTarget{
		SessionID: "01a013f3", UserMsg: anchorMsg15_09,
		UserTs: mustTs("2026-08-19T15:09:56"),
		Claim:  anchorClaim4b41ba8, ClaimTs: mustTs("2026-08-19T15:33:05"),
	}
	res := AnalyzeAnchoring(target, DefaultAnchoringParams())
	if res.Undecidable {
		t.Fatalf("不可判定（存在场景词），结果: %+v", res)
	}
	if !res.Aligned {
		t.Fatalf("负样本误报：15:09 vs 4b41ba8 已知对准被判错位。覆盖率 %.2f 高频全命中 %v", res.Coverage, res.HighFreqAll)
	}
	if res.Coverage < 0.5 {
		t.Errorf("覆盖率 %.2f < 0.5", res.Coverage)
	}
	if !res.HighFreqAll {
		t.Errorf("高频词全命中 = false（打开 ↔ 打开方式 应共享）")
	}
	// 实证 0.83（5/6）：第三方/打开方式/文件/跳转/导入 共享，直传未命中
	if got := len(res.Shared); got != 5 {
		t.Errorf("共享词 = %d 个, want 5（0.83）：%v", got, res.Shared)
	}
}

// 高频词全命中判定的正/负例：声称对象覆盖用户强调主题（打开 ↔ 打开方式）。
func TestHighFreqShared(t *testing.T) {
	positive := []SceneWord{{Word: "打开", Count: 2}, {Word: "资云集", Count: 3}}
	if !highFreqShared(positive, []string{"打开方式", "直传", "文件"}) {
		t.Error("打开 ↔ 打开方式 应共享（声称对象覆盖用户强调主题）")
	}
	negative := []SceneWord{{Word: "资云集", Count: 3}}
	if highFreqShared(negative, []string{"直传", "文件"}) {
		t.Error("资云集 与 直传/文件 无共享，不应判命中")
	}
}

// 明显错位样本：用户报 A（资云集打开方式跳转）、agent 首 commit 做 B（蓝牙配对刷新）→ 目标错位候选。
func TestAnchoringMismatch(t *testing.T) {
	target := AnchorTarget{
		UserMsg: "从第三方选择图片 打开方式 资云集 打开方式 资云集 没有跳转",
		Claim:   "fix: 蓝牙设备配对列表刷新异常",
	}
	res := AnalyzeAnchoring(target, DefaultAnchoringParams())
	if res.Undecidable {
		t.Fatal("有场景词，不应不可判定")
	}
	if res.Aligned {
		t.Errorf("错位样本误判对准: %+v", res)
	}
	if res.Coverage >= 0.5 {
		t.Errorf("错位样本覆盖率 %.2f 应 < 0.5", res.Coverage)
	}
}

// 不可判定：无场景词（无重复 CJK 主题词 / 纯英文）。
func TestAnchoringUndecidable(t *testing.T) {
	target := AnchorTarget{UserMsg: "继续", Claim: "fix: 继续开发"}
	res := AnalyzeAnchoring(target, DefaultAnchoringParams())
	if !res.Undecidable {
		t.Errorf("短消息无场景词应不可判定: %+v", res)
	}
	target2 := AnchorTarget{UserMsg: "please continue the work", Claim: "feat: add auth"}
	res2 := AnalyzeAnchoring(target2, DefaultAnchoringParams())
	if !res2.Undecidable {
		t.Errorf("纯英文消息应不可判定: %+v", res2)
	}
}

// ── T7 构建产物完整性（P0a1b）──

// 验收③空壳构建单样本：01a013f3 8/20 10:24 xcodebuild build（BUILD SUCCEEDED）→
// 10:27:26 ls /tmp/EDDerived/.../EncryptDrive.app/ 输出 total 16（无主可执行）→ 规则命中。
func TestHollowBuildDetected(t *testing.T) {
	turns := []Turn{{
		UserMsg: "编译验证主工程",
		Start:   mustTs("2026-08-20T10:24:03"), End: mustTs("2026-08-20T10:27:26"),
		Records: []Record{
			pqRec("bash", "xcodebuild -project EncryptDrive.xcodeproj -scheme EncryptDrive -sdk iphonesimulator -derivedDataPath /tmp/EDDerived build 2>&1 | tail -5", "2026-08-20T10:24:23"),
			pqRec("bash", "echo \"===/tmp sim build contents===\"; ls -la /tmp/EDDerived/Build/Products/Debug-iphonesimulator/EncryptDrive.app/ | head -20", "2026-08-20T10:27:26"),
		},
	}}
	// 补 ls 输出（空壳：total 16，无 EncryptDrive 主可执行行）
	turns[0].Records[1].Content = "===/tmp sim build contents===\ntotal 16\ndrwxr-xr-x  5 dazsec  wheel   160 Aug 20 10:24 .\ndrwxr-xr-x@ 3 dazsec  wheel    96 Aug 20 10:24 ..\ndrwxr-xr-x  3 dazsec  wheel    96 Aug 20 10:24 Frameworks\n"

	hbs := DetectHollowBuilds(turns, DefaultArtifactParams())
	if len(hbs) != 1 {
		t.Fatalf("空壳构建候选 = %d, want 1（10:25 空壳构建单样本）", len(hbs))
	}
	if !hbs[0].Hollow {
		t.Errorf("Hollow = false, want true（ls total 16 无主可执行）")
	}
	if hbs[0].AppPath != "EncryptDrive.app" {
		t.Errorf("AppPath = %s, want EncryptDrive.app", hbs[0].AppPath)
	}
}

// 正常构建：产物含主可执行文件 → 不标记空壳。
func TestHollowBuildNormalNotFlagged(t *testing.T) {
	turns := []Turn{{
		UserMsg: "构建验证",
		Start:   mustTs("2026-08-20T11:00:00"), End: mustTs("2026-08-20T11:05:00"),
		Records: []Record{
			pqRec("bash", "xcodebuild -project EncryptDrive.xcodeproj -scheme EncryptDrive -sdk iphonesimulator build 2>&1 | tail -5", "2026-08-20T11:00:10"),
			pqRec("bash", "ls -la /tmp/Out/Debug-iphonesimulator/EncryptDrive.app/", "2026-08-20T11:05:00"),
		},
	}}
	turns[0].Records[1].Content = "total 4096\ndrwxr-xr-x  5 dazsec  wheel   160 Aug 20 11:05 .\n-rwxr-xr-x  1 dazsec  wheel  987654 Aug 20 11:05 EncryptDrive\n"

	hbs := DetectHollowBuilds(turns, DefaultArtifactParams())
	if len(hbs) != 0 {
		t.Fatalf("正常构建被误标空壳: %+v", hbs)
	}
}

// 无产物检查的构建 → 不标记（缺证据不误报）。
func TestHollowBuildNoCheckNotFlagged(t *testing.T) {
	turns := []Turn{{
		UserMsg: "构建",
		Start:   mustTs("2026-08-20T11:00:00"), End: mustTs("2026-08-20T11:01:00"),
		Records: []Record{
			pqRec("bash", "xcodebuild -project EncryptDrive.xcodeproj build 2>&1 | tail -5", "2026-08-20T11:00:10"),
		},
	}}
	hbs := DetectHollowBuilds(turns, DefaultArtifactParams())
	if len(hbs) != 0 {
		t.Fatalf("无产物检查的构建不应标记: %+v", hbs)
	}
}

// 构建产物占位符残留信号（产物 Info.plist CFBundleExecutable = $(EXECUTABLE_NAME)）。
func TestHollowBuildPlaceholder(t *testing.T) {
	turns := []Turn{{
		UserMsg: "构建",
		Start:   mustTs("2026-08-20T11:00:00"), End: mustTs("2026-08-20T11:02:00"),
		Records: []Record{
			pqRec("bash", "xcodebuild -project EncryptDrive.xcodeproj build 2>&1 | tail -5", "2026-08-20T11:00:10"),
			pqRec("bash", "/usr/libexec/PlistBuddy -c \"Print :CFBundleExecutable\" /tmp/Out/EncryptDrive.app/Info.plist", "2026-08-20T11:02:00"),
		},
	}}
	turns[0].Records[1].Content = "$(EXECUTABLE_NAME)\n"

	hbs := DetectHollowBuilds(turns, DefaultArtifactParams())
	if len(hbs) != 1 || !hbs[0].Placeholder {
		t.Fatalf("占位符残留应被标记: %+v", hbs)
	}
}

// ── T8 命令级五子信号（P0a1b）──

// 响应✓ + 对准✓ + 收敛✓：纠偏「分享时没有日志打印」→ agent 20min 内 edit 分享日志相关文件 →
// 用户确认「可以了」。
func TestSignalResponseAlignedConverged(t *testing.T) {
	turns := []Turn{{
		UserMsg: "分享时没有日志打印",
		Start:   mustTs("2026-08-19T17:06:09"), End: mustTs("2026-08-19T17:10:00"),
		Records: []Record{
			pqRec("bash", "grep -rn \"print\" ShareExtension --include='*.swift'", "2026-08-19T17:06:30"),
			pqRec("edit", "add log", "2026-08-19T17:08:00"),
		},
	}, {
		UserMsg: "可以了",
		Start:   mustTs("2026-08-19T17:12:00"), End: mustTs("2026-08-19T17:12:00"),
	}}
	// 给 edit 记录补文件
	turns[0].Records[1].Tool.Files = []string{"ShareExtension/ShareViewController.swift"}

	feedback := []FeedbackCandidate{{
		Ts: mustTs("2026-08-19T17:06:09"), Kind: KindCorrection,
		Snippet: "分享时没有日志打印", Referents: []string{"分享", "日志", "打印"},
	}}
	rep := BuildSignalReport(turns, feedback, nil, DefaultSignalParams())
	if len(rep.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rep.Rows))
	}
	r := rep.Rows[0]
	if !r.Response {
		t.Errorf("响应 = false, want true（17:08 edit 分享扩展文件）")
	}
	if !r.Aligned {
		t.Errorf("对准近似 = false, want true（ShareViewController ↔ 分享）")
	}
	if !r.Converged {
		t.Errorf("收敛 = false, want true（用户确认「可以了」）")
	}
}

// 响应✓ + 对准✗：纠偏「分享时没有日志打印」→ agent 补不相关 dzsec-import 日志（01a013f3 17:06 实证形态）。
func TestSignalAlignedMismatch(t *testing.T) {
	turns := []Turn{{
		UserMsg: "分享时没有日志打印",
		Start:   mustTs("2026-08-19T17:06:09"), End: mustTs("2026-08-19T17:09:00"),
		Records: []Record{
			pqRec("edit", "add log", "2026-08-19T17:07:00"),
		},
	}}
	turns[0].Records[0].Tool.Files = []string{"EncryptDrive/App/dzsec-import.swift"}

	feedback := []FeedbackCandidate{{
		Ts: mustTs("2026-08-19T17:06:09"), Kind: KindCorrection,
		Snippet: "分享时没有日志打印", Referents: []string{"分享", "日志", "打印"},
	}}
	rep := BuildSignalReport(turns, feedback, nil, DefaultSignalParams())
	r := rep.Rows[0]
	if !r.Response {
		t.Errorf("响应 = false, want true（有 edit 行为）")
	}
	if r.Aligned {
		t.Errorf("对准近似 = true, want false（dzsec-import ↔ 分享 无共享）")
	}
}

// 响应✗：纠偏后只重复纠偏前命令（旧行为重现，无新命令/edit）→ 响应 ✗（交 L2）。
func TestSignalNoResponse(t *testing.T) {
	turns := []Turn{{
		UserMsg: "继续测试",
		Start:   mustTs("2026-06-24T09:00:00"), End: mustTs("2026-06-24T09:11:00"),
		Records: []Record{
			pqRec("bash", "git log --oneline -10", "2026-06-24T09:05:00"),
		},
	}, {
		UserMsg: "方向不对 重新审视",
		Start:   mustTs("2026-06-24T09:11:46"), End: mustTs("2026-06-24T09:30:00"),
		Records: []Record{
			pqRec("bash", "git log --oneline -10", "2026-06-24T09:20:00"), // 与纠偏前重复
		},
	}}
	feedback := []FeedbackCandidate{{
		Ts: mustTs("2026-06-24T09:11:46"), Kind: KindCorrection,
		Snippet: "方向不对 重新审视", Referents: []string{"方向"},
	}}
	rep := BuildSignalReport(turns, feedback, nil, DefaultSignalParams())
	if rep.Rows[0].Response {
		t.Errorf("响应 = true, want false（纠偏后仅重复旧命令）")
	}
}

// 持续近似✗：响应后同命令重现（旧对象重现）。
func TestSignalNotPersistent(t *testing.T) {
	turns := []Turn{{
		UserMsg: "还是不行",
		Start:   mustTs("2026-08-13T11:37:00"), End: mustTs("2026-08-13T12:00:00"),
		Records: []Record{
			pqRec("bash", "xcodebuild -project X build 2>&1 | tail -5", "2026-08-13T11:38:00"),
			pqRec("bash", "xcodebuild -project X build 2>&1 | tail -5", "2026-08-13T11:50:00"),
		},
	}}
	feedback := []FeedbackCandidate{{
		Ts: mustTs("2026-08-13T11:37:00"), Kind: KindCorrection,
		Snippet: "还是不行", Referents: []string{"显示"},
	}}
	rep := BuildSignalReport(turns, feedback, nil, DefaultSignalParams())
	r := rep.Rows[0]
	if !r.Response {
		t.Fatalf("响应 = false, want true")
	}
	if r.Persistent {
		t.Errorf("持续近似 = true, want false（11:50 同命令重现）")
	}
}

// 收敛不绑自报：agent 声称「编译通过」但无用户确认/无 commit → 收敛 ✗。
func TestSignalConvergedNotSelfClaim(t *testing.T) {
	turns := []Turn{{
		UserMsg: "请修复",
		Start:   mustTs("2026-08-20T10:20:00"), End: mustTs("2026-08-20T10:26:00"),
		Records: []Record{
			pqRec("bash", "xcodebuild -project EncryptDrive.xcodeproj build 2>&1 | tail -5", "2026-08-20T10:24:00"),
			pqRec("llm_message", "", "2026-08-20T10:25:21"),
		},
	}}
	turns[0].Records[1].Content = "完成。改动已就绪，编译通过。"
	feedback := []FeedbackCandidate{{
		Ts: mustTs("2026-08-20T10:20:00"), Kind: KindCorrection,
		Snippet: "请修复", Referents: []string{"打开方式"},
	}}
	rep := BuildSignalReport(turns, feedback, nil, DefaultSignalParams())
	if rep.Rows[0].Converged {
		t.Errorf("收敛 = true, want false（agent 自报「编译通过」≠ 收敛，无用户确认/无 commit）")
	}
}

// ── T9 验收报告（P0a1b）──

// 验收① 分钟级计算：候选命中正样本（6/23 15:00-17:00、6/24 09:00-09:11）与负样本。
func TestAcceptance1MinuteCalc(t *testing.T) {
	rep := &AcceptanceReport{Process: &ProcessReport{Deadloops: []DeadloopCandidate{
		{Start: mustTs("2026-06-23T15:00:00"), End: mustTs("2026-06-23T17:00:00")}, // 正样本 120min
		{Start: mustTs("2026-06-24T09:00:00"), End: mustTs("2026-06-24T09:11:00")}, // 正样本 11min
		{Start: mustTs("2026-06-24T11:05:00"), End: mustTs("2026-06-24T11:10:00")}, // 负样本（修复执行）5min
	}}}
	rows := acceptanceRows(rep)
	var recallRow, fpRow *AcceptanceRow
	for i := range rows {
		if rows[i].ID == "验收①" && rows[i].Item == "死循环召回" {
			recallRow = &rows[i]
		}
		if rows[i].ID == "验收①" && rows[i].Item == "负样本误报" {
			fpRow = &rows[i]
		}
	}
	if recallRow == nil || !strings.HasPrefix(recallRow.Status, "pass") {
		t.Errorf("召回未达标: %+v", recallRow)
	}
	if !strings.Contains(recallRow.Status, "131/131") {
		t.Errorf("召回明细 = %s, want 131/131（正样本全覆盖）", recallRow.Status)
	}
	if fpRow == nil || !strings.HasPrefix(fpRow.Status, "pass") {
		t.Errorf("误报超限: %+v", fpRow)
	}
	if !strings.Contains(fpRow.Status, "5 分钟") {
		t.Errorf("误报明细 = %s, want 5 分钟", fpRow.Status)
	}
}

// 验收③：anchor 负样本（15:09 vs 4b41ba8）→ 目标锚定 pass；空壳构建 → pass。
func TestAcceptance3Rows(t *testing.T) {
	anchor := AnchorTarget{UserMsg: anchorMsg15_09, Claim: anchorClaim4b41ba8}
	rep := &AcceptanceReport{
		Anchoring: func() *AnchoringResult {
			a := AnalyzeAnchoring(anchor, DefaultAnchoringParams())
			return &a
		}(),
		Hollow: []HollowBuild{{Hollow: true, BuildCmd: "xcodebuild ... build", BuildAt: mustTs("2026-08-20T10:24:23")}},
	}
	rows := acceptanceRows(rep)
	got := map[string]string{}
	for _, r := range rows {
		if r.ID == "验收③" {
			got[r.Item] = r.Status
		}
	}
	if !strings.HasPrefix(got["空壳构建单样本检出"], "pass") {
		t.Errorf("空壳构建检出未 pass: %s", got["空壳构建单样本检出"])
	}
	if !strings.HasPrefix(got["目标锚定负样本验证"], "pass") {
		t.Errorf("目标锚定负样本未 pass: %s", got["目标锚定负样本验证"])
	}
}

// 验收③：无 anchor 参数 → not_run（不卡验收）。
func TestAcceptance3AnchorNotRun(t *testing.T) {
	rep := &AcceptanceReport{}
	rows := acceptanceRows(rep)
	for _, r := range rows {
		if r.ID == "验收③" && r.Item == "目标锚定负样本验证" {
			if r.Status != "not_run" {
				t.Errorf("无 anchor 应 not_run: %s", r.Status)
			}
			return
		}
	}
	t.Error("验收③ 目标锚定行缺失")
}

// 验收②：自发/被动比方向性输出。
func TestAcceptance2Directional(t *testing.T) {
	rep := &AcceptanceReport{Process: &ProcessReport{Retrieval: RetrievalStats{Spontaneous: 2, Passive: 9, Ratio: 0.222}}}
	rows := acceptanceRows(rep)
	for _, r := range rows {
		if r.ID == "验收②" {
			if r.Status != "directional" {
				t.Errorf("验收② 应为 directional: %s", r.Status)
			}
			return
		}
	}
	t.Error("验收② 行缺失")
}
