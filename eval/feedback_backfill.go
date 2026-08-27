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

// FeedbackBackfillRef session_summaries.entity_refs 中的对象形态条目。
//
// entity_refs 两层语义（Claude 8/27 审核标注，防未来消费端踩坑）：
//  1. 引用记录：L2 历史 []string 与规范化后的 {type,id} 对象（ref_text 可为
//     空、无 missing_queries）——表示 session 引用过该实体，计数语义；
//  2. 反馈信号：missing_queries 非空的对象（决策/计划/bug 强漏查回填写入）
//     ——表示「引用但未查询」的反馈。消费方区分用 len(missing_queries)>0
//     （ref_text 非空但无 mq 的高价值对象为引用记录，如 bug 标题上下文）。
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
	existingRefs, _, changed := parseExistingRefs(existing, e.SessionID)
	idx := make(map[string]int, len(existingRefs))
	for i, r := range existingRefs {
		idx[r.ID] = i
	}
	added := 0
	for _, r := range refs {
		if i, ok := idx[r.ID]; ok {
			// 已存在但 shadow 携带更多反馈信息（mq/ref_text）→ 补全替换
			// （如上次写坏成空壳、或 L2 无 mq 的引用被检测器补全）。
			if richer(r, existingRefs[i]) {
				existingRefs[i] = r
				changed = true
				added++
			}
			continue
		}
		idx[r.ID] = len(existingRefs)
		existingRefs = append(existingRefs, r)
		added++
	}
	// 无新增但存量形态需要修复（L2 双前缀/脏对象规范化）也写回——
	// 写库端保持干净，消费端不再兜底（Claude 8/27 审核 Challenge 1/2）。
	if added == 0 && !changed {
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
// 返回合并用的 ref 切片 + 已见 id 集合（幂等去重）+ 是否发生规范化变更。
// 规范化：L2 双前缀 id（"task:task-xxx"）拆成 {Type:"task", ID:"task-xxx"}，
// 对象形态 type 空但 id 带前缀的脏对象同样拆补——统一 id 去前缀形态，
// 与回填写入的反馈对象一致（Claude 8/27 审核 Challenge 1/2：写库端不产生
// type:"" 脏对象、id 双形态不去重失效）。
func parseExistingRefs(existing *store.SessionSummary, sid string) ([]FeedbackBackfillRef, map[string]bool, bool) {
	seen := map[string]bool{}
	if existing == nil || strings.TrimSpace(existing.EntityRefs) == "" {
		return []FeedbackBackfillRef{}, seen, false
	}
	raw := []byte(existing.EntityRefs)
	// L2 形态：[]string（实体 ID）
	var strRefs []string
	if err := json.Unmarshal(raw, &strRefs); err == nil {
		var out []FeedbackBackfillRef
		for _, id := range strRefs {
			if id == "" {
				continue
			}
			r := normalizeRef(FeedbackBackfillRef{ID: id})
			if r.ID == "" || seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			out = append(out, r)
		}
		return out, seen, true // L2 → 对象形态转换本身即规范化变更
	}
	// 反馈形态：[]FeedbackBackfillRef（对象）
	var objRefs []FeedbackBackfillRef
	if err := json.Unmarshal(raw, &objRefs); err == nil {
		var out []FeedbackBackfillRef
		idx := map[string]int{} // 规范化 id → out 下标
		changed := false
		for _, r := range objRefs {
			norm := normalizeRef(r)
			if norm.Type != r.Type || norm.ID != r.ID {
				changed = true
			}
			if norm.ID == "" {
				continue
			}
			if i, ok := idx[norm.ID]; ok {
				// id 冲突（L2 拆前缀空壳 vs feedback 对象）：信息更全者胜，
				// feedback（ref_text/missing_queries）优先于空壳——否则
				// 规范化会把已回填的高价值反馈对象踢掉（8/27 实测数据丢失）。
				if richer(norm, out[i]) {
					out[i] = norm
					changed = true
				}
				continue
			}
			idx[norm.ID] = len(out)
			out = append(out, norm)
		}
		for i := range out {
			seen[out[i].ID] = true
		}
		return out, seen, changed
	}
	return []FeedbackBackfillRef{}, seen, false
}

// richer 报告 a 是否比 b 携带更多反馈信息（有 ref_text 或 missing_queries 者胜）。
func richer(a, b FeedbackBackfillRef) bool {
	aInfo := a.RefText != "" || len(a.MissingQueries) > 0
	bInfo := b.RefText != "" || len(b.MissingQueries) > 0
	return aInfo && !bInfo
}

// normalizeRef 把 type 空但 id 带 "prefix:" 前缀的 ref 拆成完整形态。
// L2/脏对象 id 形如 "task:task-20260615-..."，拆第一个冒号后与回填反馈对象
// （Type="task", ID="task-20260615-..."）形态一致。
func normalizeRef(r FeedbackBackfillRef) FeedbackBackfillRef {
	if r.Type != "" {
		return r
	}
	if i := strings.Index(r.ID, ":"); i > 0 {
		return FeedbackBackfillRef{Type: r.ID[:i], ID: r.ID[i+1:], RefText: r.RefText}
	}
	return r
}
