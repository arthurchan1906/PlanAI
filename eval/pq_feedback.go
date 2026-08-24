package eval

// P0a1a T3 反馈识别（PROCESS_QUALITY_SPEC §2.1 用户介入，口径冻结）。
// 统一判别 = 文本层（metadata 来源层无判别力，v1.3 实证）：
//   ① 含 CJK 自然语言 = 用户介入计入（纠偏关键词可匹配）
//   ② 纯结构化（[模块] 开头 + 无 CJK） = 存疑候选：标记、不排除、不计入纠偏（P0a1 无 L2）
//   ③ 重复出现不做判别依据
//   ④ <task-notification> XML 前缀 = 系统注入，显式排除
//   ⑤ 无 CJK 且非 [模块] 行首且非系统通知 = 用户手动输入（证据/命令/确认），计入介入、不参与纠偏计数
// 两级计数：介入 ⊇ 纠偏。T3 验收：c0ad2534 25 次纠偏召回 ≥80%。

import (
	"regexp"
	"strings"
	"time"
)

// FeedbackKind 反馈候选类型。
type FeedbackKind string

const (
	KindCorrection FeedbackKind = "纠偏"      // 纠偏关键词/否定方向词命中
	KindProgress   FeedbackKind = "推进"      // 继续/可以/好
	KindSuspicious FeedbackKind = "存疑"      // ② 纯结构化，P0a1 标记不排除
	KindInjection  FeedbackKind = "hook_注入" // ④ 系统通知，显式排除
	KindManual     FeedbackKind = "介入"      // ⑤ 用户手动输入（证据/命令/确认）
)

// FeedbackCandidate 一条 user 消息的反馈识别结果。
type FeedbackCandidate struct {
	UserMsgID string       `json:"user_msg_id"`
	Ts        time.Time    `json:"ts"`
	Kind      FeedbackKind `json:"kind"`
	Keywords  []string     `json:"keywords,omitempty"`
	TextClass int          `json:"text_class"` // 1/2/4/5（对应文本层判别规则）
	Snippet   string       `json:"snippet"`
}

// 纠偏关键词（SPEC §2.1 冻结清单）。
var correctionKeywords = []string{"此前", "之前", "应该", "查", "历史", "记录", "提交记录", "以前"}
var negationKeywords = []string{"方向不对", "不对", "错了", "重新审视"}
var progressKeywords = []string{"继续", "可以", "好"}

var moduleStructuredRe = regexp.MustCompile(`^(?:\[[A-Za-z0-9_-]+\][^\n]*\n?)+$`)

// RecognizeFeedback 对 session 全部 user 消息做反馈识别。
// 返回候选列表 + 两级计数（介入 / 纠偏）。
func RecognizeFeedback(turns []Turn) ([]FeedbackCandidate, FeedbackCounts) {
	var out []FeedbackCandidate
	var counts FeedbackCounts
	for i := range turns {
		t := &turns[i]
		c := classifyUserText(t.UserMsg)
		// ② 存疑：不参与纠偏关键词匹配，只标记
		if c.Class == 2 {
			out = append(out, FeedbackCandidate{
				UserMsgID: t.UserMsgID, Ts: t.Start, Kind: KindSuspicious,
				TextClass: 2, Snippet: snippetOf(t.UserMsg),
			})
			counts.Suspicious++
			continue
		}
		// ④ 系统通知：显式排除
		if c.Class == 4 {
			out = append(out, FeedbackCandidate{
				UserMsgID: t.UserMsgID, Ts: t.Start, Kind: KindInjection,
				TextClass: 4, Snippet: snippetOf(t.UserMsg),
			})
			counts.Injection++
			continue
		}
		// ① ⑤ 用户介入（CJK 或手动输入）
		counts.Intervention++
		kw := matchKeywords(t.UserMsg)
		kind := KindManual
		switch {
		case len(kw.Correction) > 0:
			kind = KindCorrection
			counts.Correction++
		case len(kw.Progress) > 0:
			kind = KindProgress
		}
		out = append(out, FeedbackCandidate{
			UserMsgID: t.UserMsgID, Ts: t.Start, Kind: kind,
			Keywords: append(kw.Correction, kw.Progress...), TextClass: c.Class,
			Snippet: snippetOf(t.UserMsg),
		})
	}
	return out, counts
}

// FeedbackCounts 两级计数（介入 ⊇ 纠偏）。
type FeedbackCounts struct {
	Intervention int `json:"intervention"` // ①⑤ 合计
	Correction   int `json:"correction"`   // 纠偏关键词/否定方向词命中
	Progress     int `json:"progress"`     // 推进
	Suspicious   int `json:"suspicious"`   // ② 存疑（P0a1 标记，P1 L2 判定）
	Injection    int `json:"injection"`    // ④ 系统通知排除
}

// textClass 文本层判别结果。
type textClass struct {
	Class int // 1 CJK / 2 纯结构化 / 4 系统通知 / 5 手动输入
}

// classifyUserText 按 SPEC §2.1 ①-⑤ 判别 user 消息文本层。
func classifyUserText(content string) textClass {
	c := strings.TrimSpace(content)
	if strings.HasPrefix(c, "<task-notification>") {
		return textClass{Class: 4}
	}
	if hasCJK(c) {
		return textClass{Class: 1}
	}
	if moduleStructuredRe.MatchString(c) {
		return textClass{Class: 2}
	}
	return textClass{Class: 5}
}

// keywordMatch 纠偏/推进关键词命中结果。
type keywordMatch struct {
	Correction []string
	Progress   []string
}

// matchKeywords 纠偏关键词 + 否定方向词 + 推进词匹配（大小写不敏感，子串）。
func matchKeywords(content string) keywordMatch {
	var m keywordMatch
	lower := strings.ToLower(content)
	for _, k := range correctionKeywords {
		if strings.Contains(lower, strings.ToLower(k)) {
			m.Correction = append(m.Correction, k)
		}
	}
	for _, k := range negationKeywords {
		if strings.Contains(lower, strings.ToLower(k)) {
			m.Correction = append(m.Correction, k)
		}
	}
	for _, k := range progressKeywords {
		if strings.Contains(lower, strings.ToLower(k)) {
			m.Progress = append(m.Progress, k)
		}
	}
	return m
}

// hasCJK 是否含 CJK 字符（中文/日文/韩文统一区）。
func hasCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF || r >= 0x3400 && r <= 0x4DBF || r >= 0xF900 && r <= 0xFAFF {
			return true
		}
	}
	return false
}

func snippetOf(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 80 {
		return s
	}
	return s[:80] + "…"
}
