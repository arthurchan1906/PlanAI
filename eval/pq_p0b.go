package eval

// P0b：⑤ 019ff89b 复合标签对象级方向性验证（加深，不承诺精度）
//     + ⑥ 候选→人工确认闭环（10 候选时段，每段读 5-10 条原始记录）。
// SPEC §5 P0b 验收：误报率报告（每类检测点误报数/候选总数，L3 校准层输入）
// + 用户/Claude 抽检 2-3 候选时段三方判定一致率（防 codex 自确认主观性）。
//
// ⑤ 方向性口径（SPEC 形态 7/8）：重复调查 = 同一对象重复读 N 次 + 对象扩展率低；
// 单点死磕 = 修改/检索对象集合高度集中 + 新对象扩展率低。019ff89b 实证：
// LocalVaultStore×43、9h 同文件域打转（232 条 rel_path 记录）。

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ── ⑤ 对象级「加深」方向性 ─────────────────────────────────

// ObjectStat 单对象（文件）访问统计。
type ObjectStat struct {
	File      string    `json:"file"`
	Count     int       `json:"count"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// DeepenWindow 小时窗对象扩展。
type DeepenWindow struct {
	Start         time.Time `json:"start"`
	End           time.Time `json:"end"`
	ActiveObjects int       `json:"active_objects"` // 窗内活跃对象数
	NewObjects    int       `json:"new_objects"`    // 窗内首次出现对象数
	ExpansionRate float64   `json:"expansion_rate"` // 新对象 ÷ 活跃对象
}

// DeepenReport 对象级加深方向性验证报告。
type DeepenReport struct {
	SessionID         string         `json:"session_id"`
	Span              string         `json:"span"`
	ObjectAccesses    int            `json:"object_accesses"`      // 对象访问总次数（去重 per 记录）
	UniqueObjects     int            `json:"unique_objects"`       // 去重对象数
	Top1Concentrate   float64        `json:"top1_concentration"`   // top1 访问 ÷ 总访问
	DomainTop         string         `json:"domain_top"`           // top 领域（首路径段）
	DomainConcentrate float64        `json:"domain_concentration"` // top 领域访问 ÷ 总访问
	TopObjects        []ObjectStat   `json:"top_objects"`          // top 8
	Windows           []DeepenWindow `json:"windows"`
	Verdict           string         `json:"verdict"`
	Evidence          []string       `json:"evidence"`
}

// DetectObjectDeepening 对象级加深方向性验证（P0b ⑤，不承诺精度）。
// 对象 = Record.Tool.Files（post_tool file_path/rel_path 解析产物）。
func DetectObjectDeepening(turns []Turn, sessionID string) *DeepenReport {
	type access struct {
		ts   time.Time
		file string
	}
	var accs []access
	firstSeen := map[string]time.Time{}
	for i := range turns {
		for j := range turns[i].Records {
			r := &turns[i].Records[j]
			for _, f := range r.Tool.Files {
				if f == "" {
					continue
				}
				accs = append(accs, access{ts: r.CreatedAt, file: f})
				if _, ok := firstSeen[f]; !ok {
					firstSeen[f] = r.CreatedAt
				}
			}
		}
	}
	rep := &DeepenReport{SessionID: sessionID}
	if len(accs) == 0 {
		rep.Verdict = "不可判定"
		rep.Evidence = []string{"对象级数据（Record.Tool.Files）为空——session 无 file_path/rel_path 解析产物"}
		return rep
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].ts.Before(accs[j].ts) })
	counts := map[string]int{}
	for _, a := range accs {
		counts[a.file]++
	}
	files := make([]string, 0, len(counts))
	for f := range counts {
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool {
		if counts[files[i]] != counts[files[j]] {
			return counts[files[i]] > counts[files[j]]
		}
		return files[i] < files[j]
	})
	rep.ObjectAccesses = len(accs)
	rep.UniqueObjects = len(files)
	topN := 8
	if len(files) < topN {
		topN = len(files)
	}
	for i := 0; i < topN; i++ {
		f := files[i]
		var last time.Time
		for _, a := range accs {
			if a.file == f && a.ts.After(last) {
				last = a.ts
			}
		}
		rep.TopObjects = append(rep.TopObjects, ObjectStat{File: f, Count: counts[f], FirstSeen: firstSeen[f], LastSeen: last})
	}
	if len(accs) > 0 {
		rep.Top1Concentrate = float64(counts[files[0]]) / float64(len(accs))
		// 领域（首路径段）集中度——019ff89b「同文件域打转」实证口径
		dom := map[string]int{}
		for _, a := range accs {
			dom[domainOf(a.file)]++
		}
		bestDom, bestN := "", 0
		for d, n := range dom {
			if n > bestN {
				bestDom, bestN = d, n
			}
		}
		rep.DomainTop = bestDom
		rep.DomainConcentrate = float64(bestN) / float64(len(accs))
	}
	start, end := accs[0].ts, accs[len(accs)-1].ts
	rep.Span = fmt.Sprintf("%s → %s（%s）", tsFmt(start), tsFmt(end), end.Sub(start).Round(time.Minute))
	for w := start.Truncate(time.Hour); !w.After(end); w = w.Add(time.Hour) {
		wEnd := w.Add(time.Hour)
		active := map[string]bool{}
		for _, a := range accs {
			if !a.ts.Before(w) && a.ts.Before(wEnd) {
				active[a.file] = true
			}
		}
		if len(active) == 0 {
			continue
		}
		newCnt := 0
		for f := range active {
			if fs := firstSeen[f]; !fs.Before(w) && fs.Before(wEnd) {
				newCnt++
			}
		}
		rep.Windows = append(rep.Windows, DeepenWindow{
			Start: w, End: wEnd, ActiveObjects: len(active), NewObjects: newCnt,
			ExpansionRate: float64(newCnt) / float64(len(active)),
		})
	}
	// 方向性判定（启发式，只做方向不承诺精度）
	top1 := rep.TopObjects[0]
	rep.Evidence = append(rep.Evidence,
		fmt.Sprintf("top1 对象 %s 访问 %d 次（占总访问 %.0f%%）", top1.File, top1.Count, rep.Top1Concentrate*100))
	if rep.Top1Concentrate >= 0.15 {
		rep.Evidence = append(rep.Evidence, fmt.Sprintf("对象集合高度集中（top1 ≥ 15%%）——单点死磕（形态 8）方向性成立"))
	}
	if rep.DomainConcentrate >= 0.5 {
		rep.Evidence = append(rep.Evidence, fmt.Sprintf("领域集中度 %.0f%% 落在 %s——同文件域打转（单点死磕形态）方向性成立", rep.DomainConcentrate*100, rep.DomainTop))
	}
	repeatRead := 0
	repeatDur := 0
	for _, o := range rep.TopObjects {
		if o.Count >= 20 && o.LastSeen.Sub(o.FirstSeen) >= 3*time.Hour {
			repeatRead++
			repeatDur = maxInt(repeatDur, int(o.LastSeen.Sub(o.FirstSeen).Hours()))
		}
	}
	if repeatRead > 0 {
		rep.Evidence = append(rep.Evidence, fmt.Sprintf("%d 个对象 3h+ 内重复访问 ≥20 次（最长跨 %dh）——重复调查（形态 7）方向性成立", repeatRead, repeatDur))
	}
	var laterRates []float64
	for _, w := range rep.Windows {
		if w.ExpansionRate <= 0.3 {
			laterRates = append(laterRates, w.ExpansionRate)
		}
	}
	lowExpansion := len(laterRates) > 0 && float64(len(laterRates))/float64(len(rep.Windows)) >= 0.5
	if lowExpansion {
		rep.Evidence = append(rep.Evidence, fmt.Sprintf("%d/%d 小时窗新对象扩展率 ≤ 0.3——对象扩展率低（加深✗）方向性成立", len(laterRates), len(rep.Windows)))
	}
	switch {
	case rep.DomainConcentrate >= 0.5 && repeatRead > 0:
		rep.Verdict = "加深✗ 方向性成立（单点死磕 + 重复调查复合：同文件域打转，对象扩展受限）"
	case rep.Top1Concentrate >= 0.15 && repeatRead > 0 && lowExpansion:
		rep.Verdict = "加深✗ 方向性成立（单点死磕 + 重复调查复合，对象扩展率低）"
	case rep.Top1Concentrate >= 0.15 && (repeatRead > 0 || lowExpansion):
		rep.Verdict = "加深✗ 方向性倾向成立（对象集中 + 重复访问/扩展率低其一）"
	default:
		rep.Verdict = "方向性不成立或样本不足（不可判定）"
	}
	return rep
}

// domainOf 对象所属领域 = 首路径段（相对路径场景如 EncryptDrive/... / app/...）；
// 无斜杠的文件（如 EncryptDrive.xcodeproj）自成一域。
func domainOf(p string) string {
	p = strings.TrimPrefix(p, "/")
	if i := strings.Index(p, "/"); i > 0 {
		return p[:i]
	}
	return p
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ── ⑥ 候选→人工确认闭环 ─────────────────────────────────

// ConfirmWindow 人工确认候选时段（附 5-10 条原始记录）。
type ConfirmWindow struct {
	Detector    string    `json:"detector"`
	SessionID   string    `json:"session_id"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	L1Verdict   string    `json:"l1_verdict"`
	Records     []string  `json:"records"`               // 原始记录人类可读行（≤10 条）
	CodexJudge  string    `json:"codex_judge,omitempty"` // codex 自评 true_positive/false_positive/uncertain
	SpotCheck   bool      `json:"spot_check"`            // 是否抽检时段（用户/Claude 交叉验证）
}

// SelectConfirmWindows 从 P0a2 报告候选中挑选 10 个代表性时段（覆盖 5 个检测点）：
// 死循环×2（c0ad2534）、hint_responded×2（c0ad2534+01a013f3）、hint_missed×1（c0ad2534）、
// 静态可核对×1（01a013f3）、重复验证点×2（01a013f3 两自然轮）、自建记录利用×2（01a013f3+c0ad2534）。
// 抽检 3 时段（SpotCheck）：死循环 15:00、重复验证 8/19 轮、自建记录 15:32 锚点。
func SelectConfirmWindows(reports map[string]*P0a2Report, allTurns map[string][]Turn) []ConfirmWindow {
	var out []ConfirmWindow
	add := func(det, sid string, from, to time.Time, verdict string, spot bool) {
		out = append(out, ConfirmWindow{
			Detector: det, SessionID: sid,
			WindowStart: from, WindowEnd: to, L1Verdict: verdict,
			Records:   windowRecords(allTurns[sid], from, to, 10),
			SpotCheck: spot,
		})
	}
	// 1. deadloop_no_aipm（c0ad2534）前 2
	if rp := reports["c0ad2534"]; rp != nil {
		n := 0
		for _, c := range rp.Proactive {
			if c.SceneKind != "deadloop_no_aipm" {
				continue
			}
			if n >= 2 {
				break
			}
			add("死循环时段该用未用", "c0ad2534", c.SceneAt, c.SceneAt.Add(time.Duration(c.WindowMin)*time.Minute),
				fmt.Sprintf("%s（L1：该用未用）", c.SceneSnippet), n == 0)
			n++
		}
		// 2. hint_responded（c0ad2534 第 1）+ hint_missed（c0ad2534 第 1）
		for _, c := range rp.Proactive {
			if c.SceneKind == "hint_responded" {
				add("用户提示后响应", "c0ad2534", c.SceneAt, c.SceneAt.Add(time.Duration(c.WindowMin)*time.Minute),
					fmt.Sprintf("hint_responded aipm=%d（L1：窗口内已响应）", c.SelfRetrieval), false)
				break
			}
		}
		for _, c := range rp.Proactive {
			if c.SceneKind == "hint_missed" {
				add("用户提示后响应（missed）", "c0ad2534", c.SceneAt, c.SceneAt.Add(time.Duration(c.WindowMin)*time.Minute),
					fmt.Sprintf("hint_missed aipm=%d（L1：窗口内零检索）", c.SelfRetrieval), false)
				break
			}
		}
		// 6b. 自建记录利用（c0ad2534）
		if len(rp.SelfRecords) > 0 {
			c := rp.SelfRecords[0]
			to := c.FirstConsultAt
			if to.IsZero() {
				to = c.CreatedAt.Add(60 * time.Minute)
			}
			add("自建记录利用", "c0ad2534", c.CreatedAt, to,
				fmt.Sprintf("%s 后工作 %d 条零 aipm 检索（延迟 %dmin）", c.Kind, c.WorkRecords, c.DelayMin), false)
		}
	}
	if rp := reports["01a013f3"]; rp != nil {
		// 2b. hint_responded（01a013f3 第 1）
		for _, c := range rp.Proactive {
			if c.SceneKind == "hint_responded" {
				add("用户提示后响应", "01a013f3", c.SceneAt, c.SceneAt.Add(time.Duration(c.WindowMin)*time.Minute),
					fmt.Sprintf("hint_responded aipm=%d（L1：窗口内已响应）", c.SelfRetrieval), false)
				break
			}
		}
		// 4. 静态可核对（01a013f3 第 1 真机轮次）
		if len(rp.StaticChecks) > 0 {
			c := rp.StaticChecks[0]
			add("静态可核对", "01a013f3", c.RoundAt.Add(-time.Duration(c.WindowMin)*time.Minute), c.RoundAt,
				"真机轮次前窗口内无 SDK 头文件核对（L1：静态核对缺失）", false)
		}
		// 5. 重复验证点（01a013f3 最大 episode + 8/20 轮）
		best := -1
		for i := range rp.RepeatedVerif {
			if best < 0 || rp.RepeatedVerif[i].Count > rp.RepeatedVerif[best].Count {
				best = i
			}
		}
		if best >= 0 {
			c := rp.RepeatedVerif[best]
			add("重复验证点", "01a013f3", c.EpisodeStart, c.EpisodeEnd,
				fmt.Sprintf("同验证点重复 %d 次请求（L1：无 fix commit/休眠间隔）", c.Count), true)
			// 跨日轮次（8/19 vs 8/20 两个自然验证轮）另取 1 个代表
			second := -1
			for i := range rp.RepeatedVerif {
				if i == best {
					continue
				}
				if rp.RepeatedVerif[i].EpisodeStart.Day() == rp.RepeatedVerif[best].EpisodeStart.Day() {
					continue
				}
				if second < 0 || rp.RepeatedVerif[i].Count > rp.RepeatedVerif[second].Count {
					second = i
				}
			}
			if second >= 0 {
				c2 := rp.RepeatedVerif[second]
				add("重复验证点", "01a013f3", c2.EpisodeStart, c2.EpisodeEnd,
					fmt.Sprintf("同验证点重复 %d 次请求（跨日轮次，L1：无 fix commit/休眠间隔）", c2.Count), false)
			}
		}
		// 6a. 自建记录利用（01a013f3 锚点 15:32 record_bug）
		if len(rp.SelfRecords) > 0 {
			c := rp.SelfRecords[0]
			to := c.FirstConsultAt
			if to.IsZero() {
				to = c.CreatedAt.Add(60 * time.Minute)
			}
			add("自建记录利用", "01a013f3", c.CreatedAt, to,
				fmt.Sprintf("%s 后工作 %d 条 17:29 才首次检索（延迟 %dmin）", c.Kind, c.WorkRecords, c.DelayMin), true)
		}
	}
	return out
}

// windowRecords 抽取 [from, to] 内的原始记录（≤maxN 条，人类可读行）。
func windowRecords(turns []Turn, from, to time.Time, maxN int) []string {
	var rows []string
	for i := range turns {
		for j := range turns[i].Records {
			r := &turns[i].Records[j]
			if r.CreatedAt.Before(from) || r.CreatedAt.After(to) {
				continue
			}
			rows = append(rows, fmt.Sprintf("%s [%s] %s｜%s", tsFmt(r.CreatedAt), r.Role, toolLabel(r.Tool), truncate(r.Content, 100)))
			if len(rows) >= maxN {
				return rows
			}
		}
	}
	return rows
}

func toolLabel(t ToolRecord) string {
	switch {
	case t.Tool == "llm_message":
		return "assistant"
	case t.Tool == "mcp_aipm_other":
		return t.Command
	case t.Tool == "unknown" && strings.TrimSpace(t.Command) == "":
		return "text"
	default:
		return t.Tool
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ── 报告聚合 ─────────────────────────────────────────

// P0bReport P0b 报告（⑤ 加深方向性 + ⑥ 候选确认闭环）。
type P0bReport struct {
	Deepen         *DeepenReport   `json:"deepen"`
	Confirm        []ConfirmWindow `json:"confirm_windows"`
	SpotCheckCount int             `json:"spot_check_count"`
	Annotations    []string        `json:"annotations,omitempty"`
}

// BuildP0bReport P0b 报告聚合。deepenSession = 019ff89b（对象级加深）；
// confirmSessions = c0ad2534,01a013f3（P0a2 候选 → 10 时段人工确认）。
func BuildP0bReport(db *sql.DB, deepenSession string, confirmSessions []string) (*P0bReport, error) {
	rep := &P0bReport{}
	if deepenSession != "" {
		turns, err := BuildTurns(db, deepenSession)
		if err != nil {
			return nil, fmt.Errorf("deepen turns: %w", err)
		}
		rep.Deepen = DetectObjectDeepening(turns, deepenSession)
	}
	reports := map[string]*P0a2Report{}
	allTurns := map[string][]Turn{}
	for _, sid := range confirmSessions {
		turns, err := BuildTurns(db, sid)
		if err != nil {
			return nil, fmt.Errorf("confirm turns %s: %w", sid, err)
		}
		allTurns[shortID(sid)] = turns
		rp, err := BuildP0a2Report(db, sid, "")
		if err != nil {
			return nil, fmt.Errorf("p0a2 %s: %w", sid, err)
		}
		reports[shortID(sid)] = rp
	}
	rep.Confirm = SelectConfirmWindows(reports, allTurns)
	rep.SpotCheckCount = 0
	for i := range rep.Confirm {
		if rep.Confirm[i].SpotCheck {
			rep.SpotCheckCount++
		}
	}
	if len(rep.Confirm) < 10 {
		rep.Annotations = append(rep.Annotations, fmt.Sprintf("候选时段 %d/10（样本不足：某检测点候选少于预期）", len(rep.Confirm)))
	}
	return rep, nil
}

// FormatP0bHuman P0b 人类可读输出。
func FormatP0bHuman(rep *P0bReport) string {
	var b strings.Builder
	b.WriteString("P0b 报告（对象级加深方向性 + 候选→人工确认闭环）\n")
	if rep.Deepen != nil {
		d := rep.Deepen
		b.WriteString("\n── ⑤ 对象级「加深」方向性（019ff89b 复合标签）──\n")
		b.WriteString(fmt.Sprintf("  时段 %s\n", d.Span))
		b.WriteString(fmt.Sprintf("  对象访问 %d 次 / 去重对象 %d 个\n", d.ObjectAccesses, d.UniqueObjects))
		b.WriteString(fmt.Sprintf("  top1 集中度 %.0f%%：%s\n", d.Top1Concentrate*100, top1Name(d)))
		if d.DomainTop != "" {
			b.WriteString(fmt.Sprintf("  领域集中度 %.0f%%：%s\n", d.DomainConcentrate*100, d.DomainTop))
		}
		b.WriteString("  top 对象：\n")
		for _, o := range d.TopObjects {
			b.WriteString(fmt.Sprintf("    %-60s %3d 次（%s → %s）\n", shortPath(o.File), o.Count, tsClock(o.FirstSeen), tsClock(o.LastSeen)))
		}
		b.WriteString("  小时窗扩展率（新对象/活跃对象）：\n")
		for _, w := range d.Windows {
			b.WriteString(fmt.Sprintf("    %s %02d 个活跃 / %02d 个新（%.0f%%）\n", tsClock(w.Start), w.ActiveObjects, w.NewObjects, w.ExpansionRate*100))
		}
		b.WriteString(fmt.Sprintf("  结论：%s\n", d.Verdict))
		for _, e := range d.Evidence {
			b.WriteString(fmt.Sprintf("    - %s\n", e))
		}
	}
	b.WriteString("\n── ⑥ 候选→人工确认闭环（10 候选时段，每段读原始记录）──\n")
	for i, c := range rep.Confirm {
		mark := "  "
		if c.SpotCheck {
			mark = "抽"
		}
		b.WriteString(fmt.Sprintf("  %s[%02d] %s（%s）%s → %s\n", mark, i+1, c.Detector, shortID(c.SessionID), tsClock(c.WindowStart), tsClock(c.WindowEnd)))
		b.WriteString(fmt.Sprintf("       L1：%s\n", c.L1Verdict))
		for _, r := range c.Records {
			b.WriteString(fmt.Sprintf("       · %s\n", r))
		}
		if c.CodexJudge != "" {
			b.WriteString(fmt.Sprintf("       codex 自评：%s\n", c.CodexJudge))
		}
	}
	return b.String()
}

func top1Name(d *DeepenReport) string {
	if len(d.TopObjects) == 0 {
		return ""
	}
	return shortPath(d.TopObjects[0].File)
}

// shortPath 裁剪超长路径（对象展示用）。
func shortPath(p string) string {
	if len(p) <= 60 {
		return p
	}
	if i := strings.LastIndex(p, "/"); i >= 0 {
		base := p[i+1:]
		if len(base) > 20 {
			base = base[len(base)-20:]
		}
		return "…/" + base
	}
	return p[:57] + "…"
}
