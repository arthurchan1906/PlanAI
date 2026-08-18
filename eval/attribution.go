package eval

// M1-M5 归因提取器（HARNESS_ROADMAP §2，8/18 写策略修订后口径）。
// headless：纯 SQL + 日志解析，零 LLM 依赖；fixture 驱动测试见 attribution_test.go。
//
// 8/18 口径修订：inject_log 实际注入即写（suppressed=0/1 如实记录），
// same_content/no_summary 不写表（对照组从日志侧重建）。
// M2 分层：full_inject（suppressed=0 完整注入）/ partial_inject（suppressed=1
// 不完整注入）/ same_content_ctl / no_summary_ctl（日志侧对照组）。

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// AttributionReport 归因报告（HARNESS M1-M5）。
type AttributionReport struct {
	Since   string              `json:"since"`
	ByAgent map[string]AgentM12 `json:"by_agent"` // M1/M2 按 agent
	M3      M3Report            `json:"m3"`
	M4      M4Report            `json:"m4"`
	M5      M5Report            `json:"m5"`
}

// AgentM12 单个 agent 的 M1（注入覆盖率）与 M2（文件命中率）指标。
type AgentM12 struct {
	M1 M1Report `json:"m1"`
	M2 M2Report `json:"m2"`
}

// M1Report 注入覆盖率：分子=inject_log 行数（含 suppressed=1），
// 分母=inject + same_content（排除 no_summary_data）。
type M1Report struct {
	Injected    int     `json:"injected"`
	SameContent int     `json:"same_content"`
	Denominator int     `json:"denominator"`
	Coverage    float64 `json:"coverage"`
	NoSummary   int     `json:"no_summary"` // 报告但不进分母
}

// M2Group 命中率组。
type M2Group struct {
	Sessions    int     `json:"sessions"`
	HitSessions int     `json:"hit_sessions"`
	HitRate     float64 `json:"hit_rate"`
}

// M2Report 文件命中率（按 session 计，不按文件计）。
type M2Report struct {
	FullInject     M2Group `json:"full_inject"`     // inject_log suppressed=0 且 fileAssoc 非空
	PartialInject  M2Group `json:"partial_inject"`  // inject_log suppressed=1 且 fileAssoc 非空（8/18 新分层）
	SameContentCtl M2Group `json:"same_content_ctl"` // 日志侧对照组
	NoSummaryCtl   M2Group `json:"no_summary_ctl"`   // 日志侧对照组
}

// M3Report 警告回避率：注入的 warning 指向文件 X 后，N 轮内未对 X 写操作。
type M3Report struct {
	Mapped     int     `json:"mapped"`     // 能映射到文件的 warning 数
	Avoided    int     `json:"avoided"`    // 回避的 warning 数
	Unknown    int     `json:"unknown"`    // 无法映射到文件的 warning 数（不进分母）
	Avoidance  float64 `json:"avoidance_rate"`
}

// M4Report 立即行动区信噪比：近 7 天 unconsumed 事件中噪音事件占比。
type M4Report struct {
	Total     int     `json:"total"`      // 近 7 天 unconsumed events 全部
	Noise     int     `json:"noise"`      // isEmergeEvent 噪音类型
	NoiseRatio float64 `json:"noise_ratio"`
}

// M5Report 截断分布（:153 suppressed 行按 segment 汇总）。
type M5Report struct {
	SuppressedRequests int       `json:"suppressed_requests"`
	Segments           M5Segments `json:"segments"`
}

type M5Segments struct {
	FileAssoc  int `json:"file_cut"`
	Warnings   int `json:"warn"`
	Actions    int `json:"act"`
	Goals      int `json:"goals"`
	Guidelines int `json:"guide"`
}

// m2Session 注入组的一条 session 记录（取该 session 最后一次注入作为水位）。
type m2Session struct {
	agent, session string
	ts             time.Time
	files          map[string]bool
}

// BuildAttribution 计算 M1-M5。d 为 pmai.db 连接；logFile 为 aipmc 日志路径
// （日志行解析 :153/:166 对照组与 M5）；since 为 M1/M2/M3 时间窗起点，
// M4 固定近 7 天（HARNESS §2）。
func BuildAttribution(d *sql.DB, logFile string, since time.Time) (*AttributionReport, error) {
	rep := &AttributionReport{
		Since:   since.Format(time.RFC3339),
		ByAgent: map[string]AgentM12{},
	}
	logLines, err := readLogLines(logFile)
	if err != nil {
		return nil, err
	}
	sinceISO := since.Format("2006-01-02T15:04:05")

	// ── M1: inject_log 分子 + 日志 same_content/no_summary 分母 ──
	rows, err := d.Query(`SELECT agent, COUNT(*) FROM inject_log WHERE ts >= ? GROUP BY agent`, sinceISO)
	if err != nil {
		return nil, fmt.Errorf("m1 inject_log: %w", err)
	}
	for rows.Next() {
		var agent string
		var n int
		if err := rows.Scan(&agent, &n); err != nil {
			rows.Close()
			return nil, err
		}
		a := rep.ByAgent[agent]
		a.M1.Injected = n
		rep.ByAgent[agent] = a
	}
	rows.Close()

	// 日志侧：same_content / no_summary（8/18 后不写表，仅日志可统计）
	m2Ctl := map[string]map[string]time.Time{} // agent → session → last skip ts
	for _, ln := range logLines {
		if !strings.Contains(ln, "[INJECT] skip") {
			continue
		}
		ts, agent, session, reason := parseSkipLine(ln)
		if ts.IsZero() || ts.Before(since) {
			continue
		}
		if agent == "" {
			continue
		}
		a := rep.ByAgent[agent]
		switch {
		case reason == "same_content":
			a.M1.SameContent++
			ctl := m2Ctl[agent]
			if ctl == nil {
				ctl = map[string]time.Time{}
				m2Ctl[agent] = ctl
			}
			if session != "" {
				if t, ok := ctl[session]; !ok || ts.After(t) {
					ctl[session] = ts
				}
			}
		case reason == "no_summary_data":
			a.M1.NoSummary++
		}
		rep.ByAgent[agent] = a
	}
	for agent, a := range rep.ByAgent {
		a.M1.Denominator = a.M1.Injected + a.M1.SameContent
		if a.M1.Denominator > 0 {
			a.M1.Coverage = float64(a.M1.Injected) / float64(a.M1.Denominator)
		}
		rep.ByAgent[agent] = a
	}

	// ── M2: 注入组（full/partial）+ 对照组（same_content/no_summary）──
	injRows, err := d.Query(`SELECT agent, session_id, ts, segments_json, suppressed FROM inject_log WHERE ts >= ?`, sinceISO)
	if err != nil {
		return nil, fmt.Errorf("m2 inject_log: %w", err)
	}
	type injRec struct {
		session    string
		ts         time.Time
		fileAssoc  []string
		suppressed int
	}
	injBySession := map[string][]injRec{} // agent|session → records
	for injRows.Next() {
		var agent, session, ts, segJSON string
		var suppressed int
		if err := injRows.Scan(&agent, &session, &ts, &segJSON, &suppressed); err != nil {
			injRows.Close()
			return nil, err
		}
		if session == "" {
			continue
		}
		var seg struct {
			FileAssoc []string `json:"fileAssoc"`
		}
		_ = json.Unmarshal([]byte(segJSON), &seg)
		t, _ := time.Parse("2006-01-02T15:04:05", ts)
		key := agent + "|" + session
		injBySession[key] = append(injBySession[key], injRec{session: session, ts: t, fileAssoc: seg.FileAssoc, suppressed: suppressed})
	}
	injRows.Close()

	// 每 session 取最后一次注入（水位），按 suppressed 分 full/partial 组
	var fullSess, partialSess []m2Session
	for key, recs := range injBySession {
		last := recs[0]
		for _, r := range recs[1:] {
			if r.ts.After(last.ts) {
				last = r
			}
		}
		files := map[string]bool{}
		for _, f := range last.fileAssoc {
			files[f] = true
		}
		if len(files) == 0 {
			continue // 无文件关联的注入不进 M2 分母（口径：注入含 ≥1 个文件）
		}
		parts := strings.SplitN(key, "|", 2)
		ms := m2Session{agent: parts[0], session: last.session, ts: last.ts, files: files}
		if last.suppressed == 0 {
			fullSess = append(fullSess, ms)
		} else {
			partialSess = append(partialSess, ms)
		}
	}
	// 对照组：日志侧 skip 行末次时间之后的文件调用（基线）
	ctlFiles := map[string][]string{} // agent|session → [file...]（抑制后引用的文件）

	fullHit := computeM2Hit(d, fullSess)
	partialHit := computeM2Hit(d, partialSess)
	_ = ctlFiles

	// 对照组：从 m2Ctl（same_content）与 no_summary（暂从日志侧重建 session）计算
	sameCtl := computeCtlHit(d, m2Ctl, logLines, since, "same_content")
	noSumCtl := computeCtlHitNoSummary(d, logLines, since)

	// 归并到 ByAgent
	for agent, a := range rep.ByAgent {
		a.M2.FullInject = groupFor(fullHit, agent)
		a.M2.PartialInject = groupFor(partialHit, agent)
		a.M2.SameContentCtl = groupFor(sameCtl, agent)
		a.M2.NoSummaryCtl = groupFor(noSumCtl, agent)
		rep.ByAgent[agent] = a
	}

	// ── M3: 警告回避率（inject_log warnings + discussion_log 写操作）──
	m3, err := buildM3(d, sinceISO)
	if err != nil {
		return nil, err
	}
	rep.M3 = m3

	// ── M4: 立即行动区信噪比（近 7 天 unconsumed events）──
	m4, err := buildM4(d)
	if err != nil {
		return nil, err
	}
	rep.M4 = m4

	// ── M5: 截断分布（:153 日志行）──
	rep.M5 = buildM5(logLines, since)

	// 排序 ByAgent 保证输出稳定
	agents := make([]string, 0, len(rep.ByAgent))
	for k := range rep.ByAgent {
		agents = append(agents, k)
	}
	sort.Strings(agents)
	sorted := map[string]AgentM12{}
	for _, k := range agents {
		sorted[k] = rep.ByAgent[k]
	}
	rep.ByAgent = sorted

	return rep, nil
}

// ── M2 helpers ──

type m2Hit struct {
	agent        string
	sessions     int
	hitSessions  int
}

// computeM2Hit 注入组：session 注入 ts 之后是否引用了注入文件（file_op 任意类型）。
func computeM2Hit(d *sql.DB, sessions []m2Session) map[string]m2Hit {
	hits := map[string]m2Hit{}
	for _, s := range sessions {
		h := hits[s.agent]
		h.sessions++
		hit := sessionReferencedAny(d, s.session, s.ts, s.files)
		if hit {
			h.hitSessions++
		}
		hits[s.agent] = h
	}
	return hits
}

func groupFor(hits map[string]m2Hit, agent string) M2Group {
	h, ok := hits[agent]
	if !ok {
		return M2Group{}
	}
	g := M2Group{Sessions: h.sessions, HitSessions: h.hitSessions}
	if g.Sessions > 0 {
		g.HitRate = float64(g.HitSessions) / float64(g.Sessions)
	}
	return g
}

// computeCtlHit 对照组：日志侧 (agent, session) 抑制 ts 之后有文件工具调用即「命中」
// （对照组无注入内容，命中=抑制后该 session 仍引用文件的比例，作为基线）。
func computeCtlHit(d *sql.DB, ctl map[string]map[string]time.Time, logLines []string, since time.Time, reason string) map[string]m2Hit {
	hits := map[string]m2Hit{}
	for agent, sessions := range ctl {
		for session, ts := range sessions {
			h := hits[agent]
			h.sessions++
			if sessionReferencedAny(d, session, ts, nil) {
				h.hitSessions++
			}
			hits[agent] = h
		}
	}
	return hits
}

// computeCtlHitNoSummary no_summary_data 对照组（M1 分母排除，M2 单独报告）。
func computeCtlHitNoSummary(d *sql.DB, logLines []string, since time.Time) map[string]m2Hit {
	hits := map[string]m2Hit{}
	seen := map[string]bool{}
	for _, ln := range logLines {
		if !strings.Contains(ln, "reason=no_summary_data") {
			continue
		}
		ts, agent, session, _ := parseSkipLine(ln)
		if ts.IsZero() || ts.Before(since) || agent == "" || session == "" {
			continue
		}
		key := agent + "|" + session
		if seen[key] {
			continue
		}
		seen[key] = true
		h := hits[agent]
		h.sessions++
		if sessionReferencedAny(d, session, ts, nil) {
			h.hitSessions++
		}
		hits[agent] = h
	}
	return hits
}

// sessionReferencedAny 查询 discussion_log：session 在 ts 之后是否有 file_op 工具调用，
// 且（若 files 非空）引用了注入文件集合中的任一文件。
func sessionReferencedAny(d *sql.DB, session string, ts time.Time, files map[string]bool) bool {
	if session == "" {
		return false
	}
	tsISO := ts.Format("2006-01-02T15:04:05")
	rows, err := d.Query(`SELECT metadata FROM discussion_log WHERE session_id = ? AND created_at > ? AND metadata != ''`, session, tsISO)
	if err != nil {
		return false
	}
	defer rows.Close()
	pathRe := regexp.MustCompile(`"rel_path"\s*:\s*"([^"]+)"`)
	for rows.Next() {
		var md string
		if err := rows.Scan(&md); err != nil {
			continue
		}
		if !strings.Contains(md, "file_op") {
			continue
		}
		m := pathRe.FindStringSubmatch(md)
		if m == nil {
			continue
		}
		if files == nil {
			return true
		}
		if files[m[1]] {
			return true
		}
	}
	return false
}

// ── M3 ──

var pathInWarningRe = regexp.MustCompile(`[A-Za-z0-9_\-./]+\.(go|js|ts|py|rs|java|rb|c|cpp|h|hpp|swift|kt|scala|css|html|vue|svelte|sql|sh|yaml|yml|toml|json|md)\b`)

var writeTypes = map[string]bool{
	"edit": true, "create": true, "delete": true, "rename": true, "append": true,
}

// buildM3 警告回避率：注入 warning 含可映射路径 → 注入后该 session 5 条 file_op 内
// 是否对该路径发生写操作；未写=回避。不可映射路径记 unknown（不进分母）。
func buildM3(d *sql.DB, sinceISO string) (M3Report, error) {
	var rep M3Report
	rows, err := d.Query(`SELECT session_id, ts, segments_json FROM inject_log WHERE ts >= ? AND segments_json != '{}'`, sinceISO)
	if err != nil {
		return rep, fmt.Errorf("m3: %w", err)
	}
	// 先把行读入内存再关闭 rows：sqlite 单连接池下，外层 rows 未关闭时
	// 循环内的 sessionWrotePath 内层查询会阻塞/失败（M3 计数失真根因）。
	type warnRec struct {
		session  string
		ts       string
		warnings []string
	}
	var recs []warnRec
	for rows.Next() {
		var session, ts, segJSON string
		if err := rows.Scan(&session, &ts, &segJSON); err != nil {
			rows.Close()
			return rep, err
		}
		if session == "" {
			continue
		}
		var seg struct {
			Warnings []string `json:"warnings"`
		}
		_ = json.Unmarshal([]byte(segJSON), &seg)
		recs = append(recs, warnRec{session: session, ts: ts, warnings: seg.Warnings})
	}
	rows.Close()
	for _, r := range recs {
		for _, w := range r.warnings {
			path := pathInWarningRe.FindString(w)
			if path == "" {
				rep.Unknown++
				continue
			}
			rep.Mapped++
			if !sessionWrotePath(d, r.session, r.ts, path) {
				rep.Avoided++
			}
		}
	}
	if rep.Mapped > 0 {
		rep.Avoidance = float64(rep.Avoided) / float64(rep.Mapped)
	}
	return rep, nil
}

// sessionWrotePath 注入 ts 后该 session 前 5 条 file_op 内是否对 path 写操作。
func sessionWrotePath(d *sql.DB, session, tsISO, path string) bool {
	rows, err := d.Query(`SELECT metadata FROM discussion_log WHERE session_id = ? AND created_at > ? AND metadata LIKE '%file_op%' ORDER BY created_at ASC LIMIT 5`, session, tsISO)
	if err != nil {
		return false
	}
	defer rows.Close()
	type op struct{ Type, RelPath string }
	for rows.Next() {
		var md string
		if err := rows.Scan(&md); err != nil {
			continue
		}
		var wrapper struct {
			FileOp struct {
				Type    string `json:"type"`
				RelPath string `json:"rel_path"`
			} `json:"file_op"`
		}
		if json.Unmarshal([]byte(md), &wrapper) != nil {
			continue
		}
		if writeTypes[wrapper.FileOp.Type] && wrapper.FileOp.RelPath == path {
			return true
		}
	}
	return false
}

// ── M4 ──

func isEmergeEvent(typ string) bool {
	return strings.HasSuffix(typ, "_orphan") || strings.HasSuffix(typ, "_stale_file") ||
		strings.Contains(typ, "untracked") || typ == "mcp_error"
}

// buildM4 近 7 天 unconsumed events 噪音占比（HARNESS §2 M4）。
func buildM4(d *sql.DB) (M4Report, error) {
	var rep M4Report
	cutoff := time.Now().AddDate(0, 0, -7).Format("2006-01-02T15:04:05")
	rows, err := d.Query(`SELECT type FROM events WHERE consumed_by_agent = 0 AND created_at >= ?`, cutoff)
	if err != nil {
		return rep, fmt.Errorf("m4: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var typ string
		if err := rows.Scan(&typ); err != nil {
			return rep, err
		}
		rep.Total++
		if isEmergeEvent(typ) {
			rep.Noise++
		}
	}
	if rep.Total > 0 {
		rep.NoiseRatio = float64(rep.Noise) / float64(rep.Total)
	}
	return rep, nil
}

// ── M5 ──

var m5SegRe = regexp.MustCompile(`segments=file_cut:(\d+) warn:(\d+) act:(\d+) goals:(\d+) guide:(\d+)`)

// buildM5 解析 :153 suppressed 日志行的 segment 截断分布。
func buildM5(logLines []string, since time.Time) M5Report {
	var rep M5Report
	for _, ln := range logLines {
		if !strings.Contains(ln, "reason=char_limit") {
			continue
		}
		ts, _, _, _ := parseSkipLine(ln)
		if !ts.IsZero() && ts.Before(since) {
			continue
		}
		m := m5SegRe.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		rep.SuppressedRequests++
		rep.Segments.FileAssoc += atoi(m[1])
		rep.Segments.Warnings += atoi(m[2])
		rep.Segments.Actions += atoi(m[3])
		rep.Segments.Goals += atoi(m[4])
		rep.Segments.Guidelines += atoi(m[5])
	}
	return rep
}

// ── 日志解析 ──

func readLogLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

var logTsRe = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\]`)

// parseSkipLine 解析 INJECT skip/suppressed 行的时间、agent、session、reason。
func parseSkipLine(ln string) (time.Time, string, string, string) {
	var ts time.Time
	if m := logTsRe.FindStringSubmatch(ln); m != nil {
		ts, _ = time.Parse("2006-01-02 15:04:05", m[1])
	}
	agent := ""
	if m := regexp.MustCompile(`agent=([^\s]+)`).FindStringSubmatch(ln); m != nil {
		agent = m[1]
	}
	session := ""
	if m := regexp.MustCompile(`session=([^\s]+)`).FindStringSubmatch(ln); m != nil {
		session = m[1]
	}
	reason := ""
	if m := regexp.MustCompile(`reason=([^\s]+)`).FindStringSubmatch(ln); m != nil {
		reason = m[1]
	}
	return ts, agent, session, reason
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
