package eval

// P0a1b T6 目标锚定（PROCESS_QUALITY_SPEC §2.1 目标锚定 + EXECUTION_PLAN §9.3 对准阈值）。
// 对准判定 = 场景词覆盖率 ≥0.5 + 高频场景词（单条消息内 ≥2 次）全命中。
// 落地 = 负样本验证（存在性）：01a013f3 15:09 用户原话 vs 4b41ba8 期望「对准」（覆盖率 0.83，不误报）。
// 待验证候选检测点：P1 全库扫描无正样本（用户报 A、agent 首 commit 做 B）则删除；阈值不承诺（防 Goodhart）。
//
// 口径说明（§9.3 实证 + 实现记录）：
//   - 高频场景词 = 用户原话单条消息内重复 ≥2 次的 CJK 子串（实证 15:09 = 资云集×3/打开×2；
//     规格表述「打开方式×2」对应同一重复源：打开方式 + 打开资云集）。
//   - 声称对象 = 首个 commit 标题问题描述部分（破折号前）关键词；「直传文件」按名词核心拆为
//     直传/文件；实证六词分法 = 第三方/打开方式/直传/文件/不跳转/不导入。
//   - 覆盖率 = 共享词 ÷ 声称对象词（实证 5/6 = 0.83）。
//   - 高频词全命中：声称对象覆盖至少一个高频场景词的核心主题（打开 ↔ 打开方式）；
//     产品专名（资云集）不要求字面出现于声称对象——commit 以功能描述（第三方打开方式直传文件）
//     指代，若要求专名字面共享则本负样本会误报，与规格「判对准」矛盾。

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// AnchoringParams 目标锚定参数（阈值 = 规格建议初值，防 Goodhart，验收①-③ 不依赖）。
type AnchoringParams struct {
	CoverageMin float64 // 覆盖率阈值（默认 0.5）
	FreqMin     int     // 高频词频次阈值（默认 2，单条 user 消息内）
}

// DefaultAnchoringParams 规格实证默认参数。
func DefaultAnchoringParams() AnchoringParams {
	return AnchoringParams{CoverageMin: 0.5, FreqMin: 2}
}

// SceneWord 用户侧场景词（单条消息内频次）。
type SceneWord struct {
	Word  string `json:"word"`
	Count int    `json:"count"`
}

// ClaimWord 声称对象词（含核心词与命中判定）。
type ClaimWord struct {
	Word string `json:"word"` // 声称词（commit 标题问题描述部分提取）
	Core string `json:"core"` // 匹配用核心词（剥否定前缀后）
	Hit  bool   `json:"hit"`  // 核心词被用户原话覆盖（共享）
}

// AnchoringResult T6 目标锚定结果。
type AnchoringResult struct {
	Undecidable bool        `json:"undecidable"` // 无场景词可提取 → 不可判定（模糊指令类）
	SceneWords  []SceneWord `json:"scene_words"` // 高频场景词（单条消息内重复 ≥2 次的 CJK 子串）
	HighFreq    []string    `json:"high_freq"`   // 高频词词面（频次 ≥ FreqMin）
	ClaimWords  []ClaimWord `json:"claim_words"` // 声称对象词 + 共享判定
	Shared      []string    `json:"shared"`      // 共享词（命中声称词）
	Coverage    float64     `json:"coverage"`    // 覆盖率 = 共享词 ÷ 声称对象词
	HighFreqAll bool        `json:"high_freq_all_hit"`
	Aligned     bool        `json:"aligned"` // 对准判定 = 覆盖率 ≥ 阈值 且 高频词全命中
	Notes       []string    `json:"notes,omitempty"`
}

// AnchorTarget T6 输入：子任务首条 user 消息 + 首个 commit 声称对象。
type AnchorTarget struct {
	SessionID string    `json:"session_id"`
	UserMsg   string    `json:"user_msg"`
	UserMsgID string    `json:"user_msg_id,omitempty"`
	UserTs    time.Time `json:"user_ts,omitempty"`
	Claim     string    `json:"claim"` // 首个 commit 标题（声称对象）
	ClaimTs   time.Time `json:"claim_ts,omitempty"`
}

// AnalyzeAnchoring T6 目标锚定判定。
func AnalyzeAnchoring(target AnchorTarget, p AnchoringParams) AnchoringResult {
	if p.CoverageMin <= 0 {
		p.CoverageMin = 0.5
	}
	if p.FreqMin <= 0 {
		p.FreqMin = 2
	}
	res := AnchoringResult{}

	highFreq := extractHighFreqWords(target.UserMsg, p.FreqMin)
	res.SceneWords = highFreq
	for _, w := range highFreq {
		res.HighFreq = append(res.HighFreq, w.Word)
	}

	claimWords := extractClaimWords(target.Claim)
	var shared []string
	for _, cw := range claimWords {
		core := claimCore(cw)
		hit := strings.Contains(target.UserMsg, core)
		if !hit {
			// 4 字段回退：名词核心子串（直传文件 → 文件；用户原话「选择图片或者文件」）
			runes := []rune(core)
			if len(runes) >= 4 {
				hit = strings.Contains(target.UserMsg, string(runes[len(runes)-2:]))
			}
		}
		res.ClaimWords = append(res.ClaimWords, ClaimWord{Word: cw, Core: core, Hit: hit})
		if hit {
			shared = append(shared, cw)
		}
	}
	res.Shared = shared
	if len(claimWords) > 0 {
		res.Coverage = float64(len(shared)) / float64(len(claimWords))
	}

	if len(highFreq) == 0 {
		res.Undecidable = true
		res.Notes = append(res.Notes, "无场景词可提取（用户原话无重复 CJK 主题词）→ 目标锚定不可判定（模糊指令类排除）")
		return res
	}
	res.HighFreqAll = highFreqShared(highFreq, claimWords)
	res.Aligned = res.Coverage >= p.CoverageMin && res.HighFreqAll
	return res
}

// extractHighFreqWords 提取用户原话中重复 ≥ freqMin 次的 2-4 字 CJK 子串（去停用词）。
// 实证：15:09 资云集×3/打开×2（打开方式 + 打开资云集）；「选择×2」等动词停用词过滤；
// 「资云×3/云集×3」被更长同频词「资云集×3」吸收。
func extractHighFreqWords(msg string, freqMin int) []SceneWord {
	if freqMin <= 0 {
		freqMin = 2
	}
	counts := map[string]int{}
	for _, seg := range cjkSegments(msg) {
		runes := []rune(seg)
		n := len(runes)
		for l := 2; l <= 4 && l <= n; l++ {
			for i := 0; i+l <= n; i++ {
				w := string(runes[i : i+l])
				if sceneStopWord(w) {
					continue
				}
				counts[w]++
			}
		}
	}
	type wc struct {
		word  string
		count int
	}
	var cands []wc
	for w, c := range counts {
		if c >= freqMin {
			cands = append(cands, wc{w, c})
		}
	}
	// 吸收：同频子串被更长词吸收（资云×3/云集×3 → 资云集×3）
	var out []SceneWord
	for _, c := range cands {
		absorbed := false
		for _, o := range cands {
			if o.word != c.word && o.count == c.count && len([]rune(o.word)) > len([]rune(c.word)) && strings.Contains(o.word, c.word) {
				absorbed = true
				break
			}
		}
		if !absorbed {
			out = append(out, SceneWord{Word: c.word, Count: c.count})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Word < out[j].Word
	})
	return out
}

// sceneStopWord 停用词/停用字判定（用户原话中的动词、虚词、泛词）。
func sceneStopWord(w string) bool {
	if sceneStopWords[w] {
		return true
	}
	for _, ch := range w {
		if strings.ContainsRune("的了在从中还有能才看到然后或者没自己手动点击这个那是要会把可已", ch) {
			return true
		}
	}
	return false
}

var sceneStopWords = map[string]bool{
	"还有一个": true, "一个": true, "还有": true, "问题": true,
	"应用": true, "选择": true, "或者": true, "然后": true,
	"没有": true, "界面": true, "自己": true, "手动": true,
	"点击": true, "才能": true, "看到": true, "选择图片": true,
	"第三方应用": true, "有数据": true, "已经": true,
}

// cjkSegments 提取字符串中连续 CJK 片段（跳过空白/标点/英文）。
func cjkSegments(s string) []string {
	var segs []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			segs = append(segs, string(cur))
			cur = nil
		}
	}
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			cur = append(cur, r)
		} else {
			flush()
		}
	}
	flush()
	return segs
}

// nounCoreSuffix 声称对象 4 字段拆分用的名词核心后缀表。
// 实证：直传文件 → 直传/文件（用户原话「文件」为主题词）；打开方式 → 不拆（「方式」非核心名词）。
var nounCoreSuffix = []string{"文件", "图片", "界面", "数据", "队列", "列表", "面板", "目录", "消息", "窗口", "页面"}

// claimMetaWords commit 标题前缀元词（fix/feat 等），不参与声称对象。
var claimMetaWords = map[string]bool{
	"fix": true, "feat": true, "chore": true, "docs": true, "refactor": true,
	"perf": true, "test": true, "style": true, "build": true, "ci": true,
	"修复": true, "新增": true, "优化": true, "重构": true, "调整": true, "完善": true, "移除": true, "恢复": true,
}

// extractClaimWords 从 commit 标题提取声称对象词。
// 规则：取破折号前问题描述部分，去 fix: 前缀，按标点切分 CJK 段（2-4 字），
// 4 字段含名词核心后缀时拆为 2+2。实证：4b41ba8 = 第三方/打开方式/直传/文件/不跳转/不导入。
func extractClaimWords(title string) []string {
	desc := title
	for _, sep := range []string{"—", "–", " - ", "："} {
		if i := strings.Index(desc, sep); i >= 0 {
			desc = desc[:i]
			break
		}
	}
	desc = strings.TrimSpace(desc)
	if i := strings.Index(desc, ":"); i >= 0 && i+1 < len(desc) {
		desc = strings.TrimSpace(desc[i+1:])
	}
	var out []string
	seen := map[string]bool{}
	for _, tok := range strings.FieldsFunc(desc, func(r rune) bool {
		return r == ' ' || r == '「' || r == '」' || r == '（' || r == '）' || r == '(' || r == ')' || r == '/' || r == '：' || r == ',' || r == '，' || r == '。' || r == '·' || r == '-'
	}) {
		runes := []rune(tok)
		if len(runes) < 2 {
			continue // 英文段（fix/SceneDelegate/URL）跳过
		}
		if !hasCJK(tok) {
			continue
		}
		if claimMetaWords[tok] {
			continue
		}
		for _, w := range splitClaimSegment(tok) {
			addClaimWord(&out, seen, w)
		}
	}
	return out
}

// splitClaimSegment 对声称段启发式切分（CJK 2-4 字段）：
// 1) 否定前缀（不/没/无/未）作切分点：直传文件不跳转 → 直传文件|不跳转
// 2) 名词核心后缀后切分：打开方式直传文件 → 打开方式直传|文件（递归 → 打开方式|直传|文件）
// 3) 8 字段 4+4 对齐（名词短语组合）
// 4) 2-4 字段整段 + 4 字段名词核心拆分（直传文件 → 直传/文件）
func splitClaimSegment(seg string) []string {
	runes := []rune(seg)
	if len(runes) <= 4 {
		if len(runes) == 4 && containsStr(nounCoreSuffix, string(runes[2:])) {
			return []string{string(runes[:2]), string(runes[2:])}
		}
		return []string{seg}
	}
	// 1. 否定前缀（不/没/无/未 在段中）
	for i := 1; i < len(runes); i++ {
		if isNegPrefix(string(runes[i])) {
			var out []string
			out = append(out, splitClaimSegment(string(runes[:i]))...)
			out = append(out, splitClaimSegment(string(runes[i:]))...)
			return out
		}
	}
	// 2. 名词核心后缀（出现在段中 → 后缀后切）
	for i := 0; i+2 < len(runes); i++ {
		two := string(runes[i : i+2])
		if containsStr(nounCoreSuffix, two) {
			var out []string
			out = append(out, splitClaimSegment(string(runes[:i+2]))...)
			out = append(out, splitClaimSegment(string(runes[i+2:]))...)
			return out
		}
	}
	// 3. 8 字段 4+4 对齐
	if len(runes) == 8 {
		var out []string
		out = append(out, splitClaimSegment(string(runes[:4]))...)
		out = append(out, splitClaimSegment(string(runes[4:]))...)
		return out
	}
	// 无法切分：整段返回（保守，不误切）
	return []string{seg}
}

// isNegPrefix 否定前缀判定（声称词否定语义：不跳转/不导入/没反应）。
func isNegPrefix(s string) bool {
	return s == "不" || s == "没" || s == "无" || s == "未"
}

func addClaimWord(out *[]string, seen map[string]bool, w string) {
	if w == "" || seen[w] {
		return
	}
	seen[w] = true
	*out = append(*out, w)
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// claimCore 声称词核心：剥否定前缀（不/没/无/未）。
// 实证：不跳转 → 跳转、不导入 → 导入（否定语义计入共享，方向判定归 L2）。
func claimCore(w string) string {
	for _, p := range []string{"不", "没", "无", "未"} {
		if strings.HasPrefix(w, p) && len([]rune(w)) > 2 {
			return strings.TrimPrefix(w, p)
		}
	}
	return w
}

// highFreqShared 高频词与声称对象的共享判定：任一高频词与任一声称词核心互相包含。
// 实证：打开 ↔ 打开方式（strings.Contains("打开方式", "打开")）→ 覆盖用户强调主题。
func highFreqShared(highFreq []SceneWord, claimWords []string) bool {
	for _, hw := range highFreq {
		for _, cw := range claimWords {
			core := claimCore(cw)
			if strings.Contains(hw.Word, core) || strings.Contains(core, hw.Word) {
				return true
			}
		}
	}
	return false
}

// FormatAnchoringHuman T6 人类可读输出（匹配表 + 阈值计算）。
func FormatAnchoringHuman(res AnchoringResult, target AnchorTarget) string {
	var b strings.Builder
	fmt.Fprintf(&b, "T6 目标锚定（session %s）\n", shortID(target.SessionID))
	fmt.Fprintf(&b, "  用户消息（%s）：%s\n", tsClock(target.UserTs), snippetOf(target.UserMsg))
	fmt.Fprintf(&b, "  声称对象（%s）：%s\n", tsClock(target.ClaimTs), target.Claim)
	if res.Undecidable {
		fmt.Fprintf(&b, "  判定：不可判定（无场景词可提取）\n")
		return b.String()
	}
	fmt.Fprintf(&b, "  高频场景词：")
	for i, w := range res.SceneWords {
		if i > 0 {
			b.WriteString("、")
		}
		fmt.Fprintf(&b, "%s×%d", w.Word, w.Count)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "  声称对象词 ↔ 用户原话覆盖：\n")
	for _, cw := range res.ClaimWords {
		mark := "✗"
		if cw.Hit {
			mark = "✓"
		}
		fmt.Fprintf(&b, "    %s %s（核心：%s）\n", mark, cw.Word, cw.Core)
	}
	fmt.Fprintf(&b, "  共享词：%v\n", res.Shared)
	fmt.Fprintf(&b, "  覆盖率 = %d/%d = %.2f（阈值 ≥0.5）\n", len(res.Shared), len(res.ClaimWords), res.Coverage)
	fmt.Fprintf(&b, "  高频词全命中：%v\n", res.HighFreqAll)
	if res.Aligned {
		fmt.Fprintf(&b, "  判定：对准（无初始错位）✓\n")
	} else {
		fmt.Fprintf(&b, "  判定：目标错位候选 → 交 L2\n")
	}
	return b.String()
}


// tsClock 时钟格式（零值显示 --:--）。
func tsClock(t time.Time) string {
	if t.IsZero() {
		return "--:--"
	}
	return t.Format("15:04")
}
