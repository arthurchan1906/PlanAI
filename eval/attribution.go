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
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// AttributionReport 归因报告（HARNESS M1-M5）。
type AttributionReport struct {
	Since       string              `json:"since"`
	WriteErr    int                 `json:"write_err"` // inject_log 写库失败次数（直接证据告警）
	ByAgent     map[string]AgentM12 `json:"by_agent"`  // M1/M2 按 agent
	M3          M3Report            `json:"m3"`
	M4          M4Report            `json:"m4"`
	M5          M5Report            `json:"m5"`
	Annotations []string            `json:"annotations,omitempty"` // 规格注记（HARNESS §2：准实验/含噪/口径边界）
}

var curProjectOnce sync.Once
var curProject string

// currentProjectPath 当前项目根（M1a 对账过滤，8/26）：与 proxy 写库目标一致
// （cwd 向上找 .pmai；proxy 侧日志 project= 也按 pmdb.FindPath 语义打标）。
func currentProjectPath() string {
	curProjectOnce.Do(func() {
		if cwd, err := os.Getwd(); err == nil {
			for dir := cwd; dir != "/" && dir != "."; {
				if info, err := os.Stat(filepath.Join(dir, ".pmai")); err == nil && info.IsDir() {
					curProject = dir
					return
				}
				parent := filepath.Dir(dir)
				if parent == dir {
					break
				}
				dir = parent
			}
		}
	})
	return curProject
}

// AgentM12 单个 agent 的 M1（注入覆盖率）与 M2（文件命中率）指标。
type AgentM12 struct {
	M1 M1Report `json:"m1"`
	M2 M2Report `json:"m2"`
}

// M1Report（8/18 v2 口径，HARNESS §2）：
// M1a 注入观测完整性（对账）= Injected/LogInject（:148 日志行 vs 表行，期望 1.0）；
// M1b 注入新鲜度（参考）= Injected/(Injected+SameContent)（排除 no_summary）。
// WriteErr 为直接证据告警（inject_log 写库失败），非间接推断。
type M1Report struct {
	Injected    int     `json:"injected"`     // inject_log 行数（含 suppressed=1）
	LogInject   int     `json:"log_inject"`   // 日志侧 :148 注入行数（对账分母）
	Reconcile   float64 `json:"reconcile"`    // M1a = injected/log_inject，期望 1.0
	SameContent int     `json:"same_content"` // 日志侧 reason=same_content
	Freshness   float64 `json:"freshness"`    // M1b = injected/(injected+same_content)
	NoSummary   int     `json:"no_summary"`   // 报告但不进任何分母
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
	events         []injectEvent // 该 session 窗口内全部注入事件（按 ts 升序）
}

// injectEvent 一次注入：时刻 + 注入文件集 + 是否被 cap 裁剪（分层依据）。
type injectEvent struct {
	ts         time.Time
	files      map[string]bool
	suppressed int
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

	// M1a 对账窗口：从 inject_log 最早行开始（观测层启用时间）。此前的
	// :148 日志无对应表行（表 8/18 才启用），计入分母会造成系统性误报。
	var tableMinTS string
	if err := d.QueryRow(`SELECT MIN(ts) FROM inject_log WHERE ts >= ?`, sinceISO).Scan(&tableMinTS); err != nil {
		tableMinTS = ""
	}

	// 日志侧：:148 注入行（M1a 对账分母）、same_content / no_summary（对照组）、
	// inject_log write_err（写库故障直接证据）
	m2Ctl := map[string]map[string]time.Time{} // agent → session → last skip ts
	for _, ln := range logLines {
		if !strings.Contains(ln, "[INJECT]") {
			continue
		}
		// 写库失败告警（直接证据，非间接推断——8/18 v2 口径）
		if strings.Contains(ln, "inject_log write_err=") {
			// 测试进程写临时库失败的噪音不计入生产观测（8/18 实测：proxy 包
			// 测试未隔离前，Test* 临时目录的 "PMAI database not found" 全部
			// 落在生产日志里）。Bootstrap 修复后不再产生，历史行在此过滤。
			if strings.Contains(ln, os.TempDir()) {
				continue
			}
			ts, _, _, _ := parseSkipLine(ln)
			if !ts.IsZero() && !ts.Before(since) {
				rep.WriteErr++
			}
			continue
		}
		// :148 注入行（M1a 对账分母）：仅统计真实注入明细行（agent= + hash=）。
		// 排除 "inject agent=... source=guidelines_only" 标记行——它在 dedup 之前
		// 打印，对 same_content 跳过也出现，计入分母会让 guidelines_only 流量
		// 的对账系统性虚低（8/18 实测 codex 分母翻倍）。
		if strings.Contains(ln, "[INJECT] agent=") && strings.Contains(ln, " hash=") {
			ts, agent, _, _ := parseSkipLine(ln)
			if ts.IsZero() || ts.Before(since) || agent == "" {
				continue
			}
			// 8/26 M1a 口径修复：日志全局共享（~/.aipmc/logs/aipmc.log 含所有项目的
			// 注入行，8/19-8/24 实测 EncryptDrive 注入行混入分母致 aipmc 对账 12-14%）。
			// 8/26 起 proxy 日志带 project= 绝对路径，分母只计当前项目；历史无
			// project= 的行不过滤（8/18/8/25 纯净窗口对账正常，口径注记在规格）。
			if proj := projectFromLine(ln); proj != "" && proj != currentProjectPath() {
				continue
			}
			// 观测层启用前的历史 :148 行不参与对账（无对应表行）
			if tableMinTS != "" && ts.Format("2006-01-02T15:04:05") < tableMinTS {
				continue
			}
			a := rep.ByAgent[agent]
			a.M1.LogInject++
			rep.ByAgent[agent] = a
			continue
		}
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
		if a.M1.LogInject > 0 {
			a.M1.Reconcile = float64(a.M1.Injected) / float64(a.M1.LogInject)
		}
		freshDenom := a.M1.Injected + a.M1.SameContent
		if freshDenom > 0 {
			a.M1.Freshness = float64(a.M1.Injected) / float64(freshDenom)
		}
		rep.ByAgent[agent] = a
	}

	// ── M2: 注入组（full/partial）+ 对照组（same_content/no_summary）──
	injRows, err := d.Query(`SELECT agent, session_id, ts, segments_json, suppressed FROM inject_log WHERE ts >= ?`, sinceISO)
	if err != nil {
		return nil, fmt.Errorf("m2 inject_log: %w", err)
	}
	eventsByKey := map[string][]injectEvent{} // agent|session → 全部注入事件
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
		files := map[string]bool{}
		for _, f := range seg.FileAssoc {
			files[fileAssocPath(f)] = true
		}
		eventsByKey[key] = append(eventsByKey[key], injectEvent{ts: t, files: files, suppressed: suppressed})
	}
	injRows.Close()

	// 每 session：末次注入的 suppressed 决定 full/partial 分层（水位语义）；
	// 命中判定用全部注入事件（修正：仅末次文件集会漏早期注入——HARNESS §2
	// 「注入时刻之后引用注入文件」指该 session 注入过的全部文件）。
	var fullSess, partialSess []m2Session
	injFiles := map[string]map[string]bool{} // agent|session → 已注入文件 union（对照组基线推导）
	for key, evs := range eventsByKey {
		last := evs[0]
		union := map[string]bool{}
		for _, ev := range evs {
			if ev.ts.After(last.ts) {
				last = ev
			}
			for f := range ev.files {
				union[f] = true
			}
		}
		if len(union) == 0 {
			continue // 无文件关联的注入不进 M2 分母（口径：注入含 ≥1 个文件）
		}
		sort.Slice(evs, func(i, j int) bool { return evs[i].ts.Before(evs[j].ts) })
		parts := strings.SplitN(key, "|", 2)
		ms := m2Session{agent: parts[0], session: parts[1], events: evs}
		injFiles[key] = union
		if last.suppressed == 0 {
			fullSess = append(fullSess, ms)
		} else {
			partialSess = append(partialSess, ms)
		}
	}
	fullHit := computeM2Hit(d, fullSess)
	partialHit := computeM2Hit(d, partialSess)

	// 对照组：same_content 复用注入历史文件集（基线=已见过）；no_summary 语义=无记忆（活跃基线）
	sameCtl := computeCtlHit(d, m2Ctl, injFiles)
	noSumCtl := computeCtlHitNoSummary(d, logLines, since)

	// 归并到 ByAgent
	for agent, a := range rep.ByAgent {
		a.M2.FullInject = groupFor(fullHit, agent)
		a.M2.PartialInject = groupFor(partialHit, agent)
		a.M2.SameContentCtl = groupFor(sameCtl, agent)
		a.M2.NoSummaryCtl = groupFor(noSumCtl, agent)
		rep.ByAgent[agent] = a
	}

	// 规格注记（HARNESS §2 强制项：准实验/含噪/对照组语义，缺失会让输出被误读）
	rep.Annotations = []string{
		"M1a 对账口径（8/26）：日志全局共享（~/.aipmc/logs 混入其他项目注入行，8/19-24 实测跨项目污染致对账 12-14%）——8/26 起 proxy 日志带 project=，分母按当前项目过滤；历史无 project= 行不过滤，对账仅对单项目纯净窗口（8/18/8/25）有效",
		"M2 为观察性准实验（非随机对照）：注入/对照组差值反映关联而非因果",
		"M2 命中判定（8/25 修订）：fileAssoc 注记串按 ' → ' 拆路径 + op 路径精确/basename/后缀三级匹配；写/读引用均计",
		"M2 same_content 对照组基线=已见过（此前注入过同内容）：有注入历史时按已注入文件判定，无历史时降级为活跃基线（任意文件引用）",
		"M3 为可映射子集（headless 无 LLM 语义映射），不可映射风险提示记 unknown 单独列示；数据源 = warnings + actionItems（8/25 修订，生产注入端路径风险提示在 actionItems）",
		"M3 语义局限（8/25 注记）：当前 actionItems 的行为语义是「建 task 跟踪」（热点/阻塞提示），非「禁止写该文件」——回避率数值含义有限，宜解读为「对告警文件继续操作的比例」",
		"M3 same_content/无后续注入窗口注记：对照组命中率偏低可能因抑制多发生在会话尾部（天然无后续操作），非仅匹配问题",
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

// FormatHuman 输出人类可读报告（对齐 metrics.go printRow 风格：名字/值/目标/✅❌）。
// 与 JSON 输出并存（main.go eval 双输出，HARNESS S4 核验项 4）。
func FormatHuman(rep *AttributionReport) string {
	var b strings.Builder
	row := func(name, val, target string, ok bool) {
		mark := "❌"
		if ok {
			mark = "✅"
		}
		fmt.Fprintf(&b, "%-26s %-14s 目标 %-10s %s\n", name, val, target, mark)
	}
	pct := func(v float64) string { return fmt.Sprintf("%.1f%%", v*100) }

	fmt.Fprintf(&b, "=== M1a 对账完整性（期望 1.0）===\n")
	for agent, a := range rep.ByAgent {
		ok := a.M1.LogInject == 0 || a.M1.Reconcile >= 1.0
		row("M1a "+agent, fmt.Sprintf("注入%d/日志%d %s", a.M1.Injected, a.M1.LogInject, pct(a.M1.Reconcile)), "1.0", ok)
	}
	row("M1a write_err", fmt.Sprint(rep.WriteErr), "0", rep.WriteErr == 0)

	fmt.Fprintf(&b, "\n=== M1b 新鲜度（参考，不判定）===\n")
	for agent, a := range rep.ByAgent {
		row("M1b "+agent, fmt.Sprintf("注入%d same_content=%d %s", a.M1.Injected, a.M1.SameContent, pct(a.M1.Freshness)), "参考", true)
	}

	fmt.Fprintf(&b, "\n=== M2 文件命中率（按 session）===\n")
	for agent, a := range rep.ByAgent {
		g := func(g M2Group) string {
			if g.Sessions == 0 {
				return "-"
			}
			return fmt.Sprintf("%d/%d(%.0f%%)", g.HitSessions, g.Sessions, g.HitRate*100)
		}
		row("M2 "+agent, fmt.Sprintf("full=%s partial=%s same=%s nosum=%s",
			g(a.M2.FullInject), g(a.M2.PartialInject), g(a.M2.SameContentCtl), g(a.M2.NoSummaryCtl)), "参考", true)
	}

	fmt.Fprintf(&b, "\n=== M3 警告回避 ===\n")
	row("M3", fmt.Sprintf("mapped=%d avoided=%d unknown=%d", rep.M3.Mapped, rep.M3.Avoided, rep.M3.Unknown), "参考", true)

	fmt.Fprintf(&b, "\n=== M4 事件信噪比（近 7 天，目标 <50%%）===\n")
	row("M4", fmt.Sprintf("noise=%d/%d", rep.M4.Noise, rep.M4.Total), "<50%", rep.M4.Total == 0 || rep.M4.NoiseRatio < 0.5)

	fmt.Fprintf(&b, "\n=== M5 截断分布（:153）===\n")
	row("M5", fmt.Sprintf("suppressed=%d", rep.M5.SuppressedRequests), "参考", true)
	fmt.Fprintf(&b, "  segments: file_cut=%d warn=%d act=%d goals=%d guide=%d\n",
		rep.M5.Segments.FileAssoc, rep.M5.Segments.Warnings, rep.M5.Segments.Actions, rep.M5.Segments.Goals, rep.M5.Segments.Guidelines)
	if len(rep.Annotations) > 0 {
		fmt.Fprintf(&b, "\n=== 注记（HARNESS §2）===\n")
		for _, a := range rep.Annotations {
			fmt.Fprintf(&b, "  - %s\n", a)
		}
	}
	return b.String()
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
		if injectHit(d, s.session, s.events) {
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

// injectHit：任一注入事件 (t,F) 之后该 session 引用了 F 中任一文件即命中。
// 与「仅末次注入水位」的区别：早期注入的文件集不被末次注入覆盖丢失
// （8/18 攻击性审核修正，HARNESS §2「注入时刻之后引用注入文件」）。
func injectHit(d *sql.DB, session string, events []injectEvent) bool {
	ops := sessionFileOps(d, session)
	for _, ev := range events {
		for _, op := range ops {
			if op.ts.After(ev.ts) && fileHit(ev.files, op.path) {
				return true
			}
		}
	}
	return false
}

// fileAssocPath 从注入 fileAssoc 元素提取纯路径（8/25 D1 修复）：
// 生产格式是注记串 "agent/session.go → task-xxx (done, P0) task-xxx..."（resolveFileContext
// 故意拼接「文件→task 关联」），精确 map 查找恒 miss（真实库 241/241 注记串、M2 恒 0%）。
// 取 " → " 前段并 trim；裸路径（旧格式/夹具）原样返回。
func fileAssocPath(f string) string {
	if i := strings.Index(f, " → "); i > 0 {
		return strings.TrimSpace(f[:i])
	}
	return f
}

// pathsMatch 两级路径归一化匹配（精确 / basename / 后缀）。
// 真实数据形态：警告提取为 basename（"discussion_test.go"）、写操作为带目录
// （"discussion/discussion_test.go"）或绝对路径——精确匹配恒 miss。
func pathsMatch(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if path.Base(a) == path.Base(b) {
		return true
	}
	return strings.HasSuffix(b, "/"+a) || strings.HasSuffix(a, "/"+path.Base(b))
}

// fileHit 判定 op 解析路径是否命中注入文件集（精确 / basename / 后缀三级匹配）。
func fileHit(files map[string]bool, opPath string) bool {
	if files[opPath] {
		return true
	}
	for f := range files {
		if pathsMatch(f, opPath) {
			return true
		}
	}
	return false
}

// computeCtlHit same_content 对照组：命中 = 抑制 ts 之后引用了「已注入过的文件」
// （基线=已见过，从窗口内 inject_log 推导该 session 的注入历史文件集）；
// 无注入历史的 session 降级为活跃基线（任意文件引用）——两种语义在注记中显式说明。
func computeCtlHit(d *sql.DB, ctl map[string]map[string]time.Time, injFiles map[string]map[string]bool) map[string]m2Hit {
	hits := map[string]m2Hit{}
	for agent, sessions := range ctl {
		for session, ts := range sessions {
			h := hits[agent]
			h.sessions++
			ops := sessionFileOps(d, session)
			if referencedAny(ops, ts, injFiles[agent+"|"+session]) {
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
		ops := sessionFileOps(d, session)
		if referencedAny(ops, ts, nil) {
			h.hitSessions++
		}
		hits[agent] = h
	}
	return hits
}

type fileOp struct {
	ts   time.Time
	path string
}

// isWriteTool 判定工具名是否为写操作（大小写不敏感：normalizeToolName default 保留原大小写，
// 如 Create/NewFile）。writeTypes（edit/create/delete/rename/append）之外补 write/newfile/
// new_file/multiedit/patch 等变体。
func isWriteTool(t string) bool {
	switch strings.ToLower(t) {
	case "edit", "create", "delete", "rename", "append", "write", "newfile", "new_file", "multiedit", "patch":
		return true
	}
	return false
}

// opPaths 从一条 metadata 提取引用路径（M2 用，任意类型含读：注入文件被后续 read/edit 引用均计命中）。
// 多格式兼容：post_tool 平铺（rel_path/file_path + tool_name）→ ParseToolRecord 归一化；
// file_op 嵌套旧格式（claude）兜底。
func opPaths(metadata string) []string {
	rec := ParseToolRecord("", metadata)
	if len(rec.Files) > 0 {
		return rec.Files
	}
	var wrapper struct {
		FileOp struct {
			RelPath string `json:"rel_path"`
		} `json:"file_op"`
	}
	if json.Unmarshal([]byte(metadata), &wrapper) == nil && wrapper.FileOp.RelPath != "" {
		return []string{wrapper.FileOp.RelPath}
	}
	return nil
}

// writeOpPaths 从一条 metadata 提取写操作路径（M3 用，8/18 Claude 审核 + codex 实测）：
// 生产主流 post_tool（codex/cursor）无 file_op 键——复用 S1 ParseToolRecord 归一化
// （顶层 rel_path/file_path + tool_name → edit/write 等）；file_op 嵌套旧格式（claude）兜底；
// read/bash/mcp 等非写操作返回 nil。
func writeOpPaths(metadata string) []string {
	rec := ParseToolRecord("", metadata)
	if isWriteTool(rec.Tool) {
		return rec.Files
	}
	// codex 写操作经 Bash 执行 apply_patch/sed -i/重定向（8/25 Claude 审核 + codex 实测）：
	// tool_name=Bash 被写过滤器误挡，但 hook 已打标 source=bash_heuristic + type=edit 等
	// 写类型（read/stage 不得误判）；unverified（退出码非 0，写未发生）排除；
	// ParseToolRecord 已把顶层 rel_path/rel_paths 并入 rec.Files。
	var ph struct {
		Source string `json:"source"`
		Type   string `json:"type"`
	}
	if rec.Tool == "bash" && len(rec.Files) > 0 &&
		json.Unmarshal([]byte(metadata), &ph) == nil &&
		ph.Source == "bash_heuristic" && isWriteTool(ph.Type) {
		return rec.Files
	}
	var wrapper struct {
		FileOp struct {
			Type    string `json:"type"`
			RelPath string `json:"rel_path"`
		} `json:"file_op"`
	}
	if json.Unmarshal([]byte(metadata), &wrapper) == nil && isWriteTool(wrapper.FileOp.Type) && wrapper.FileOp.RelPath != "" {
		return []string{wrapper.FileOp.RelPath}
	}
	return nil
}

// sessionFileOps 一次查询该 session 全部可解析出路径的引用调用（多格式兼容，任意类型）。
func sessionFileOps(d *sql.DB, session string) []fileOp {
	if session == "" {
		return nil
	}
	rows, err := d.Query(`SELECT metadata, created_at FROM discussion_log WHERE session_id = ? AND metadata != ''`, session)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ops []fileOp
	for rows.Next() {
		var md, ts string
		if err := rows.Scan(&md, &ts); err != nil {
			continue
		}
		for _, p := range opPaths(md) {
			t, _ := time.Parse("2006-01-02T15:04:05", ts)
			ops = append(ops, fileOp{ts: t, path: p})
		}
	}
	return ops
}

// referencedAny：ops 中 after 之后的 file_op；files==nil 时任意命中（活跃基线），
// files 非空时须命中集合内文件（精确语义）。
func referencedAny(ops []fileOp, after time.Time, files map[string]bool) bool {
	for _, op := range ops {
		if !op.ts.After(after) {
			continue
		}
		if files == nil {
			return true
		}
		if fileHit(files, op.path) {
			return true
		}
	}
	return false
}

// ── M3 ──

var pathInWarningRe = regexp.MustCompile(`[A-Za-z0-9_\-./]+\.(go|js|ts|py|rs|java|rb|c|cpp|h|hpp|swift|kt|scala|css|html|vue|svelte|sql|sh|yaml|yml|toml|json|md)\b`)

// buildM3 警告回避率：注入的风险提示（warnings + actionItems，8/25 D2 修复——生产注入端
// 把路径风险提示放在 actionItems：⚠️ 热点文件/阻塞提示，warnings 恒空）含可映射路径 →
// 注入后该 session 是否对该路径
// 发生写操作（窗口 = 注入 ts 至该 session 下一次注入 ts，8/25 修复：原 LIMIT 5 +
// file_op 嵌套假设丢失 post_tool 写操作与第 6+ 条 → 回避率虚高；全窗口会让更晚的
// 无关写也计入「未回避」，方向失真，故取近邻注入窗口）；未写=回避。
// 不可映射路径记 unknown（不进分母）。
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
		actions  []string
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
			Warnings    []string `json:"warnings"`
			ActionItems []string `json:"actionItems"`
		}
		_ = json.Unmarshal([]byte(segJSON), &seg)
		recs = append(recs, warnRec{session: session, ts: ts, warnings: seg.Warnings, actions: seg.ActionItems})
	}
	rows.Close()
	for _, r := range recs {
		for _, w := range append(append([]string{}, r.warnings...), r.actions...) {
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

// sessionWrotePath 注入 ts 后至该 session 下一次注入 ts 之间是否对 path 发生写操作
// （多格式兼容；无下一次注入则到分析窗口末）。
func sessionWrotePath(d *sql.DB, session, tsISO, path string) bool {
	var windowEnd string
	if err := d.QueryRow(`SELECT COALESCE(MIN(ts),'') FROM inject_log WHERE session_id = ? AND ts > ?`, session, tsISO).Scan(&windowEnd); err != nil {
		windowEnd = ""
	}
	q := `SELECT metadata FROM discussion_log WHERE session_id = ? AND created_at > ? AND metadata != ''`
	args := []any{session, tsISO}
	if windowEnd != "" {
		q += ` AND created_at < ?`
		args = append(args, windowEnd)
	}
	q += ` ORDER BY created_at ASC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var md string
		if err := rows.Scan(&md); err != nil {
			continue
		}
		for _, p := range writeOpPaths(md) {
			if pathsMatch(path, p) {
				return true
			}
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

// projectFromLine 从 [INJECT] 日志行提取 project= 值（8/26 起 proxy 打标；历史行无）。
func projectFromLine(ln string) string {
	if m := regexp.MustCompile(`project=([^\s]+)`).FindStringSubmatch(ln); m != nil {
		return m[1]
	}
	return ""
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
