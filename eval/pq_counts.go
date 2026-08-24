package eval

// P0a2 P3 计数类基线（PROCESS_QUALITY_SPEC §2.1 P3 基线检测点族，v1.4 第二轮先行定义）。
// 排期（v1.4 第三轮）：计数类（重复验证点/自建记录利用）P0a 先行；时间/存在性类（知识同步延迟/
// 知识沉淀时机/复盘存在性）P1。P3 改进绑定前后对比（先测基线再改进），P0a2 只出方向性候选。
//
// 重复验证点：同一验证点（同一测试/同一真机验证请求）重复 N 次。实证负样本 = 01a013f3
// 8/20 10:16「我已经说了资云集始终无法触发 为什么你要一而再再而三的测试？」——agent 8/19
// 17:25「请 Xcode Run…再测」+ 8/20 09:05「你直接 Xcode Run 到真机测」在各自自然轮次内
// 重复多次。P3 改进：测试记录结构化（记录后 analyze 可先喊停）。
// 口径（Claude P0a2 审核 challenge 1，2026-08-24）：episode 边界 = fix commit **或跨夜休眠**
// （相邻事件 gap ≥ SleepGapHours 且跨日，T1 同源口径）——8/19 晚与 8/20 上午是两个自然验证
// 轮次，合并计算会把「12 次」跨天失真（实测切分 = 8/19 9 次 + 8/20 3 次）。
//
// 自建记录利用：自己/aipm 已有记录（bug/commit/task）在后续调试中是否被访问利用。实证：
// 01a013f3 8/19 15:32 record_bug（bug-20260819-153222-dd3d52）后 16:48-17:29 继续调试
// 17:29 才首次检索（search_discussions）；「15:32 的 bug 记录被无视 2 天」（SPEC §2.1）。
// P3 改进：代码↔任务索引。
// 口径（Claude P0a2 审核 challenge 2，2026-08-24）：DelayMin = 扣休眠后的工作延迟——8/18
// 17:54 create_task → 8/19 09:07 检索的 912min 含 14h 跨夜休眠，扣后 ≈ 工作延迟（小时级），
// 避免「延迟 912min 未利用」夸大。休眠段 = 相邻记录 gap ≥ SleepGapHours 且跨日（T1 同源）。

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// RepeatedVerificationParams 重复验证点参数。
type RepeatedVerificationParams struct {
	MinRequests int // 同 episode 内用户侧验证请求阈值（默认 2）
}

// DefaultRepeatedVerificationParams 默认参数。
func DefaultRepeatedVerificationParams() RepeatedVerificationParams {
	return RepeatedVerificationParams{MinRequests: 2}
}

// RepeatedVerificationCandidate 重复验证点候选：同一 episode（无 fix commit 间隔）内
// 多次向用户请求同一验证。
type RepeatedVerificationCandidate struct {
	EpisodeStart time.Time `json:"episode_start"`
	EpisodeEnd   time.Time `json:"episode_end"`
	Requests     []string  `json:"requests"` // 请求时间（"15:04"）
	Count        int       `json:"count"`
	Note         string    `json:"note,omitempty"`
}

// testRequestRe 用户侧验证请求（assistant 消息）：祈使前缀（请/麻烦/帮/去/直接/回头/稍后）+ 测/试/验证、
// Xcode Run、再测、等你…验证。实证锚点：8/19 17:25「现在请 Xcode Run 一次…再测」、
// 8/20 09:05「你直接 Xcode Run 到真机测」、10:52「等你真机验证」。
// 状态汇报误报排除：祈使前缀不含「先/现在」（「先尝试/先验证后动手/已验证/关键测试」为 agent
// 自述不命中）；「不用/不需要再测」无祈使前缀不命中（10:16 用户抗议的重复验证请求 = 祈使句
// 「一而再再而三的测试」，非叙述句）。
var testRequestRe = regexp.MustCompile(`Xcode Run|等你(?:测|试|验证)|(?:请|麻烦|帮忙|帮|去|直接|回头|稍后)[^。；]{0,12}(?:测|试|验证)|(?:请|麻烦|帮|去|直接|回头|稍后)[^。；]{0,6}再测`)

// DetectRepeatedVerification 重复验证点检测：
// episode = 连续记录段，以 git commit 命令为边界（fix commit 分隔验证轮次；「后期真机验证=合理」）。
// 每 episode 内统计用户侧验证请求（assistant 消息文本匹配 testRequestRe）；≥ MinRequests → 候选。
func DetectRepeatedVerification(turns []Turn, p RepeatedVerificationParams) []RepeatedVerificationCandidate {
	if p.MinRequests <= 0 {
		p.MinRequests = 2
	}
	// 时间有序事件流（user 消息 + assistant 记录），同时标记 episode 边界（git commit）
	type ev struct {
		ts     time.Time
		user   string
		rec    *Record
		commit bool
	}
	var evs []ev
	for i := range turns {
		t := &turns[i]
		if strings.TrimSpace(t.UserMsg) != "" {
			evs = append(evs, ev{ts: t.Start, user: t.UserMsg})
		}
		for j := range t.Records {
			r := &t.Records[j]
			commit := r.Tool.Tool == "bash" && strings.Contains(strings.ToLower(r.Tool.Command), "git commit")
			evs = append(evs, ev{ts: r.CreatedAt, rec: r, commit: commit})
		}
	}
	var out []RepeatedVerificationCandidate
	var epStart, epEnd, prevTs time.Time
	var reqs []time.Time
	flush := func(end time.Time) {
		if len(reqs) >= p.MinRequests {
			var times []string
			for _, r := range reqs {
				times = append(times, tsClock(r))
			}
			out = append(out, RepeatedVerificationCandidate{
				EpisodeStart: epStart, EpisodeEnd: end, Requests: times, Count: len(reqs),
				Note: fmt.Sprintf("同一验证点重复 %d 次（无 fix commit/休眠间隔）；测试记录结构化（P3）可先喊停", len(reqs)),
			})
		}
		reqs = nil
	}
	for i := range evs {
		e := &evs[i]
		if e.commit {
			flush(e.ts)
			epStart = time.Time{}
			epEnd = time.Time{}
			prevTs = e.ts
			continue
		}
		// 跨夜休眠（T1 同源口径：相邻事件 gap ≥ SleepGapHours 且跨日）= 新自然验证轮次
		if !prevTs.IsZero() && e.ts.Sub(prevTs) >= SleepGapHours && e.ts.Day() != prevTs.Day() {
			flush(e.ts)
			epStart = time.Time{}
			epEnd = time.Time{}
		}
		prevTs = e.ts
		if epStart.IsZero() {
			epStart = e.ts
		}
		epEnd = e.ts
		if e.rec != nil && isAssistantText(e.rec) && testRequestRe.MatchString(e.rec.Content) {
			reqs = append(reqs, e.ts)
		}
	}
	flush(epEnd)
	return out
}

// SelfRecordParams 自建记录利用参数。
type SelfRecordParams struct {
	WorkMin int // 无检索工作块阈值（默认 5 条工作记录）
}

// DefaultSelfRecordParams 默认参数。
func DefaultSelfRecordParams() SelfRecordParams {
	return SelfRecordParams{WorkMin: 5}
}

// SelfRecordCandidate 自建记录未利用候选：记录创建后，工作块内零 aipm 检索。
type SelfRecordCandidate struct {
	CreatedAt      time.Time `json:"created_at"`
	Kind           string    `json:"kind"` // record_bug / record_commit / create_task / record_decision
	BlockStart     time.Time `json:"block_start"`
	FirstConsultAt time.Time `json:"first_consult_at,omitempty"` // 零值 = 至会话结束未检索
	WorkRecords    int       `json:"work_records"`
	DelayMin       int       `json:"delay_min"` // BlockStart → 首次检索（或会话结束）
	Note           string    `json:"note,omitempty"`
}

// recordCreateRe 自建记录创建工具（aipm_record_* / create_task）。
var recordCreateRe = regexp.MustCompile(`record_bug|record_commit|create_task|record_decision`)

// isAipmConsult 记录利用检索：get/search/trace/list（状态读取与定向检索均可能命中记录）；
// read_discussions（例行）不算（T4 边界同源）。
func isAipmConsult(t ToolRecord) bool {
	switch t.Tool {
	case "mcp_aipm_get", "mcp_aipm_search", "mcp_aipm_trace", "mcp_aipm_list":
		return true
	}
	return false
}

// isWorkRecord 工作记录：非 LLM 文本、非 aipm 工具的活动（edit/write/bash/read 等）。
func isWorkRecord(r *Record) bool {
	if isAssistantText(r) {
		return false
	}
	if r.Tool.Tool == "llm_message" || strings.HasPrefix(r.Tool.Tool, "mcp_aipm_") {
		return false
	}
	return true
}

// isAssistantText assistant 文本消息：llm_message（assistant_message 格式）或
// unknown + 有内容（_type:stop 的 last_assistant_message 行，01a013f3 实测为 unknown 但
// content 列含全文）——T8/P0a2 侧不依赖 Tool 归属，只认「非工具调用的 assistant 文本」。
func isAssistantText(r *Record) bool {
	if r == nil || r.Role != "assistant" {
		return false
	}
	switch r.Tool.Tool {
	case "llm_message":
		return true
	case "unknown":
		return strings.TrimSpace(r.Content) != ""
	}
	return false
}

// DetectSelfRecordUsage 自建记录利用检测：
// 扫描记录创建事件（record_bug/record_commit/create_task/record_decision，分钟级去重）；
// 每个创建事件向后看：到首次 aipm 检索（get/search/trace/list；read_discussions 例行不算）
// 或会话结束前累计的工作记录 ≥ WorkMin 且其间零检索 → 候选（一条创建一条候选，防噪声）。
func DetectSelfRecordUsage(turns []Turn, p SelfRecordParams) []SelfRecordCandidate {
	if p.WorkMin <= 0 {
		p.WorkMin = 5
	}
	var all []Record
	for i := range turns {
		for j := range turns[i].Records {
			all = append(all, turns[i].Records[j])
		}
	}
	var out []SelfRecordCandidate
	sleeps := recordSleepRanges(all)
	var lastCreate time.Time
	for i := range all {
		r := &all[i]
		if strings.HasPrefix(r.Tool.Tool, "mcp_aipm_") && recordCreateRe.MatchString(r.Tool.Command) {
			// 分钟级去重（record_bug + create_task 同秒双事件只锚一次）
			if !lastCreate.IsZero() && r.CreatedAt.Sub(lastCreate) < 2*time.Minute {
				continue
			}
			lastCreate = r.CreatedAt
			if c, ok := scanRecordUsage(all, i, r, p.WorkMin, sleeps); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

// recordSleepRanges 记录流中的跨夜休眠段（相邻记录 gap ≥ SleepGapHours 且跨日，T1 同源口径）。
func recordSleepRanges(all []Record) []SleepRange {
	var out []SleepRange
	var prev time.Time
	for i := range all {
		ts := all[i].CreatedAt
		if !prev.IsZero() && ts.Sub(prev) >= SleepGapHours && ts.Day() != prev.Day() {
			out = append(out, SleepRange{Start: prev, End: ts})
		}
		prev = ts
	}
	return out
}

// awakeMinutes 扣休眠后的工作分钟数（[from, to] 内总时长减去休眠段重叠）。
func awakeMinutes(from, to time.Time, sleeps []SleepRange) int {
	total := int(to.Sub(from).Minutes())
	for _, s := range sleeps {
		st, en := s.Start, s.End
		if st.Before(from) {
			st = from
		}
		if en.After(to) {
			en = to
		}
		if en.After(st) {
			total -= int(en.Sub(st).Minutes())
		}
	}
	if total < 0 {
		total = 0
	}
	return total
}

// scanRecordUsage 从创建事件位置向后扫描到首次 aipm 检索（或会话结束）。
// DelayMin 扣休眠（awakeMinutes），避免跨夜休眠夸大「未利用」严重性（Claude challenge 2）。
func scanRecordUsage(all []Record, idx int, created *Record, workMin int, sleeps []SleepRange) (SelfRecordCandidate, bool) {
	work := 0
	var consultAt time.Time
	for i := idx + 1; i < len(all); i++ {
		r := &all[i]
		if isAipmConsult(r.Tool) {
			consultAt = r.CreatedAt
			break
		}
		if isWorkRecord(r) {
			work++
		}
	}
	if work < workMin {
		return SelfRecordCandidate{}, false
	}
	c := SelfRecordCandidate{
		CreatedAt: created.CreatedAt, Kind: created.Tool.Command,
		BlockStart: created.CreatedAt, WorkRecords: work,
	}
	end := all[len(all)-1].CreatedAt
	if !consultAt.IsZero() {
		c.FirstConsultAt = consultAt
		end = consultAt
		c.Note = "记录创建后该记录在后续调试未被访问利用（工作期间零 aipm 检索），首次检索后才消费"
	} else {
		c.Note = "记录创建后至会话结束零 aipm 检索（该记录从未被后续调试访问）"
	}
	c.DelayMin = awakeMinutes(created.CreatedAt, end, sleeps)
	return c, true
}

// FormatRepeatedVerificationHuman 人类可读输出。
func FormatRepeatedVerificationHuman(cands []RepeatedVerificationCandidate) string {
	if len(cands) == 0 {
		return "  重复验证点：无候选\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "  重复验证点候选 %d 个（同一验证点重复请求，P3 测试记录结构化可先喊停）\n", len(cands))
	for _, c := range cands {
		fmt.Fprintf(&sb, "    episode %s→%s 请求 %d 次：%s\n", tsClock(c.EpisodeStart), tsClock(c.EpisodeEnd), c.Count, strings.Join(c.Requests, ", "))
	}
	return sb.String()
}

// FormatSelfRecordHuman 人类可读输出。
func FormatSelfRecordHuman(cands []SelfRecordCandidate) string {
	if len(cands) == 0 {
		return "  自建记录利用：无候选\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "  自建记录利用候选 %d 个（记录创建后工作块零 aipm 检索）\n", len(cands))
	for _, c := range cands {
		consult := "至会话结束未检索"
		if !c.FirstConsultAt.IsZero() {
			consult = fmt.Sprintf("%s 才首次检索", tsClock(c.FirstConsultAt))
		}
		fmt.Fprintf(&sb, "    %s [%s] 工作 %d 条 %s（延迟 %d 分钟）\n", tsClock(c.CreatedAt), c.Kind, c.WorkRecords, consult, c.DelayMin)
	}
	return sb.String()
}
