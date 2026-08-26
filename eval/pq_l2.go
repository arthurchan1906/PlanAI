package eval

// P1b L2 语义层确认器（PROCESS_QUALITY_SPEC §2.2，prompt 三约束）。
// 五任务：断言分类 / 证据细配对 / 死循环确认 / 方向评估 / 反馈响应确认。
// Prompt 三约束（§2.2）：① 系统声明只基于提供证据判定；② 证据上下文必填（命令截断）；
// ③ 输出强制 JSON、单一职责。输入组装为纯函数（可测）；LLM 通道复用 ai.SummarizeJSON
// 模式（classify.go 同款接口注入替身，nil = 不可用，L2 降级由调用方决定）。

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// L2Task L2 任务类型。
type L2Task string

const (
	L2ClaimClassify    L2Task = "claim_classify"    // 断言分类
	L2EvidenceMatch    L2Task = "evidence_match"    // 证据细配对
	L2DeadloopConfirm  L2Task = "deadloop_confirm"  // 死循环确认
	L2DirectionEval    L2Task = "direction_eval"    // 方向评估
	L2FeedbackResponse L2Task = "feedback_response" // 反馈响应确认
)

// L2Prompt 单任务确认输入（prompt 三约束：证据必填、命令截断）。
type L2Prompt struct {
	Task     L2Task `json:"task"`
	System   string `json:"system"`   // 系统指令（三约束 + 任务 + JSON schema）
	Evidence string `json:"evidence"` // 证据上下文（命令/文本截断）
}

// L2Confirmer LLM 确认通道（测试注入替身；nil = 不可用）。
type L2Confirmer interface {
	Confirm(p L2Prompt) (string, error)
}

// L2Client 基于 ai.Client.SummarizeJSON 的确认器实现。
type L2Client struct {
	Summarizer interface {
		SummarizeJSON(text, instruction string) (string, error)
	}
}

// Confirm 调用 LLM：System = 系统指令，Evidence = 用户侧证据文本。
func (c *L2Client) Confirm(p L2Prompt) (string, error) {
	if p.Evidence == "" {
		return "", fmt.Errorf("L2 %s: 证据上下文为空（prompt 三约束②）", p.Task)
	}
	return c.Summarizer.SummarizeJSON(p.Evidence, p.System)
}

// l2CommonConstraints prompt 三约束前缀（§2.2），每个任务系统指令共用。
const l2CommonConstraints = "你是行为测量管线的 L2 语义确认器，只做单一职责任务。只基于下方「证据」字段提供的内容判定，不得使用证据之外的假设或记忆。证据中的命令/文本已截断（截断不代表缺失证据）。输出严格 JSON——不要 markdown 代码块、不要任何额外文字，字段与枚举严格按 schema。"

// ── 任务 1：断言分类（L3 字段 claim_type）──

// ClaimClassifyResult 断言分类结果：事实/意见/摘要/进度。
type ClaimClassifyResult struct {
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
}

// BuildClaimClassifyPrompt 断言分类输入：候选断言 + 前序工具记录（命令截断）。
func BuildClaimClassifyPrompt(claim string, priorCommands []string) L2Prompt {
	system := l2CommonConstraints + `
任务：把候选断言分类为四类之一——
- 事实：事实性陈述，可由证据验证真伪（如「问题在 X 文件」「错误是 Y」）
- 意见：评估、判断、推测（如「我认为」「可能是」「看起来」）
- 摘要：对已完成动作的回顾性描述（如「我检查了」「运行了」「改了」）
- 进度：对当前任务进展/完成状态的报告（如「已修复」「完成」「准备提交」）
输出 JSON：{"type":"事实|意见|摘要|进度","confidence":0.0-1.0}`
	evidence := fmt.Sprintf("候选断言：%s\n\n前序工具记录（最近 %d 条，已截断）：\n%s",
		claim, len(priorCommands), strings.Join(priorCommands, "\n"))
	return L2Prompt{Task: L2ClaimClassify, System: system, Evidence: evidence}
}

// ParseClaimClassify 解析断言分类 JSON。
func ParseClaimClassify(out string) (ClaimClassifyResult, error) {
	var r ClaimClassifyResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return r, fmt.Errorf("L2 断言分类 JSON 解析失败: %w", err)
	}
	switch r.Type {
	case "事实", "意见", "摘要", "进度":
	default:
		return r, fmt.Errorf("L2 断言分类非法 type %q", r.Type)
	}
	if r.Confidence < 0 || r.Confidence > 1 {
		r.Confidence = 0
	}
	return r, nil
}

// ── 任务 2：证据细配对（L3 字段 evidence_match；只确认「真无证据」）──

// EvidenceMatchResult 证据细配对结果：强/弱/无。
type EvidenceMatchResult struct {
	Match string `json:"match"`
	Basis string `json:"依据"`
}

// BuildEvidenceMatchPrompt 证据细配对输入：事实断言 + 前序工具对象。
// 触发：L1 粗配对（关键词匹配）无命中才调用——prompt 明示「只确认真无证据」。
func BuildEvidenceMatchPrompt(claim string, priorObjects []string) L2Prompt {
	system := l2CommonConstraints + `
任务：判定事实断言中的具体事实是否被前序访问的工具对象（文件路径/命令）支撑——
- match=强：前序证据直接支撑断言核心事实（对象命中 + 操作内容相关）
- match=弱：证据部分相关但未直接支撑核心事实（只沾边对象/只读未改）
- match=无：找不到任何支撑——只确认「真无证据」才算无，不得因证据截断或不完整就判无
输出 JSON：{"match":"强|弱|无","依据":"引用具体证据（对象名/命令摘要）"}`
	evidence := fmt.Sprintf("事实断言：%s\n\n前序工具对象（已截断）：\n%s",
		claim, strings.Join(priorObjects, "\n"))
	return L2Prompt{Task: L2EvidenceMatch, System: system, Evidence: evidence}
}

// ParseEvidenceMatch 解析证据细配对 JSON。
func ParseEvidenceMatch(out string) (EvidenceMatchResult, error) {
	var r EvidenceMatchResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return r, fmt.Errorf("L2 证据细配对 JSON 解析失败: %w", err)
	}
	switch r.Match {
	case "强", "弱", "无":
	default:
		return r, fmt.Errorf("L2 证据细配对非法 match %q", r.Match)
	}
	return r, nil
}

// ── 任务 3：死循环确认（L3 字段 is_deadloop）──

// DeadloopConfirmResult 死循环确认结果。
type DeadloopConfirmResult struct {
	IsDeadloop    bool    `json:"is_deadloop"`
	RepeatPattern string  `json:"repeat_pattern"`
	Confidence    float64 `json:"confidence"`
}

// BuildDeadloopConfirmPrompt 死循环确认输入：候选段行为序列（T5 DeadloopCandidate 同源）。
func BuildDeadloopConfirmPrompt(c DeadloopCandidate, commands []string) L2Prompt {
	system := l2CommonConstraints + `
任务：判定候选时段是否为死循环（重复盲试）——特征：同命令反复重试、无新信息获取（无检索/无新对象读取）、无进展信号（无 edit/commit/根因定位）。重复模式 = 观察到的具体重复模式（如「同一构建命令反复执行，中间无任何修改/分析」）。只基于证据判定；未观察到重复盲试 → is_deadloop=false。排除：有 edit/commit/根因定位的时段不是死循环。
输出 JSON：{"is_deadloop":true|false,"repeat_pattern":"...","confidence":0.0-1.0}`
	evidence := fmt.Sprintf("候选时段：%s → %s\n组合信号：build=%d fail=%d 自发检索=%d 被动检索=%d edit=%d 根因=%d 排除=%v（%s）\n\n行为序列（已截断）：\n%s",
		c.Start.Format("2006-01-02 15:04:05"), c.End.Format("2006-01-02 15:04:05"),
		c.Builds, c.Fails, c.SpontRetr, c.Passive, c.Edits, c.RootCause,
		c.Excluded, c.Reason,
		strings.Join(commands, "\n"))
	return L2Prompt{Task: L2DeadloopConfirm, System: system, Evidence: evidence}
}

// ParseDeadloopConfirm 解析死循环确认 JSON。
func ParseDeadloopConfirm(out string) (DeadloopConfirmResult, error) {
	var r DeadloopConfirmResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return r, fmt.Errorf("L2 死循环确认 JSON 解析失败: %w", err)
	}
	if r.Confidence < 0 || r.Confidence > 1 {
		r.Confidence = 0
	}
	return r, nil
}

// ── 任务 4：方向评估（L3 字段 direction_ok；触发条件见 DirectionEvalTriggered）──

// DirectionEvalResult 方向评估结果：只判「历史检索信号缺失是否被确认」。
type DirectionEvalResult struct {
	DirectionOK bool   `json:"direction_ok"`
	Note        string `json:"note"`
}

// DirectionEvalTriggered 任务 4 触发条件（§2.2）：仅当 L1 已标记「候选方向错」
// （技术动作密集 + 自发检索 < 2）时才调用。
func DirectionEvalTriggered(spontRetr, builds, buildMin int) bool {
	return spontRetr < 2 && builds >= buildMin
}

// BuildDirectionEvalPrompt 方向评估输入：候选段行为序列 + 问题上下文。
// 「应该查什么」仅作建议（note 内标注），不进标注项（§2.2）。
func BuildDirectionEvalPrompt(problem string, commands []string, spontRetr int) L2Prompt {
	system := l2CommonConstraints + `
任务：只判定一件事——候选段是否缺少对问题上下文的历史检索（L1 标记「候选方向错」= 技术动作密集 + 自发检索 < 2 是否被证据确认）。
- direction_ok=false：确认该段确实缺少对问题上下文的检索（历史检索信号缺失成立）
- direction_ok=true：检索其实存在，或缺失判定不成立
禁止把「应该查什么」当作判定依据；如确有必要可在 note 给出建议，但必须标注「(建议)」且不影响 direction_ok。
输出 JSON：{"direction_ok":true|false,"note":"..."}`
	evidence := fmt.Sprintf("问题上下文（用户原话/任务目标）：%s\n\n候选段自发检索次数：%d\n\n候选段行为序列（已截断）：\n%s",
		problem, spontRetr, strings.Join(commands, "\n"))
	return L2Prompt{Task: L2DirectionEval, System: system, Evidence: evidence}
}

// ParseDirectionEval 解析方向评估 JSON。
func ParseDirectionEval(out string) (DirectionEvalResult, error) {
	var r DirectionEvalResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return r, fmt.Errorf("L2 方向评估 JSON 解析失败: %w", err)
	}
	return r, nil
}

// ── 任务 5：反馈响应确认（五子信号 L2 确认，v1.3 新增）──

// FeedbackResponseResult 反馈响应确认：五子信号 + matched_object。
type FeedbackResponseResult struct {
	Responded     bool   `json:"responded"`
	Deepened      bool   `json:"deepened"`
	Sustained     bool   `json:"sustained"`
	Aligned       bool   `json:"aligned"`
	MatchedObject string `json:"matched_object"`
	Note          string `json:"note"`
}

// BuildFeedbackResponsePrompt 反馈响应确认输入：候选反馈事件 + 反馈后 20min 行为序列。
func BuildFeedbackResponsePrompt(fb FeedbackCandidate, windowCommands []string) L2Prompt {
	system := l2CommonConstraints + `
任务：判定反馈后 20 分钟行为序列的五子信号——
- responded：20 分钟内是否有响应动作（工具调用/检索/修改）
- deepened：响应是否深入问题（新检索/新对象/定位动作）而非表面应付（复述/重复旧动作）
- sustained：是否持续跟进而非单次动作（窗口内 ≥2 个不同动作或持续到窗口末）
- aligned：响应方向是否与反馈所指一致
- matched_object：反馈所指实体 vs 响应行为的匹配对象；无匹配填「无」
只基于证据判定；窗口内无行为 → responded=false 且其余信号默认 false。
输出 JSON：{"responded":true|false,"deepened":true|false,"sustained":true|false,"aligned":true|false,"matched_object":"...","note":"..."}`
	evidence := fmt.Sprintf("用户反馈（%s，%s）：%s\n反馈所指实体（L1 候选）：%s\n\n反馈后 20 分钟行为序列（已截断）：\n%s",
		fb.Kind, fb.Ts.Format("2006-01-02 15:04:05"), fb.Snippet,
		strings.Join(fb.Referents, "/"),
		strings.Join(windowCommands, "\n"))
	return L2Prompt{Task: L2FeedbackResponse, System: system, Evidence: evidence}
}

// ParseFeedbackResponse 解析反馈响应确认 JSON。
func ParseFeedbackResponse(out string) (FeedbackResponseResult, error) {
	var r FeedbackResponseResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return r, fmt.Errorf("L2 反馈响应确认 JSON 解析失败: %w", err)
	}
	return r, nil
}

// ── 证据组装辅助 ──

// l2CommandLines 记录 → 截断命令行（「HH:MM 工具 命令/摘要」），limit 条内、单条截断 maxLen。
func l2CommandLines(recs []Record, limit, maxLen int) []string {
	if limit <= 0 {
		limit = 20
	}
	if maxLen <= 0 {
		maxLen = 200
	}
	var out []string
	for i, r := range recs {
		if i >= limit {
			break
		}
		line := r.CreatedAt.Format("15:04") + " " + r.Tool.Tool
		cmd := strings.TrimSpace(r.Tool.Command)
		if cmd == "" {
			cmd = strings.TrimSpace(r.Content)
		}
		cmd = strings.ReplaceAll(cmd, "\n", " ")
		if len(cmd) > maxLen {
			cmd = cmd[:maxLen] + "…"
		}
		if cmd != "" {
			line += " " + cmd
		}
		out = append(out, line)
	}
	return out
}

// l2RecordsBetween 窗口内记录（[from, to]，含端点；时间有序）。
func l2RecordsBetween(turns []Turn, from, to time.Time) []Record {
	var out []Record
	for i := range turns {
		for j := range turns[i].Records {
			r := turns[i].Records[j]
			if !r.CreatedAt.Before(from) && !r.CreatedAt.After(to) {
				out = append(out, r)
			}
		}
	}
	return out
}

// claimSentenceRe 声称句关键词（低精度高召回，L1 候选生成；L2 断言分类确认）。
var claimSentenceRe = regexp.MustCompile(`(完成|修复|通过|定位|找到|原因|解决|搞定|验证|测过|成功|失败|实锤|已|确认|问题在|错误是|根因|提交)`)

// claimSplitRe 句子切分（中英文句末标点）。
var claimSplitRe = regexp.MustCompile(`[。！？!?；;]`)

// CandidateClaims 从 assistant 文本记录提取候选断言句（含声称关键词的完整句，去重）。
// 供任务 1（断言分类）与任务 2（证据细配对）的 L1 候选生成。
func CandidateClaims(recs []Record) []string {
	var out []string
	seen := map[string]bool{}
	for i := range recs {
		r := &recs[i]
		if r.Role != "assistant" || !isAssistantText(r) {
			continue
		}
		for _, s := range claimSplitRe.Split(r.Content, -1) {
			s = strings.TrimSpace(s)
			n := len([]rune(s))
			if n < 4 || n > 200 || !claimSentenceRe.MatchString(s) || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
