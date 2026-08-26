package eval

// P1b L2 编排层（PROCESS_QUALITY_SPEC §2.2 五任务确认器的管道接入）。
// 把 L1 候选按任务类型喂给 L2Confirmer → 解析 → 回填 L2RunResult：
//   任务 1/2（断言分类/证据细配对）：assistant 文本候选断言（ClaimHit）
//   任务 3（死循环确认）：T5 死循环候选 + 形态 9 验证循环候选（同命令重试语义）
//   任务 4（方向评估）：满足 DirectionEvalTriggered 的死循环候选 + 形态 6 方向转换候选
//   任务 5（反馈响应确认）：T3 纠偏/存疑反馈 + 反馈后 20min 行为序列
// LLM 不可用（confirmer nil）→ 降级：标注「L2 未运行」，不伪造确认结果（Claude C3）。

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// L2RunOptions L2 编排选项。
type L2RunOptions struct {
	// MaxPerTask 每类任务最多确认数（成本控制；<=0 用默认 5）。
	MaxPerTask int
	// SamplePerLayer P1c 分层抽样：每层（按候选日期）最多抽样数；>0 时替代「前 N 条」
	// （Claude P1c 建议：492 条候选断言取前 N 条全是 8/17 开头噪音，需按天分层代表性抽样）。
	SamplePerLayer int
}

// L2Item 单条 L2 确认记录（候选 → 结果；失败时 Error 保留，不伪装成功）。
type L2Item struct {
	Task   L2Task          `json:"task"`
	Target string          `json:"target"`           // 候选标识（时段/对象/断言）
	Result json.RawMessage `json:"result,omitempty"` // 解析后的确认 JSON
	Error  string          `json:"error,omitempty"`  // LLM/解析失败原因
}

// L2RunResult L2 编排结果（写入 ProcessReport.L2）。
type L2RunResult struct {
	Ran       bool     `json:"ran"`              // false = L2 未配置/不可用（降级标注）
	Reason    string   `json:"reason,omitempty"` // 降级/上限跳过原因
	Total     int      `json:"total"`
	Succeeded int      `json:"succeeded"`
	Failed    int      `json:"failed"`
	Items     []L2Item `json:"items"`
	Warnings  []string `json:"warnings,omitempty"` // 证据完整性警告（bug-20260826-154305-941881）
}

// warn 追加一条证据完整性/质量警告。
func (res *L2RunResult) warn(msg string) {
	res.Warnings = append(res.Warnings, msg)
}

// RunL2Confirmations 编排 L2 五任务确认。
// confirmer 为 nil → 降级：返回 Ran=false 的 L2RunResult（「L2 未运行」标注），
// 绝不把「未确认」伪装成「确认通过」。
func RunL2Confirmations(confirmer L2Confirmer, rep *ProcessReport, turns []Turn, opts L2RunOptions) (*L2RunResult, error) {
	if confirmer == nil {
		return &L2RunResult{Ran: false, Reason: "L2 未配置（LLM 确认器不可用）：候选未确认，标注 L2 未运行，不伪造结果"}, nil
	}
	if opts.MaxPerTask <= 0 {
		opts.MaxPerTask = 3 // Claude P1b 二轮审核：默认 5×5 任务成本过高，先跑通再放开
	}
	res := &L2RunResult{Ran: true}
	all := allRecords(turns)

	res.runClaims(confirmer, all, CandidateClaimHits(all), opts)
	// 任务 3 死循环确认 = T5 候选 + 形态9 验证循环，共享 MaxPerTask 计数
	// （P2：此前两个函数各自计数，实际上限 2×MaxPerTask，注释名不副实）。
	deadloopN := 0
	res.runDeadloops(confirmer, turns, rep.Deadloops, opts, &deadloopN)
	res.runVerifyLoops(confirmer, turns, rep.VerifyLoops, opts, &deadloopN)
	res.runDirectionEvals(confirmer, turns, rep, opts)
	res.runFeedbackResponses(confirmer, turns, rep.Feedback, opts)
	return res, nil
}

// ── P1c 分层抽样（候选→人工确认闭环的标注集生成）──

// stratifiedSample 按层键分组、层内均匀抽样（首/末/中间均布点，确定性可复现）。
// 替代「取前 N 条」：前 N 条往往集中在开头（如 3 天 session 的 492 条断言全是
// 8/17 噪音），分层抽样保证每层（每天）都有代表。
func stratifiedSample[T any](items []T, key func(T) string, perLayer int) []T {
	if perLayer <= 0 || len(items) <= perLayer {
		return items
	}
	type group struct {
		items []T
	}
	var groups []group
	idx := map[string]int{}
	for _, it := range items {
		k := key(it)
		if gi, ok := idx[k]; ok {
			groups[gi].items = append(groups[gi].items, it)
		} else {
			idx[k] = len(groups)
			groups = append(groups, group{items: []T{it}})
		}
	}
	var out []T
	for _, g := range groups {
		out = append(out, evenlySample(g.items, perLayer)...)
	}
	return out
}

// evenlySample 层内均匀抽样 n 个（首/末/中间均布；确定性）。
func evenlySample[T any](items []T, n int) []T {
	if n <= 0 || len(items) <= n {
		return items
	}
	// n=1 时 (n-1)=0 除零 → NaN；Go spec 对 int(NaN) 是 implementation-specific
	// （当前平台返回 0，其他平台可能 min int64 → 负数索引 panic）——显式保护。
	if n == 1 {
		return items[:1]
	}
	out := make([]T, 0, n)
	for i := 0; i < n; i++ {
		pos := int(math.Round(float64(i) * float64(len(items)-1) / float64(n-1)))
		out = append(out, items[pos])
	}
	return out
}

// ── 任务 1/2：断言分类 + 证据细配对 ──

// runClaims 任务 1（断言分类）→ 判「事实」的断言再跑任务 2（证据细配对）。
func (res *L2RunResult) runClaims(confirmer L2Confirmer, recs []Record, hits []ClaimHit, opts L2RunOptions) {
	hits = stratifiedSample(hits, func(h ClaimHit) string { return h.At.Format("2006-01-02") }, opts.SamplePerLayer)
	n := 0
	for i := range hits {
		if n >= opts.MaxPerTask {
			res.skip("断言分类达上限（%d），剩余 %d 候选跳过", opts.MaxPerTask, len(hits)-n)
			return
		}
		prior := priorCommandLines(recs, hits[i])
		p := BuildClaimClassifyPrompt(hits[i].Text, prior)
		out, err := confirmer.Confirm(p)
		if err != nil {
			res.add(L2Item{Task: L2ClaimClassify, Target: shortTarget(hits[i].Text), Error: fmt.Sprintf("LLM: %v", err)})
			n++
			continue
		}
		parsed, err := ParseClaimClassify(out)
		if err != nil {
			res.add(L2Item{Task: L2ClaimClassify, Target: shortTarget(hits[i].Text), Error: fmt.Sprintf("解析: %v", err)})
			n++
			continue
		}
		raw, _ := json.Marshal(parsed)
		res.add(L2Item{Task: L2ClaimClassify, Target: shortTarget(hits[i].Text), Result: raw})
		n++
		// 任务 2：只对「事实」断言做证据细配对（§2.2：粗配对无匹配才触发，这里以 L1
		// 对象关键词命中近似——命中即粗配对成立，直接记录粗配对结果，不再耗 LLM
		// 覆判强证据；仅无命中才调 L2 确认「真无证据」，边界由 prompt 保证）。
		if parsed.Type != "事实" {
			continue
		}
		objects := priorObjects(recs, hits[i])
		if hit, matched := coarseEvidenceHit(hits[i].Text, objects); hit {
			raw2, _ := json.Marshal(EvidenceMatchResult{Match: coarseMatchLevel(matched), Basis: "粗配对命中（静态）: " + matched})
			res.add(L2Item{Task: L2EvidenceMatch, Target: shortTarget(hits[i].Text), Result: raw2})
			continue
		}
		p2 := BuildEvidenceMatchPrompt(hits[i].Text, objects)
		out2, err := confirmer.Confirm(p2)
		if err != nil {
			res.add(L2Item{Task: L2EvidenceMatch, Target: shortTarget(hits[i].Text), Error: fmt.Sprintf("LLM: %v", err)})
			continue
		}
		parsed2, err := ParseEvidenceMatch(out2)
		if err != nil {
			res.add(L2Item{Task: L2EvidenceMatch, Target: shortTarget(hits[i].Text), Error: fmt.Sprintf("解析: %v", err)})
			continue
		}
		raw2, _ := json.Marshal(parsed2)
		res.add(L2Item{Task: L2EvidenceMatch, Target: shortTarget(hits[i].Text), Result: raw2})
	}
}

// ── 任务 3：死循环确认 ──

// runDeadloops T5 死循环候选（非 Excluded；near-miss 已排除不确认）。
func (res *L2RunResult) runDeadloops(confirmer L2Confirmer, turns []Turn, cands []DeadloopCandidate, opts L2RunOptions, n *int) {
	cands = stratifiedSample(cands, func(c DeadloopCandidate) string { return c.Start.Format("2006-01-02") }, opts.SamplePerLayer)
	for i := range cands {
		if cands[i].Excluded {
			continue
		}
		if *n >= opts.MaxPerTask {
			res.skip("死循环确认达上限（%d），剩余候选跳过", opts.MaxPerTask)
			return
		}
		c := cands[i]
		lines := l2CommandLines(l2RecordsBetween(turns, c.Start, c.End), 0, 0)
		p := BuildDeadloopConfirmPrompt(c, lines)
		out, err := confirmer.Confirm(p)
		if err != nil {
			res.add(L2Item{Task: L2DeadloopConfirm, Target: candTarget(c.Start, c.End), Error: fmt.Sprintf("LLM: %v", err)})
			(*n)++
			continue
		}
		parsed, err := ParseDeadloopConfirm(out)
		if err != nil {
			res.add(L2Item{Task: L2DeadloopConfirm, Target: candTarget(c.Start, c.End), Error: fmt.Sprintf("解析: %v", err)})
			(*n)++
			continue
		}
		if parsed.IsDeadloop {
			res.warnDeadloopEvidence(turns, c.Start, c.End)
		}
		raw, _ := json.Marshal(parsed)
		res.add(L2Item{Task: L2DeadloopConfirm, Target: candTarget(c.Start, c.End), Result: raw})
		(*n)++
	}
}

// runVerifyLoops 形态 9 验证循环候选 → 任务 3（同命令重试 ≈ 死循环盲试语义）。
// 注记（Claude P1b 二轮审核 C5）：Builds=2/Fails=1 是形态 9 三元组（失败→同命令重试）
// 的近似组合信号，非实际统计——证据里另有 Reason 写明命令与 fail_sig，L2 按行为序列判定。
func (res *L2RunResult) runVerifyLoops(confirmer L2Confirmer, turns []Turn, cands []VerifyLoopCandidate, opts L2RunOptions, n *int) {
	cands = stratifiedSample(cands, func(v VerifyLoopCandidate) string { return v.FailTime.Format("2006-01-02") }, opts.SamplePerLayer)
	for i := range cands {
		if *n >= opts.MaxPerTask {
			res.skip("形态 9 死循环确认达上限（%d），剩余候选跳过", opts.MaxPerTask)
			return
		}
		v := cands[i]
		c := DeadloopCandidate{
			Start:  v.FailTime,
			End:    v.RetryTime,
			Builds: 2,
			Fails:  1,
			Reason: fmt.Sprintf("形态9 验证循环: %s（%s）", v.Command, v.FailSig),
		}
		lines := l2CommandLines(l2RecordsBetween(turns, v.FailTime, v.RetryTime), 0, 0)
		p := BuildDeadloopConfirmPrompt(c, lines)
		out, err := confirmer.Confirm(p)
		if err != nil {
			res.add(L2Item{Task: L2DeadloopConfirm, Target: candTarget(v.FailTime, v.RetryTime), Error: fmt.Sprintf("LLM: %v", err)})
			(*n)++
			continue
		}
		parsed, err := ParseDeadloopConfirm(out)
		if err != nil {
			res.add(L2Item{Task: L2DeadloopConfirm, Target: candTarget(v.FailTime, v.RetryTime), Error: fmt.Sprintf("解析: %v", err)})
			(*n)++
			continue
		}
		if parsed.IsDeadloop {
			res.warnDeadloopEvidence(turns, v.FailTime, v.RetryTime)
		}
		raw, _ := json.Marshal(parsed)
		res.add(L2Item{Task: L2DeadloopConfirm, Target: candTarget(v.FailTime, v.RetryTime), Result: raw})
		(*n)++
	}
}

// warnDeadloopEvidence 死循环确认为 true 且窗口内无 write 信号时，
// 追加证据完整性警告（discussion_log 捕获缺口，bug-20260826-154305-941881）。
func (res *L2RunResult) warnDeadloopEvidence(turns []Turn, from, to time.Time) {
	writes := 0
	for _, r := range l2RecordsBetween(turns, from, to) {
		if objKind(&r) == "write" {
			writes++
		}
	}
	if writes > 0 {
		return
	}
	res.warn(fmt.Sprintf("死循环确认 true 窗口 %s → %s 无 write/commit 信号——"+
		"discussion_log 存在 apply_patch 捕获缺口（bug-20260826-154305-941881），建议 JSONL 交叉校验后再回填",
		tsFmt(from), tsFmt(to)))
}

// ── 任务 4：方向评估 ──

// runDirectionEvals 方向评估：满足 DirectionEvalTriggered 的 T5 死循环候选
// + 形态 6 方向转换候选（段内自发检索 < 2 视为「候选方向错」，§2.2 触发条件）。
func (res *L2RunResult) runDirectionEvals(confirmer L2Confirmer, turns []Turn, rep *ProcessReport, opts L2RunOptions) {
	n := 0
	buildMin := DefaultDeadloopParams().BuildMin
	for i := range rep.Deadloops {
		d := rep.Deadloops[i]
		if d.Excluded || !DirectionEvalTriggered(d.SpontRetr, d.Builds, buildMin) {
			continue
		}
		if n >= opts.MaxPerTask {
			res.skip("方向评估达上限（%d），剩余候选跳过", opts.MaxPerTask)
			return
		}
		lines := l2CommandLines(l2RecordsBetween(turns, d.Start, d.End), 0, 0)
		problem := problemContext(turns, d.Start, d.End)
		p := BuildDirectionEvalPrompt(problem, lines, d.SpontRetr)
		out, err := confirmer.Confirm(p)
		target := fmt.Sprintf("%s → %s（死循环候选，build=%d 自发=%d）", tsFmt(d.Start), tsFmt(d.End), d.Builds, d.SpontRetr)
		res.addDirection(confirmer, L2DirectionEval, target, out, err)
		n++
	}
	for i := range rep.DirectionShifts {
		c := rep.DirectionShifts[i]
		spont := spontRetrBetween(turns, c.Start, c.End)
		if spont >= 2 {
			continue
		}
		if n >= opts.MaxPerTask {
			res.skip("方向评估达上限（%d），剩余候选跳过", opts.MaxPerTask)
			return
		}
		lines := l2CommandLines(l2RecordsBetween(turns, c.Start, c.End), 0, 0)
		problem := problemContext(turns, c.Start, c.End)
		p := BuildDirectionEvalPrompt(problem, lines, spont)
		out, err := confirmer.Confirm(p)
		target := fmt.Sprintf("%s → %s（形态6 转向 %d 次，自发=%d）", tsFmt(c.Start), tsFmt(c.End), c.Switches, spont)
		res.addDirection(confirmer, L2DirectionEval, target, out, err)
		n++
	}
}

func (res *L2RunResult) addDirection(confirmer L2Confirmer, task L2Task, target, out string, err error) {
	if err != nil {
		res.add(L2Item{Task: task, Target: target, Error: fmt.Sprintf("LLM: %v", err)})
		return
	}
	parsed, err := ParseDirectionEval(out)
	if err != nil {
		res.add(L2Item{Task: task, Target: target, Error: fmt.Sprintf("解析: %v", err)})
		return
	}
	raw, _ := json.Marshal(parsed)
	res.add(L2Item{Task: task, Target: target, Result: raw})
}

// ── 任务 5：反馈响应确认 ──

// runFeedbackResponses 纠偏/存疑反馈 → 反馈后 20min 行为序列（§2.2 五子信号）。
func (res *L2RunResult) runFeedbackResponses(confirmer L2Confirmer, turns []Turn, cands []FeedbackCandidate, opts L2RunOptions) {
	cands = stratifiedSample(cands, func(fb FeedbackCandidate) string { return fb.Ts.Format("2006-01-02") }, opts.SamplePerLayer)
	n := 0
	for i := range cands {
		fb := cands[i]
		if fb.Kind != KindCorrection && fb.Kind != KindSuspicious {
			continue
		}
		if n >= opts.MaxPerTask {
			res.skip("反馈响应确认达上限（%d），剩余候选跳过", opts.MaxPerTask)
			return
		}
		window := l2RecordsBetween(turns, fb.Ts, fb.Ts.Add(20*time.Minute))
		lines := l2CommandLines(window, 0, 0)
		p := BuildFeedbackResponsePrompt(fb, lines)
		out, err := confirmer.Confirm(p)
		target := fmt.Sprintf("%s %s（%s）", tsFmt(fb.Ts), fb.UserMsgID, fb.Kind)
		if err != nil {
			res.add(L2Item{Task: L2FeedbackResponse, Target: target, Error: fmt.Sprintf("LLM: %v", err)})
			n++
			continue
		}
		parsed, err := ParseFeedbackResponse(out)
		if err != nil {
			res.add(L2Item{Task: L2FeedbackResponse, Target: target, Error: fmt.Sprintf("解析: %v", err)})
			n++
			continue
		}
		raw, _ := json.Marshal(parsed)
		res.add(L2Item{Task: L2FeedbackResponse, Target: target, Result: raw})
		n++
	}
}

// ── 辅助 ──

func (res *L2RunResult) add(item L2Item) {
	res.Total++
	if item.Error != "" {
		res.Failed++
	} else {
		res.Succeeded++
	}
	res.Items = append(res.Items, item)
}

func (res *L2RunResult) skip(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if res.Reason == "" {
		res.Reason = msg
	} else {
		res.Reason += "; " + msg
	}
}

// priorCommandLines 断言记录之前的命令（L1 断言分类证据：前序工具记录，截断）。
func priorCommandLines(recs []Record, hit ClaimHit) []string {
	if hit.RecordIdx <= 0 || hit.RecordIdx > len(recs) {
		return nil
	}
	return l2CommandLines(recs[:hit.RecordIdx], 0, 0)
}

// priorObjects 断言记录之前的工具对象（文件路径 + 命令行；任务 2 证据细配对输入）。
func priorObjects(recs []Record, hit ClaimHit) []string {
	if hit.RecordIdx <= 0 || hit.RecordIdx > len(recs) {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, r := range recs[:hit.RecordIdx] {
		for _, f := range r.Tool.Files {
			if f != "" && !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	// legacy 无对象级数据：命令字符串兜底（弱信号，prompt 侧由 L2 判「只沾边对象」）
	for _, line := range l2CommandLines(recs[:hit.RecordIdx], 8, 120) {
		if !seen[line] {
			seen[line] = true
			out = append(out, line)
		}
	}
	return out
}

// coarseEvidenceHit §2.2 粗配对：断言关键词与前序对象（文件路径/命令）的子串命中。
// 任一对象含 ≥1 个关键词即粗配对命中 → 任务 2 不再调 LLM（避免覆判强证据、省调用）；
// 仅无命中才调 L2 确认「真无证据」。关键词口径：英文词（≥3 字母）+ CJK 2-gram。
func coarseEvidenceHit(claim string, objects []string) (bool, string) {
	kws := claimKeywords(claim)
	for _, o := range objects {
		lo := strings.ToLower(o)
		for _, k := range kws {
			if strings.Contains(lo, k) {
				return true, o
			}
		}
	}
	return false, ""
}

// coarseMatchLevel 粗配对命中的确定性级别：直接文件路径引用（无空格、含路径分隔符）
// → 强；命令文本沾边 → 弱。不判「操作内容相关性」——那是 L2 细配对职责。
func coarseMatchLevel(matched string) string {
	if !strings.ContainsAny(matched, " \t") &&
		(strings.Contains(matched, "/") || strings.Contains(matched, "\\") ||
			strings.HasPrefix(matched, "./") || strings.HasPrefix(matched, "../")) {
		return "强"
	}
	return "弱"
}

var claimKeywordRe = regexp.MustCompile(`[a-zA-Z0-9_]{3,}`)

// claimKeywords 从断言提取粗配对关键词（小写去重）：
// 英文/数字词（≥3 字符）+ CJK 连续段 2-gram。低精度高召回，仅用于触发判定。
func claimKeywords(s string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(k string) {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, k)
	}
	for _, w := range claimKeywordRe.FindAllString(s, -1) {
		add(w)
	}
	runes := []rune(s)
	for i := 0; i < len(runes)-1; i++ {
		if isCJK(runes[i]) && isCJK(runes[i+1]) {
			add(string(runes[i : i+2]))
		}
	}
	return out
}

// isCJK CJK 统一表意文字 + 全角标点区间（粗配对 2-gram 切分用）。
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) || (r >= 0x3000 && r <= 0x303F)
}

// shortTarget 断言句截断（L2 条目 target 用，避免整句刷屏）。
func shortTarget(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return string(r)
}

// candTarget 时段候选标识。
func candTarget(from, to time.Time) string {
	return fmt.Sprintf("%s → %s", tsFmt(from), tsFmt(to))
}

// problemContext 问题上下文（任务 4 输入）：候选时段内最近一条 user 消息优先；
// 窗口内无 user 消息（死循环候选窗口通常只有几十秒）则取窗口「开始前」最近一条，
// 而非回退 session 首条过时消息（Claude P1b 二轮审核 C6，P2 修正）；再无则回退首条非空。
func problemContext(turns []Turn, from, to time.Time) string {
	var latest string
	var beforeLatest string
	for i := range turns {
		s := strings.TrimSpace(turns[i].UserMsg)
		if s == "" {
			continue
		}
		if !turns[i].Start.Before(from) && !turns[i].Start.After(to) {
			latest = s
		} else if !turns[i].Start.After(from) {
			beforeLatest = s
		}
	}
	if latest != "" {
		return snippetOf(latest)
	}
	if beforeLatest != "" {
		return snippetOf(beforeLatest)
	}
	for i := range turns {
		if s := strings.TrimSpace(turns[i].UserMsg); s != "" {
			return snippetOf(s)
		}
	}
	return ""
}

// spontRetrBetween 时段内自发检索次数（形态 6 方向评估触发判定）。
func spontRetrBetween(turns []Turn, from, to time.Time) int {
	n := 0
	for _, r := range l2RecordsBetween(turns, from, to) {
		if isHistoryRetrieval(r.Tool) {
			n++
		}
	}
	return n
}
