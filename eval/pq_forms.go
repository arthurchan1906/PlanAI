package eval

// P1a 形态 5-10 L1 操作化扫描器（PROCESS_QUALITY_SPEC §2.1 形态分类学 A 轴，P1 全库扫描）。
// 形态 = 特征标签（可多标）：5 静默停滞 / 6 频繁换方案 / 7 重复调查 / 8 单点死磕 /
// 9 验证循环 / 10 伪进展。
//
// 数据分层（§2.1 覆盖限制表）：
//   - 对象级（rel_path，hook session）：recordObjects 用 Tool.Files（rel_path/file_path）。
//   - legacy 降级命令级近似：命令字符串中的文件路径（objPathRe）。
//   - 形态 9 失败信号：legacy = exit_code 强信号；hook = tool_response 文本错误词弱信号
//     （L1 候选 + L2 确认，§2.1）。
// 全部输出候选 + 证据字段；阈值未冻结（§9.6 数据反馈驱动校准），L3 人工确认后回修阈值。

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ── 公共：对象提取 ──

// objPathRe 命令级对象近似：命令字符串中的文件路径（legacy 无 rel_path 时降级，§2.1）。
var objPathRe = regexp.MustCompile(`(?i)[\w./-]+\.(go|swift|mm?|hpp?|py|js|tsx?|json|md|c(?:pp|cc)?|sh|plist|entitlements|pbxproj|ya?ml|txt|sql|db|html?|css)`)

// outsideProjectRe 项目外绝对路径前缀（Claude P1a 审核 C4，实测 01a00d3c 混入
// /tmp/ed_*.go 探针文件）：对象域只保留项目内路径 + 相对路径。
var outsideProjectRe = regexp.MustCompile(`^(/tmp/|/private/tmp/|/var/|/usr/|/System/|/Library/|/bin/|/etc/|/dev/|/opt/)`)

// isProjectObject 对象是否属于项目域（相对路径或非系统目录绝对路径）。
func isProjectObject(o string) bool {
	if !strings.HasPrefix(o, "/") {
		return true
	}
	return !outsideProjectRe.MatchString(o)
}

// validObjToken 命令级近似噪声过滤（实测 01a00d3c 大 session）：patch/命令中的代码引用
// 如 regexp.M、db.C、s.m（单字母扩展 + 无路径分隔符）被 objPathRe 误捕为文件路径 →
// 排除。有路径分隔符（foo/bar.m）或扩展 ≥2 字符（main.go）视为真实路径保留。
func validObjToken(s string) bool {
	i := strings.LastIndex(s, ".")
	if i < 0 {
		return false
	}
	ext := s[i+1:]
	if len(ext) == 1 && !strings.Contains(s, "/") {
		return false
	}
	return true
}

// recordObjects 记录 → 对象集合：对象级（rel_path）优先；legacy bash 命令级路径近似。
func recordObjects(rec *Record) []string {
	if len(rec.Tool.Files) > 0 {
		var out []string
		for _, f := range rec.Tool.Files {
			if isProjectObject(f) {
				out = append(out, f)
			}
		}
		return out
	}
	if rec.Tool.Tool == "bash" && rec.Tool.Command != "" {
		var out []string
		for _, m := range objPathRe.FindAllString(rec.Tool.Command, -1) {
			if validObjToken(m) && isProjectObject(m) {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// objKind 记录 → 对象访问类型：write（edit/write 工具 + apply_patch/sed 写）/ read（读工具 +
// 历史检索 + cat 类读命令）。非对象访问返回空串。
func objKind(rec *Record) string {
	if isWriteTool(rec.Tool.Tool) {
		return "write"
	}
	switch rec.Tool.Tool {
	case "read":
		return "read"
	case "bash":
		lower := strings.ToLower(rec.Tool.Command)
		switch {
		case isHistoryRetrieval(rec.Tool):
			return "read"
		case strings.HasPrefix(lower, "cat "), strings.HasPrefix(lower, "tail "),
			strings.HasPrefix(lower, "head "), strings.HasPrefix(lower, "less "):
			return "read"
		case strings.Contains(lower, "apply_patch"), strings.Contains(lower, "sed -i"):
			return "write"
		}
	}
	return ""
}

// objectAccess 对象访问事件（对象级 = rel_path；legacy = 命令级近似）。
type objectAccess struct {
	Obj  string
	Ts   time.Time
	Kind string // write / read
	Rec  int    // 记录序号（转向以记录为单位计数，避免同记录多 token 互切虚高）
	Turn int    // 回合序号（用户消息分段：跨段切换是响应指令，不计入病态转向，C3）
}

// collectObjectAccesses 从回合流构建对象访问序列（edit/write + 读 + 历史检索 + 写 bash）。
func collectObjectAccesses(turns []Turn) []objectAccess {
	var out []objectAccess
	seen := map[string]bool{}
	recIdx := 0
	for i := range turns {
		for j := range turns[i].Records {
			rec := &turns[i].Records[j]
			kind := objKind(rec)
			if kind == "" {
				continue
			}
			for _, o := range recordObjects(rec) {
				if !seen[o] {
					seen[o] = true
				}
				out = append(out, objectAccess{Obj: o, Ts: rec.CreatedAt, Kind: kind, Rec: recIdx, Turn: i})
			}
			recIdx++
		}
	}
	return out
}

// accessStats 对象访问统计（形态 6/7/8 共用）。
type accessStats struct {
	Total     int // 访问总次数
	Distinct  int // 去重对象数（= 新对象数，扩展信号）
	Switches  int // 连续访问对象变化次数（方向切换近似）
	TopObject string
	TopCount  int
}

func computeAccessStats(accs []objectAccess) accessStats {
	var st accessStats
	st.Total = len(accs)
	seen := map[string]bool{}
	counts := map[string]int{}
	prev := ""
	prevRec := -1
	prevTurn := -1
	for _, a := range accs {
		if !seen[a.Obj] {
			seen[a.Obj] = true
		}
		counts[a.Obj]++
		if prev != "" && a.Obj != prev && a.Rec != prevRec && a.Turn == prevTurn {
			st.Switches++
		}
		prev, prevRec, prevTurn = a.Obj, a.Rec, a.Turn
	}
	st.Distinct = len(seen)
	for o, c := range counts {
		if c > st.TopCount {
			st.TopCount = c
			st.TopObject = o
		}
	}
	return st
}

// allRecords 回合流 → 扁平记录序列（时间有序）。
func allRecords(turns []Turn) []Record {
	var out []Record
	for i := range turns {
		out = append(out, turns[i].Records...)
	}
	return out
}

// productionTimes edit/write + git commit 时间点（形态 7 产出时限判定）。
func productionTimes(turns []Turn) []time.Time {
	var out []time.Time
	for i := range turns {
		for j := range turns[i].Records {
			rec := &turns[i].Records[j]
			if objKind(rec) == "write" {
				out = append(out, rec.CreatedAt)
				continue
			}
			if rec.Tool.Tool == "bash" && strings.Contains(strings.ToLower(rec.Tool.Command), "git commit") {
				out = append(out, rec.CreatedAt)
			}
		}
	}
	return out
}

func anyIn(ts []time.Time, from, to time.Time) bool {
	for _, t := range ts {
		if !t.Before(from) && !t.After(to) {
			return true
		}
	}
	return false
}

// truncateTurns 截断到 cutoff（T2 fix commit 时刻）之前的记录——之后的新问题时段
// （如 11:50:08 KSN bug）不属于本问题域（与 T5 Cutoff 同口径）。
func truncateTurns(turns []Turn, cutoff time.Time) []Turn {
	if cutoff.IsZero() {
		return turns
	}
	var out []Turn
	for i := range turns {
		if turns[i].Start.After(cutoff) {
			continue
		}
		t := turns[i]
		var recs []Record
		for j := range t.Records {
			if !t.Records[j].CreatedAt.After(cutoff) {
				recs = append(recs, t.Records[j])
			}
		}
		t.Records = recs
		out = append(out, t)
	}
	return out
}

// ── 形态 5：静默停滞 ──

// StagnationParams 形态 5 参数。MinGapMin = 活跃期内无产出信号的最小间隔（默认 120 分钟）。
// N 用 c0ad2534 跨夜 9h+ 标定：跨夜大间隔（≥ SleepGapHours 且跨日）按休眠扣除（负样本），
// 活跃期内同规格无产出间隔才标记；等待用户（间隔内含 user 消息）不算（§2.1）。
type StagnationParams struct {
	MinGapMin int
}

// DefaultStagnationParams 默认参数（N=120 分钟：活跃期 2h 无产出即 L1 候选）。
func DefaultStagnationParams() StagnationParams { return StagnationParams{MinGapMin: 120} }

// StagnationCandidate 形态 5 候选：活跃期内 N 分钟无产出信号后恢复产出。
type StagnationCandidate struct {
	Start      time.Time `json:"start"`      // 活跃期起点（user 消息或上次产出信号）
	End        time.Time `json:"end"`        // 恢复产出的信号时刻
	GapMin     int       `json:"gap_min"`    // 间隔总分钟（含休眠）
	SleepMin   int       `json:"sleep_min"`  // 间隔内休眠重叠分钟（扣除后仍超阈值才标记）
	Production string    `json:"production"` // 恢复产出信号（edit/commit/new:<对象>）
	FromUser   bool      `json:"from_user"`  // 起点是否为 user 消息（指令后迟迟无产出）
}

// DetectStagnation 形态 5 L1 扫描：产出信号（edit/write、git commit、新检索对象）之间的
// 无产出间隔扣休眠后 ≥ MinGapMin → 候选。间隔内含 user 消息（等待用户）跳过；
// 跨夜大间隔按休眠扣除（c0ad2534 9h+ 负样本，§2.1）。
func DetectStagnation(turns []Turn, p StagnationParams) []StagnationCandidate {
	if p.MinGapMin <= 0 {
		p.MinGapMin = 120
	}
	sleeps := recordSleepRanges(allRecords(turns))
	// 产出信号事件流（时间有序）：user 消息 = 活跃期起点
	type sig struct {
		ts   time.Time
		user bool
		prod string
	}
	var sigs []sig
	seenObj := map[string]bool{}
	for i := range turns {
		if strings.TrimSpace(turns[i].UserMsg) != "" {
			sigs = append(sigs, sig{ts: turns[i].Start, user: true})
		}
		for j := range turns[i].Records {
			rec := &turns[i].Records[j]
			switch {
			case objKind(rec) == "write":
				sigs = append(sigs, sig{ts: rec.CreatedAt, prod: "edit"})
			case rec.Tool.Tool == "bash" && strings.Contains(strings.ToLower(rec.Tool.Command), "git commit"):
				sigs = append(sigs, sig{ts: rec.CreatedAt, prod: "commit"})
			case objKind(rec) == "read":
				for _, o := range recordObjects(rec) {
					if !seenObj[o] {
						seenObj[o] = true
						sigs = append(sigs, sig{ts: rec.CreatedAt, prod: "new:" + o})
						break // 一条记录多对象只计一次产出信号
					}
				}
			}
		}
	}
	var out []StagnationCandidate
	last := time.Time{}
	lastUser := false
	for _, s := range sigs {
		if s.user {
			// 等待用户（上轮产出 → 用户新消息）不计停滞；用户消息 = 新活跃期起点
			last, lastUser = s.ts, true
			continue
		}
		if last.IsZero() {
			last, lastUser = s.ts, false
			continue
		}
		total := int(s.ts.Sub(last).Minutes())
		awake := awakeMinutes(last, s.ts, sleeps)
		if awake >= p.MinGapMin {
			out = append(out, StagnationCandidate{
				Start: last, End: s.ts, GapMin: total, SleepMin: total - awake,
				Production: s.prod, FromUser: lastUser,
			})
		}
		last, lastUser = s.ts, false
	}
	return out
}

// ── 形态 6：频繁换方案 ──

// DirectionShiftParams 形态 6 参数：转向次数高 + 加深信号低（转向后新对象比例低）。
type DirectionShiftParams struct {
	SwitchMin   int     // 转向次数下限（默认 5）
	NewRatioMax float64 // 新对象占比上限（默认 0.35）
}

// DefaultDirectionShiftParams 默认参数。
func DefaultDirectionShiftParams() DirectionShiftParams {
	return DirectionShiftParams{SwitchMin: 5, NewRatioMax: 0.35}
}

// DirectionShiftCandidate 形态 6 候选（session 级标签）。
type DirectionShiftCandidate struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Switches    int       `json:"switches"`
	TotalAccess int       `json:"total_access"`
	Distinct    int       `json:"distinct"`
	NewRatio    float64   `json:"new_ratio"`
	Note        string    `json:"note"`
}

// DetectDirectionShifts 形态 6 L1 扫描：对象访问序列中方向切换（连续访问对象变化）≥ SwitchMin
// 且新对象占比 < NewRatioMax（加深信号低）→ 候选。方向 = 对象域近似，L2 确认语义方案转向。
func DetectDirectionShifts(turns []Turn, p DirectionShiftParams) []DirectionShiftCandidate {
	if p.SwitchMin <= 0 {
		p.SwitchMin = 5
	}
	if p.NewRatioMax <= 0 {
		p.NewRatioMax = 0.35
	}
	accs := collectObjectAccesses(turns)
	if len(accs) == 0 {
		return nil
	}
	st := computeAccessStats(accs)
	ratio := float64(st.Distinct) / float64(st.Total)
	if st.Switches >= p.SwitchMin && ratio < p.NewRatioMax {
		return []DirectionShiftCandidate{{
			Start: accs[0].Ts, End: accs[len(accs)-1].Ts,
			Switches: st.Switches, TotalAccess: st.Total, Distinct: st.Distinct, NewRatio: ratio,
			Note: fmt.Sprintf("转向 %d 次但新对象占比 %.0f%%（加深信号低，L1 对象域近似；L2 确认方案转向）",
				st.Switches, ratio*100),
		}}
	}
	return nil
}

// ── 形态 7：重复调查 ──

// RepeatInvestigationParams 形态 7 参数：同一对象重复读 N 次 + 对象扩展率低 + 产出时限。
type RepeatInvestigationParams struct {
	RepeatMin int     // 同一对象重复读下限（默认 8；019ff89b LocalVaultStore×42/9h 实证）
	ExpandMax float64 // 对象扩展率上限（默认 0.35）
	NoProdMin int     // 产出时限：首读→末读+N 分钟无 edit/commit → 标记（默认 30）
}

// DefaultRepeatInvestigationParams 默认参数。
func DefaultRepeatInvestigationParams() RepeatInvestigationParams {
	return RepeatInvestigationParams{RepeatMin: 8, ExpandMax: 0.35, NoProdMin: 30}
}

// RepeatInvestigationCandidate 形态 7 候选。
type RepeatInvestigationCandidate struct {
	Object      string    `json:"object"`
	Reads       int       `json:"reads"`
	FirstRead   time.Time `json:"first_read"`
	LastRead    time.Time `json:"last_read"`
	Distinct    int       `json:"distinct"`
	TotalAccess int       `json:"total_access"`
	ExpandRatio float64   `json:"expand_ratio"`
	NoProdSpan  int       `json:"no_prod_span"` // 首读到（末读+产出时限）内无 edit/commit 的分钟数
	Note        string    `json:"note"`
}

// DetectRepeatInvestigation 形态 7 L1 扫描。
func DetectRepeatInvestigation(turns []Turn, p RepeatInvestigationParams) []RepeatInvestigationCandidate {
	if p.RepeatMin <= 0 {
		p.RepeatMin = 8
	}
	if p.ExpandMax <= 0 {
		p.ExpandMax = 0.35
	}
	if p.NoProdMin <= 0 {
		p.NoProdMin = 30
	}
	accs := collectObjectAccesses(turns)
	if len(accs) == 0 {
		return nil
	}
	st := computeAccessStats(accs)
	expand := float64(st.Distinct) / float64(st.Total)
	if expand >= p.ExpandMax {
		return nil // 对象扩展率不低 → 不满足形态 7
	}
	readCount := map[string]int{}
	firstRead := map[string]time.Time{}
	lastRead := map[string]time.Time{}
	for _, a := range accs {
		if a.Kind != "read" {
			continue
		}
		readCount[a.Obj]++
		if firstRead[a.Obj].IsZero() {
			firstRead[a.Obj] = a.Ts
		}
		lastRead[a.Obj] = a.Ts
	}
	prods := productionTimes(turns)
	var out []RepeatInvestigationCandidate
	for o, n := range readCount {
		if n < p.RepeatMin {
			continue
		}
		spanEnd := lastRead[o].Add(time.Duration(p.NoProdMin) * time.Minute)
		if anyIn(prods, firstRead[o], spanEnd) {
			continue // 时限内有产出（edit/commit）→ 有进展，非重复调查
		}
		out = append(out, RepeatInvestigationCandidate{
			Object: o, Reads: n, FirstRead: firstRead[o], LastRead: lastRead[o],
			Distinct: st.Distinct, TotalAccess: st.Total, ExpandRatio: expand,
			NoProdSpan: int(spanEnd.Sub(firstRead[o]).Minutes()),
			Note: fmt.Sprintf("同一对象重复读 %d 次（首读→末读+%dmin 无 edit/commit）；对象扩展率 %.0f%%",
				n, p.NoProdMin, expand*100),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Reads > out[j].Reads })
	return out
}

// ── 形态 8：单点死磕 ──

// SingleFocusParams 形态 8 参数：修改/检索对象集合高度集中 + 新对象扩展率低
// （与形态 6 区别：不换方向——转向次数 ≤ SwitchMax）。
type SingleFocusParams struct {
	ConcMin   float64 // top-1 对象占比下限（默认 0.50；019ff89b 同文件域实证）
	ExpandMax float64 // 对象扩展率上限（默认 0.25）
	SwitchMax int     // 转向次数上限（默认 4）
}

// DefaultSingleFocusParams 默认参数。
func DefaultSingleFocusParams() SingleFocusParams {
	return SingleFocusParams{ConcMin: 0.50, ExpandMax: 0.25, SwitchMax: 4}
}

// SingleFocusCandidate 形态 8 候选（session 级标签）。
type SingleFocusCandidate struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	TopObject   string    `json:"top_object"`
	TopCount    int       `json:"top_count"`
	TopShare    float64   `json:"top_share"`
	Distinct    int       `json:"distinct"`
	TotalAccess int       `json:"total_access"`
	ExpandRatio float64   `json:"expand_ratio"`
	Switches    int       `json:"switches"`
	Note        string    `json:"note"`
}

// DetectSingleFocus 形态 8 L1 扫描。
func DetectSingleFocus(turns []Turn, p SingleFocusParams) []SingleFocusCandidate {
	if p.ConcMin <= 0 {
		p.ConcMin = 0.50
	}
	if p.ExpandMax <= 0 {
		p.ExpandMax = 0.25
	}
	if p.SwitchMax <= 0 {
		p.SwitchMax = 4
	}
	accs := collectObjectAccesses(turns)
	if len(accs) == 0 {
		return nil
	}
	st := computeAccessStats(accs)
	topShare := float64(st.TopCount) / float64(st.Total)
	expand := float64(st.Distinct) / float64(st.Total)
	if topShare >= p.ConcMin && expand < p.ExpandMax && st.Switches <= p.SwitchMax {
		return []SingleFocusCandidate{{
			Start: accs[0].Ts, End: accs[len(accs)-1].Ts,
			TopObject: st.TopObject, TopCount: st.TopCount, TopShare: topShare,
			Distinct: st.Distinct, TotalAccess: st.Total, ExpandRatio: expand, Switches: st.Switches,
			Note: fmt.Sprintf("对象高度集中 %s×%d（占比 %.0f%%），扩展率 %.0f%%——单点死磕不换方向（L1 对象域近似）",
				st.TopObject, st.TopCount, topShare*100, expand*100),
		}}
	}
	return nil
}

// ── 形态 9：验证循环 ──

// VerifyLoopParams 形态 9 参数：三元组（失败 → 同命令重试 → 中间无 edit/无日志分析）。
type VerifyLoopParams struct {
	RetryLookahead int // 失败后向后找同命令重试的 bash 命令条数（默认 5）
	MaxGapMin      int // 失败与重试最大时间间隔（默认 30 分钟）
}

// DefaultVerifyLoopParams 默认参数。
func DefaultVerifyLoopParams() VerifyLoopParams {
	return VerifyLoopParams{RetryLookahead: 5, MaxGapMin: 30}
}

// VerifyLoopCandidate 形态 9 候选。
type VerifyLoopCandidate struct {
	FailTime  time.Time `json:"fail_time"`
	RetryTime time.Time `json:"retry_time"`
	Command   string    `json:"command"`
	FailSig   string    `json:"fail_sig"` // exit_code（legacy 强信号）/ error_word（hook 弱信号）
	Note      string    `json:"note"`
}

// toolErrorRe 形态 9 hook 弱信号：tool_response 输出段错误词（§2.1：L1 候选 + L2 确认）。
// 用词边界收窄（\berror\b 不命中 TestFooErrorHandling 类测试名）；Claude P1a 审核 C1：
// 原实现匹配整段 Content（含命令本身/助手文本）导致 5/5 误报——只匹配输出段。
var toolErrorRe = regexp.MustCompile(`(?i)(\berror\b|failed|fatal|FAIL|panic|undefined|not found|no such file|cannot|exit status|command not found)`)

// toolErrText 失败弱信号文本来源：legacy = Tool.Output（结构化 stdout）；hook =
// content 输出段（🔧 行「→ 」之后，post_tool 无 Output 字段）。不含命令本身与助手讨论文本。
func toolErrText(rec *Record) string {
	if rec.Tool.Output != "" {
		return rec.Tool.Output
	}
	if i := strings.Index(rec.Content, "→ "); i >= 0 {
		return rec.Content[i+len("→ "):]
	}
	return ""
}

// cmdFailed 命令失败判定：legacy = exit_code 强信号；hook = 输出段文本错误词弱信号。
func cmdFailed(rec *Record) (sig string, ok bool) {
	if rec.Tool.ExitCode != nil && *rec.Tool.ExitCode != 0 {
		return "exit_code", true
	}
	if toolErrorRe.MatchString(toolErrText(rec)) {
		return "error_word", true
	}
	return "", false
}

// gitCmdRe git 命令段（含 cd ... && git ... 前缀，实测 01a00d3c 8/20 窗口；分号变体
// `cd /path; git ...` 一并覆盖，Claude P1a 二轮审核 B3）。
var gitCmdRe = regexp.MustCompile(`(^|&&\s*|;\s*)git\s+`)

// isGitCmd git 产出/版本控制类命令（Claude P1a 审核 C1）：形态 9 目标 = build/test/run
// 验证命令；git add/commit/push/checkout 失败→重试是正常工程行为（post-commit hook 失败、
// index.lock/SQLITE_BUSY 后必须重试成功才能继续），非盲试验证循环。
func isGitCmd(norm string) bool {
	return gitCmdRe.MatchString(norm)
}

// logAnalysisRe 失败与重试之间的日志分析信号（读日志/查错误）——命中即排除（盲试 vs 有依据）。
var logAnalysisRe = regexp.MustCompile(`(?i)(Logs/|\.log\b|tail |grep -i? ?err|journalctl|dmesg|查看.*日志|看.*错误|分析)`)

func normCmd(cmd string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(cmd)), " ")
}

// noAnalysisBetween 中间无 edit/write、无日志分析（读日志/查错误/分析文本）。
func noAnalysisBetween(recs []*Record, from, to int) bool {
	for k := from; k <= to; k++ {
		rec := recs[k]
		if objKind(rec) == "write" {
			return false
		}
		if rec.Tool.Tool == "bash" && logAnalysisRe.MatchString(rec.Tool.Command) {
			return false
		}
		if isAssistantText(rec) && logAnalysisRe.MatchString(rec.Content) {
			return false
		}
	}
	return true
}

// DetectVerifyLoops 形态 9 L1 扫描：失败 → 同命令（normCmd 归一化）重试 → 中间无 edit/日志分析。
func DetectVerifyLoops(turns []Turn, p VerifyLoopParams) []VerifyLoopCandidate {
	if p.RetryLookahead <= 0 {
		p.RetryLookahead = 5
	}
	if p.MaxGapMin <= 0 {
		p.MaxGapMin = 30
	}
	var recs []*Record
	for i := range turns {
		for j := range turns[i].Records {
			recs = append(recs, &turns[i].Records[j])
		}
	}
	var out []VerifyLoopCandidate
	for i, f := range recs {
		if f.Tool.Tool != "bash" {
			continue
		}
		sig, ok := cmdFailed(f)
		if !ok {
			continue
		}
		norm := normCmd(f.Tool.Command)
		if norm == "" || isGitCmd(norm) {
			continue
		}
		seenBash := 0
		for j := i + 1; j < len(recs); j++ {
			rt := recs[j]
			if rt.CreatedAt.Sub(f.CreatedAt) > time.Duration(p.MaxGapMin)*time.Minute {
				break
			}
			if rt.Tool.Tool != "bash" {
				continue
			}
			seenBash++
			if seenBash > p.RetryLookahead {
				break
			}
			if normCmd(rt.Tool.Command) == norm {
				if noAnalysisBetween(recs, i+1, j-1) {
					out = append(out, VerifyLoopCandidate{
						FailTime: f.CreatedAt, RetryTime: rt.CreatedAt, Command: norm, FailSig: sig,
						Note: "失败 → 同命令重试 → 中间无 edit/无日志分析（三元组，L1；hook 错误词为弱信号待 L2 确认）",
					})
				}
				break // 每次失败只记首个同命令重试
			}
		}
	}
	return out
}

// ── 形态 10：伪进展 ──

// FakeProgressParams 形态 10 参数：加日志/打点反复添加或撤销，无「根因定位 + 修复 commit」。
type FakeProgressParams struct {
	MinEdits   int // 同一文件打点改动次数下限（默认 2 = 反复添加或撤销）
	MinGapMin  int // 同一文件两次打点改动的间隔下限（默认 10 分钟；同刻/短间隔 = 单次修改计 1 次）
}

// DefaultFakeProgressParams 默认参数。
func DefaultFakeProgressParams() FakeProgressParams {
	return FakeProgressParams{MinEdits: 2, MinGapMin: 10}
}

// FakeProgressCandidate 形态 10 候选。
type FakeProgressCandidate struct {
	File        string    `json:"file"`
	Edits       int       `json:"edits"`
	FirstEdit   time.Time `json:"first_edit"`
	LastEdit    time.Time `json:"last_edit"`
	NoRootCause bool      `json:"no_root_cause"` // 改动跨度内无根因定位文本（有 → 正当排查）
	NoCommit    bool      `json:"no_commit"`     // 改动跨度（+30min 宽限）内无 commit（有 → 正当收尾）
	Note        string    `json:"note"`
}

// logInstrumentRe 打点/日志关键词（判别式：加日志/打点反复添加或撤销；8/14 019ffdce
// 8 处日志改动全撤销为负例）。只匹配明确打点语法；Claude P1a 审核 C2：去掉裸 print(
// （正常代码 print(x) 常见），保留 println/printf 等调试特征。
var logInstrumentRe = regexp.MustCompile(`(?i)(NSLog\(|println\(|printf\(|console\.log\(|debugPrint\(|printk\(|log\.(?:debug|info|warn|error|trace)\(|logger\.\w+\(|加日志|打点|记录日志)`)

// recordWriteTargets 形态 10 写对象：只用 Tool.Files（edit/write 工具 + bash_heuristic
// 标记的 apply_patch/sed 写，rel_path/file_path 权威）。命令级提取对形态 10 关闭——
// heredoc/sed 命令全文中的代码路径（r.ts、/tmp/ed_*.go 引用）会被误捕（Claude P1a 审核 C2/C4）。
func recordWriteTargets(rec *Record) []string {
	var out []string
	for _, f := range rec.Tool.Files {
		if isProjectObject(f) {
			out = append(out, f)
		}
	}
	return out
}

// mergeSameFileInsts 同文件打点改动按时间合并：相邻 inst 间隔 < MinGapMin 视为同一次
// 修改（单次 apply_patch 多匹配/多行同刻计多次 = 假「反复」，Claude P1a 审核 C2）。
func mergeSameFileInsts(times []time.Time, gapMin int) []time.Time {
	if len(times) <= 1 {
		return times
	}
	var out []time.Time
	prev := times[0]
	out = append(out, prev)
	for _, t := range times[1:] {
		if t.Sub(prev) >= time.Duration(gapMin)*time.Minute {
			out = append(out, t)
			prev = t // 仅保留时更新基准：间隔始终相对「最后保留的改动」算（Claude P1a 二轮 B1）
		}
	}
	return out
}

// DetectFakeProgress 形态 10 L1 扫描：同文件打点改动 ≥ MinEdits 次，且跨度内无根因定位文本、
// 撤销后 30min 宽限内无 commit → 候选（L2 确认「反复添加或撤销」语义）。
func DetectFakeProgress(turns []Turn, p FakeProgressParams) []FakeProgressCandidate {
	if p.MinEdits <= 0 {
		p.MinEdits = 2
	}
	if p.MinGapMin <= 0 {
		p.MinGapMin = 10
	}
	type inst struct {
		ts   time.Time
		file string
	}
	var insts []inst
	for i := range turns {
		for j := range turns[i].Records {
			rec := &turns[i].Records[j]
			if objKind(rec) != "write" {
				continue
			}
			text := rec.Content + " " + rec.Tool.Command + " " + rec.Tool.Output
			if !logInstrumentRe.MatchString(text) {
				continue
			}
			for _, f := range recordWriteTargets(rec) {
				insts = append(insts, inst{ts: rec.CreatedAt, file: f})
			}
		}
	}
	if len(insts) == 0 {
		return nil
	}
	var rootTs, commitTs []time.Time
	for i := range turns {
		for j := range turns[i].Records {
			rec := &turns[i].Records[j]
			if rec.Role == "assistant" && rootCauseRe.MatchString(rec.Content) {
				rootTs = append(rootTs, rec.CreatedAt)
			}
			if rec.Tool.Tool == "bash" && strings.Contains(strings.ToLower(rec.Tool.Command), "git commit") {
				commitTs = append(commitTs, rec.CreatedAt)
			}
		}
	}
	counts := map[string]int{}
	first := map[string]time.Time{}
	last := map[string]time.Time{}
	// 同刻/短间隔去重后再计次数（「反复添加或撤销」需要时间分离）
	byFile := map[string][]time.Time{}
	for _, x := range insts {
		byFile[x.file] = append(byFile[x.file], x.ts)
	}
	for f, times := range byFile {
		merged := mergeSameFileInsts(times, p.MinGapMin)
		counts[f] = len(merged)
		first[f] = merged[0]
		last[f] = merged[len(merged)-1]
	}
	var out []FakeProgressCandidate
	for f, n := range counts {
		if n < p.MinEdits {
			continue
		}
		grace := last[f].Add(30 * time.Minute) // 撤销打点后 30min 内 commit = 正当收尾
		noRoot := !anyIn(rootTs, first[f], last[f])
		noCommit := !anyIn(commitTs, first[f], grace)
		if !noRoot || !noCommit {
			continue
		}
		out = append(out, FakeProgressCandidate{
			File: f, Edits: n, FirstEdit: first[f], LastEdit: last[f],
			NoRootCause: true, NoCommit: true,
			Note: fmt.Sprintf("文件 %s 打点改动 %d 次（反复添加/撤销），跨度内无根因定位文本、无 commit——伪进展 L1 候选（L2 确认）", f, n),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Edits > out[j].Edits })
	return out
}
