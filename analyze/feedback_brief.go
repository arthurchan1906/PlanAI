package analyze

// C2 产出物 3（8/27）：briefing 消费「引用未查询」反馈（约束 A 形态：只陈述
// 行为事实——"session 引用了 decision-X 但未调用查询工具"，不注入判定标签）。
// 数据源 = session_summaries.entity_refs 中的反馈对象（{type,id,ref_text,
// missing_queries}，B 线回填写入；与 L2 的 []string 形态区分）。

import (
	"encoding/json"
	"strings"
	"time"

	"aipmc/store"
)

// feedbackHighValueTypes briefing 反馈段只展示的高价值类型（与回填过滤一致，
// B 线审核要求②：commit/task 降级为计数不告警）。
var feedbackHighValueTypes = map[string]bool{"decision": true, "plan": true, "bug": true}

// feedbackBriefWindow 反馈展示窗口（最近 48h 活跃的 session 回填）。
const feedbackBriefWindow = "48h"

// feedbackBriefCap summary 级展示条数上限。
const feedbackBriefCap = 3

// feedbackSessionBrief 一个 session 的反馈摘要。
type feedbackSessionBrief struct {
	SessionID string
	Source    string
	Refs      []map[string]any // {type,id,ref_text,missing_queries}
}

// collectFeedbackBriefs 读取最近窗口内带反馈 entity_refs 的 session。
func collectFeedbackBriefs() []feedbackSessionBrief {
	rows, err := store.ListSessionSummariesSince(since(48*time.Hour), 100)
	if err != nil {
		return nil
	}
	var out []feedbackSessionBrief
	for _, r := range rows {
		refs := parseFeedbackRefs(r.EntityRefs)
		if len(refs) == 0 {
			continue
		}
		out = append(out, feedbackSessionBrief{SessionID: r.SessionID, Source: r.Source, Refs: refs})
	}
	return out
}

// parseFeedbackRefs 解析 entity_refs 中对象形态的反馈条目（L2 []string 形态忽略）。
func parseFeedbackRefs(entityRefsJSON string) []map[string]any {
	if strings.TrimSpace(entityRefsJSON) == "" || !strings.HasPrefix(strings.TrimSpace(entityRefsJSON), "[") {
		return nil
	}
	raw := []byte(entityRefsJSON)
	// 反馈形态：[]map 对象（含 type/id/ref_text）
	var objs []map[string]any
	if err := json.Unmarshal(raw, &objs); err != nil {
		return nil
	}
	var out []map[string]any
	for _, o := range objs {
		typ, _ := o["type"].(string)
		id, _ := o["id"].(string)
		refText, _ := o["ref_text"].(string)
		mqs, _ := o["missing_queries"].([]any)
		// 只收反馈对象：高价值类型 + 携带反馈特征（ref_text/missing_queries）。
		// 规范化后的 L2 引用（type 非空但 ref_text/mq 为空）只是"引用"计数，
		// 不是"引用未查询"信号——不展示（Claude 8/27 审核要求②）。
		if id != "" && feedbackHighValueTypes[typ] && (refText != "" || len(mqs) > 0) {
			out = append(out, o)
		}
	}
	return out
}

// buildFeedbackBriefSection 生成 briefing 反馈段（compact=summary 级截断）。
func buildFeedbackBriefSection(compact bool) string {
	bs := collectFeedbackBriefs()
	if len(bs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("### 🔍 引用未查询反馈（" + Itoa(len(bs)) + " 个 session）\n")
	shown := 0
	for _, s := range bs {
		if compact && shown >= feedbackBriefCap {
			b.WriteString("  … 共 " + Itoa(len(bs)) + " 个 session，已省略\n")
			break
		}
		short := sessionIDPrefix(s.SessionID)
		b.WriteString("- session `" + short + "`（" + s.Source + "）: 引用了 ")
		names := make([]string, 0, len(s.Refs))
		for _, r := range s.Refs {
			typ, _ := r["type"].(string)
			id, _ := r["id"].(string)
			if typ != "" {
				names = append(names, typ+":"+id)
			} else {
				names = append(names, id)
			}
		}
		b.WriteString(strings.Join(names, " / "))
		if !compact {
			b.WriteString("\n  → 建议: 引用实体前先查询确认（decision/plan/bug 内容会改变结论）")
		}
		b.WriteString("\n")
		shown++
	}
	if !compact {
		b.WriteString("  → 这些是行为事实（引用但未查询），不是判定——查询后结论若有变，请更新引用\n")
	}
	b.WriteString("\n")
	return b.String()
}
