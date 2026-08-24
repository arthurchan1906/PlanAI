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
	Referents []string     `json:"referents,omitempty"` // 反馈所指实体（L1 正则候选，T8 对准近似消费；精确判定归 L2 matched_object，P1 接入）
	TextClass int          `json:"text_class"`          // 1/2/4/5（对应文本层判别规则）
	Snippet   string       `json:"snippet"`
}

// 纠偏关键词（SPEC §2.1 冻结清单）。
var correctionKeywords = []string{"此前", "之前", "应该", "查", "历史", "记录", "提交记录", "以前"}
var negationKeywords = []string{"方向不对", "不对", "错了", "重新审视"}
var progressKeywords = []string{"继续", "可以", "好"}

var moduleStructuredRe = regexp.MustCompile(`^(?:\[[A-Za-z0-9_-]+\][^\n]*\n?)+$`)

// RecognizeFeedback 对 session 全部 user 消息做反馈识别。
// 返回候选列表 + 两级计数（介入 / 纠偏）。
// modern 为「现代通道」（019f 时代，ED 库 6/26 起，v1.3 七轮口径）：规则⑤
// （无 CJK 手动输入）只输出介入候选清单不直接计数，交 P1 L2 确认（与②存疑同处理）。
func RecognizeFeedback(turns []Turn, modern bool) ([]FeedbackCandidate, FeedbackCounts) {
	var out []FeedbackCandidate
	var counts FeedbackCounts
	for i := range turns {
		t := &turns[i]
		// 孤立回合（无 user 前置的 assistant/tool 记录）UserMsg 为空，
		// classifyUserText("") 会误判为 ⑤ 手动输入污染介入计数（Claude 审核 8/24）。
		if strings.TrimSpace(t.UserMsg) == "" {
			continue
		}
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
		// ⑤ 现代通道：介入候选不直接计数（P1 L2 确认后回填）
		if c.Class == 5 && modern {
			out = append(out, FeedbackCandidate{
				UserMsgID: t.UserMsgID, Ts: t.Start, Kind: KindManual,
				Referents: extractReferents(t.UserMsg), TextClass: 5, Snippet: snippetOf(t.UserMsg),
			})
			counts.ManualCandidates++
			continue
		}
		// ① ⑤（legacy）用户介入
		counts.Intervention++
		kw := matchKeywords(t.UserMsg)
		refs := extractReferents(t.UserMsg)
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
			Keywords: append(kw.Correction, kw.Progress...), Referents: refs, TextClass: c.Class,
			Snippet: snippetOf(t.UserMsg),
		})
	}
	return out, counts
}

// FeedbackCounts 两级计数（介入 ⊇ 纠偏）。
type FeedbackCounts struct {
	Intervention     int `json:"intervention"`      // ①⑤(legacy) 合计
	Correction       int `json:"correction"`        // 纠偏关键词/否定方向词命中
	Progress         int `json:"progress"`          // 推进
	Suspicious       int `json:"suspicious"`        // ② 存疑（P0a1 标记，P1 L2 判定）
	Injection        int `json:"injection"`         // ④ 系统通知排除
	ManualCandidates int `json:"manual_candidates"` // ⑤ 现代通道介入候选（P1 L2 确认后回填）
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
// hasCJK 汉字/假名/谚文任一即视为 CJK 文本（名称与行为一致，Claude 审核 8/24：
// 原实现只覆盖汉字区，日文假名/韩文谚文未覆盖）。
func hasCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF || r >= 0x3400 && r <= 0x4DBF || r >= 0xF900 && r <= 0xFAFF || // 汉字
			r >= 0x3040 && r <= 0x30FF || // 平假名/片假名
			r >= 0xAC00 && r <= 0xD7AF || r >= 0x1100 && r <= 0x11FF { // 谚文音节/字母
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


// extractReferents 反馈所指实体 L1 提取（T8 对准近似消费，规格 §2.1：所指实体由 T3 提取输出）。
// 规则：用户消息 CJK 2-4 字段，去停用词/纠偏关键词/方位词，去重。
// 精确判定归 L2 matched_object（P1 接入），L1 只做候选（低精度高召回）。
func extractReferents(msg string) []string {
	seen := map[string]bool{}
	var out []string
	for _, seg := range cjkSegments(msg) {
		runes := []rune(seg)
		n := len(runes)
		for l := 2; l <= 4 && l <= n; l++ {
			for i := 0; i+l <= n; i++ {
				w := string(runes[i : i+l])
				if sceneStopWord(w) || referentStopWord(w) {
					continue
				}
				if !seen[w] {
					seen[w] = true
					out = append(out, w)
				}
			}
		}
	}
	// 优先保留 2-4 字段整词（如「打开方式」优于「打开」），被更长词包含的短词去除
	var merged []string
	for _, w := range out {
		absorbed := false
		for _, o := range out {
			if o != w && len([]rune(o)) > len([]rune(w)) && strings.Contains(o, w) {
				absorbed = true
				break
			}
		}
		if !absorbed {
			merged = append(merged, w)
		}
	}
	return merged
}

// referentStopWord 反馈消息中的泛词/纠偏关键词/方位词（区别于场景词提取的停用词表）。
func referentStopWord(w string) bool {
	if referentStopWords[w] {
		return true
	}
	for _, ch := range w {
		if strings.ContainsRune("时里中上下前后这那新旧同样的都还也再就", ch) {
			return true
		}
	}
	return false
}

var referentStopWords = map[string]bool{
	"此前": true, "之前": true, "以前": true, "历史": true, "记录": true,
	"提交记录": true, "应该": true, "方向": true, "问题": true,
	"继续": true, "还有": true, "已经": true, "没有": true, "可以": true,
	"这个": true, "那个": true, "需要": true, "看看": true, "一个": true,
	"重新": true, "不要": true, "一直": true, "怎么": true, "什么": true,
	"就是": true, "还是": true, "现在": true, "重新审视": true,
	"能不能": true, "麻烦": true, "请": true, "一下": true, "的话": true,
}
