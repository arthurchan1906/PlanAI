// grounding.go — 行为链条 grounding 检测原型（EVAL 描述层）。
// ⚠️ 已归档（2026-08-24，eval/archive/）——理由（规格实证证伪 + Claude 第四轮审核）：
//   1. SPEC §0「为什么转向」硬证据 2：c0ad2534（788 条记录）35 条断言全部有取证（0 沙土），
//      但 22 小时查错方向——真实盲猜形态是「取证方向错误」而非「无取证断言」，
//      断言-证据配对（Grounded/Sandy 单维度）抓不住该形态 → 检测点被规格证伪。
//   2. 原实现 Grounded = len(Evidence) > 0（窗口内任一工具调用即 grounded）无关联性判定，
//      正常会话窗口内几乎总有工具行为 → Grounded 恒真，无判别力（Claude 8/24 第四轮）。
//   3. 从未提交、无测试（原型状态），不应躺在生产目录。
//   4. 「取证方向错误」的检测由 T5 死循环 + T8 对准/持续近似覆盖（回归响应性维度）。
//   替代路径（如 P1 重启）：需补「证据-断言主题关联性」判定（L2 语义）+ 测试 + 接入 T9，
//   且须先回应 §0 硬证据 2 的证伪（为何这次能抓到「取证方向错误」形态）。
// 代码保留供参考，不参与构建语义（无引用）。
// 恢复/运行说明：本文件引用父包 eval 的未导出符号（Turn/Record/isLLMText），
// 需移回 eval/ 目录（原位置）才会参与编译；eval/archive/ 仅作冻结存档。


//go:build archive

// +build archive

package eval

// grounding.go — 行为链条 grounding 检测原型（已归档，仅 -tags archive 时编译）（EVAL 描述层）。
// 目的：检测 agent 断言（claim）是否有前序取证行为支撑，标记"猜测冒充事实"
// （逻辑链建立在沙土上的环节）。纯代码、确定性，供标注集验证。

import (
	"regexp"
	"strings"
	"time"
)

// Claim 一条断言节点。
type Claim struct {
	Text     string    // 断言原文（截断）
	Time     time.Time
	Guessing bool      // 含猜测词（可能/估计/应该是/猜测/大概）
	Evidence []string  // 前序取证行为（code/log/git/aipm/other）
	Grounded bool      // 有取证行为支撑
	Sandy    bool      // 沙土节点：无证据且含猜测词
}

var (
	// 断言触发词：agent 陈述原因/结论
	claimRe = regexp.MustCompile(`(根因|原因(?:是|在于|可能是|就是)?|问题(?:在|出在|是|可能|应该|就|不是)?|是因为|导致了|由.*引起)`)
	// 猜测词：无证据的推测性表述
	guessRe = regexp.MustCompile(`(可能是|估计|猜测|大概是|应该就是|也许|说不定|推断)`)
	evidenceCmdRe = []struct {
		re   *regexp.Regexp
		kind string
	}{
		{regexp.MustCompile(`(?i)(grep|rg|sed|cat|less|head|tail).*(log|\.go|\.swift|\.h|\.m|\.ts|\.js)`), "code"},
		{regexp.MustCompile(`(?i)(grep|rg|tail|cat|less).*(log|\.log|dmesg|journalctl)`), "log"},
		{regexp.MustCompile(`(?i)git (log|blame|show|diff|status)`), "git"},
		{regexp.MustCompile(`(?i)(sqlite3|SELECT )`), "query"},
	}
)

// ExtractClaims 从回合序列提取断言节点（assistant LLM 文本，排除工具行）。
func ExtractClaims(turns []Turn) []Claim {
	var claims []Claim
	for _, t := range turns {
		for _, r := range t.Records {
			if r.Role != "assistant" || !isLLMText(r.Content, r.Tool.Tool) {
				continue
			}
			c := strings.TrimSpace(r.Content)
			if len([]rune(c)) < 12 || !claimRe.MatchString(c) {
				continue
			}
			claims = append(claims, Claim{
				Text:     truncate(c, 120),
				Time:     r.CreatedAt,
				Guessing: guessRe.MatchString(c),
			})
		}
	}
	return claims
}

// ScanEvidence 为每条断言回溯前序调查窗口（maxBack 条工具记录），判定 grounding。
func ScanEvidence(turns []Turn, claims []Claim, maxBack int) []Claim {
	if maxBack <= 0 {
		maxBack = 15
	}
	// 展平为带索引的记录流
	type flatRec struct {
		rec   Record
		index int
	}
	var flat []flatRec
	for _, t := range turns {
		for i := range t.Records {
			flat = append(flat, flatRec{t.Records[i], len(flat)})
		}
	}
	for i := range claims {
		// 找到断言在记录流中的位置
		pos := -1
		for j := len(flat) - 1; j >= 0; j-- {
			if !flat[j].rec.CreatedAt.After(claims[i].Time) {
				pos = flat[j].index
				break
			}
		}
		if pos < 0 {
			continue
		}
		evSet := map[string]bool{}
		for k := pos - 1; k >= 0 && k >= pos-maxBack; k-- { // 跳过断言记录本身
			r := flat[k].rec
			switch r.Tool.Tool {
			case "bash", "edit", "read", "write":
				evSet["tool"] = true
			case "mcp":
				evSet["aipm"] = true
			default:
				if strings.HasPrefix(r.Tool.Tool, "mcp_aipm_") {
					evSet["aipm"] = true
				}
			}
			for _, ec := range evidenceCmdRe {
				if ec.re.MatchString(r.Tool.Command) {
					evSet[ec.kind] = true
				}
			}
		}
		claims[i].Evidence = keysOf(evSet)
		claims[i].Grounded = len(claims[i].Evidence) > 0
		claims[i].Sandy = !claims[i].Grounded && claims[i].Guessing
	}
	return claims
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
