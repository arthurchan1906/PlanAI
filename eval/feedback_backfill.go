package eval

// C2 产出物 3（8/27）：反馈回填——shadow JSONL → session_summaries.entity_refs。
// B 线审核三条前置要求（Claude 13:37）：
//   ② 强漏查按实体类型分权重——decision/plan/bug 高价值进反馈，commit/task 等降级
//     为计数不告警（commit 引用多来自 git log，无需查 AIPM）；
//   ③ 数据源规范性信号（94.1% 无来源词）无区分度，不进反馈通道，仅保留计数（M 线域）。
// 回填语义：只写「存在高价值强漏查」的 session；entity_refs 追加为对象数组
// {type,id,ref_text,missing_queries}（与 L2 的 []string 形态区分），quality_score
// 保留既有值（工作流分数语义：高=好，不能被反馈覆盖污染 metrics）。

import (
	"encoding/json"
	"os"
	"strings"

	"aipmc/store"
)

// highValueTypes 强漏查高价值类型（进反馈通道）。8/27 Claude 审核收敛：
// decision/plan/bug 引用未查询是「下结论但没查依据」的高价值信号。
var highValueTypes = map[string]bool{"decision": true, "plan": true, "bug": true}

// FeedbackBackfillRef session_summaries.entity_refs 中的反馈条目（对象形态，
// 与 L2 写入的 []string 形态区分——briefing 消费按形态过滤）。
type FeedbackBackfillRef struct {
	Type           string   `json:"type"`
	ID             string   `json:"id"`
	RefText        string   `json:"ref_text"`
	MissingQueries []string `json:"missing_queries,omitempty"`
}

// BackfillFeedback 读取 shadow JSONL（aipmc eval feedback --shadow 产物），
// 把高价值强漏查 session 回填到 session_summaries。
// 返回 (回填 session 数, 回填 ref 数, error)。幂等：同 session 重复回填按 id 去重合并。
func BackfillFeedback(shadowPath string) (int, int, error) {
	b, err := os.ReadFile(shadowPath)
	if err != nil {
		return 0, 0, err
	}
	sessions, refs := 0, 0
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e FeedbackShadowEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		n, err := backfillOne(e)
		if err != nil {
			continue // 单 session 失败不阻塞整批（fail-open，与检测器一致）
		}
		if n > 0 {
			sessions++
			refs += n
		}
	}
	return sessions, refs, nil
}

// backfillOne 回填单个 shadow 条目：过滤高价值 ref → 合并 entity_refs → upsert。
// 返回写入的 ref 数（0 = 该 session 无高价值强漏查，跳过）。
func backfillOne(e FeedbackShadowEntry) (int, error) {
	if len(e.MissingQueries) == 0 {
		return 0, nil // 无强漏查不进反馈通道
	}
	// 高价值类型过滤（②）：decision/plan/bug 才进反馈。
	var refs []FeedbackBackfillRef
	for _, r := range e.EntityRefs {
		if !highValueTypes[r.Type] {
			continue
		}
		refs = append(refs, FeedbackBackfillRef{
			Type:           r.Type,
			ID:             r.ID,
			RefText:        r.RefText,
			MissingQueries: typeMissingQueries(r.Type, e.MissingQueries),
		})
	}
	if len(refs) == 0 {
		return 0, nil
	}

	// 合并已有 entity_refs（L2 []string 或既往反馈对象），按 id 去重。
	existing, err := store.GetSessionSummary(e.SessionID)
	if err != nil {
		return 0, err
	}
	existingRefs, seen := parseExistingRefs(existing, e.SessionID)
	added := 0
	for _, r := range refs {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		existingRefs = append(existingRefs, r)
		added++
	}
	if added == 0 {
		return 0, nil
	}
	merged, _ := json.Marshal(existingRefs)

	quality := 0
	source := e.Agent
	if existing != nil {
		quality = existing.QualityScore
		if source == "" {
			source = existing.Source
		}
	}
	if err := store.UpsertSessionSummary(store.SessionSummary{
		SessionID:    e.SessionID,
		Source:       source,
		EntityRefs:   string(merged),
		QualityScore: quality, // 保留既有工作流分数，反馈不覆盖（防 metrics 语义污染）
	}); err != nil {
		return 0, err
	}
	return added, nil
}

// typeMissingQueries 返回该实体类型对应的、本次强漏查中的期望查询工具
// （briefing 可提示「查这些工具补依据」）。类型无映射时返回空。
func typeMissingQueries(typ string, all []string) []string {
	tools := entityQueryTools[typ]
	if len(tools) == 0 {
		return nil
	}
	want := make([]string, 0, len(tools))
	for _, t := range tools {
		for _, a := range all {
			if t == a {
				want = append(want, t)
				break
			}
		}
	}
	return want
}

// parseExistingRefs 解析既有 entity_refs（兼容 L2 []string 与既往反馈对象），
// 返回合并后的 ref 切片 + 已见 id 集合（幂等去重）。
func parseExistingRefs(existing *store.SessionSummary, sid string) ([]FeedbackBackfillRef, map[string]bool) {
	seen := map[string]bool{}
	if existing == nil || strings.TrimSpace(existing.EntityRefs) == "" {
		return []FeedbackBackfillRef{}, seen
	}
	raw := []byte(existing.EntityRefs)
	// L2 形态：[]string（实体 ID）
	var strRefs []string
	if err := json.Unmarshal(raw, &strRefs); err == nil {
		var out []FeedbackBackfillRef
		for _, id := range strRefs {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, FeedbackBackfillRef{ID: id})
		}
		return out, seen
	}
	// 反馈形态：[]FeedbackBackfillRef（对象）
	var objRefs []FeedbackBackfillRef
	if err := json.Unmarshal(raw, &objRefs); err == nil {
		var out []FeedbackBackfillRef
		for _, r := range objRefs {
			if r.ID == "" || seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			out = append(out, r)
		}
		return out, seen
	}
	return []FeedbackBackfillRef{}, seen
}
