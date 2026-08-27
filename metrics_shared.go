package main

import (
	"database/sql"
	"fmt"
)

// 共享口径 helper：`aipmc metrics` 与 `aipmc snapshot` 共用同一批 SQL，
// 防止口径分叉（8/27 指标注册表原则——同名指标不允许两套算法）。

// summaryCoverageStats 返回 B1 的 (total, withL2)：
// 分母=discussion_log 去重 session_id（排除空/unknown）；分子=其中至少有一条
// 非空 summary 的 session（JOIN discussion_log 保证分子属于分母宇宙）。
// 旧口径用 session_summaries 行数作分母会高估（ED 实测 58% vs 真实 34%）。
func summaryCoverageStats(db *sql.DB) (total, withL2 int, err error) {
	err = db.QueryRow(`SELECT
		(SELECT COUNT(DISTINCT session_id) FROM discussion_log WHERE session_id!='' AND session_id!='unknown'),
		(SELECT COUNT(DISTINCT s.session_id) FROM session_summaries s JOIN discussion_log d ON d.session_id=s.session_id WHERE s.summary!='' AND d.session_id!='' AND d.session_id!='unknown')`).Scan(&total, &withL2)
	return total, withL2, err
}

// l2NestedStats 返回 B2 双口径：nested=goal 值是嵌套 JSON；mdBlock=摘要含
// ```json 代码块（不同缺陷，分开统计）。
func l2NestedStats(db *sql.DB) (nested, mdBlock int, err error) {
	if err := db.QueryRow(`SELECT COUNT(*) FROM session_summaries WHERE summary LIKE '%"goal":"{%'`).Scan(&nested); err != nil {
		return 0, 0, err
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM session_summaries WHERE summary LIKE '%```json%'").Scan(&mdBlock); err != nil {
		return 0, 0, err
	}
	return nested, mdBlock, nil
}

// eventStats 是 D2/F1/B6 共享的事件统计。
type eventStats struct {
	total, unique int // B6 event_dup_rate 用
	free, action   int // F1 三口径：免处理 / 可行动
	actionProc     int // D2 分子：已处理可行动
	actionRate     float64
	actionDist     []string // 按类型处理分布（"type processed/total"）
}

// collectEventStats 按 D2 可行动口径（8/27 统一）聚合 events：
// 免处理参考事件（tentative_link/task_created/plan_created）生成即完成使命，
// 不计入可行动分母——低处理率≠管道堵。since==""|"all" 时全表。
func collectEventStats(db *sql.DB, since string) (*eventStats, error) {
	evWhere := ""
	var evArgs []any
	if since != "" && since != "all" {
		evWhere = " WHERE created_at >= ?"
		evArgs = []any{since}
	}
	es := &eventStats{}
	var evProcessed int
	if err := db.QueryRow("SELECT COUNT(*), COALESCE(SUM(processed_by_agent),0), COUNT(DISTINCT type || '|' || entity_type || '|' || entity_id) FROM events"+evWhere, evArgs...).Scan(&es.total, &evProcessed, &es.unique); err != nil {
		return nil, err
	}
	// 处理分布按类型聚合（F1 诊断：处理是否集中于单一类型——8/13 实测 19 个
	// 已处理全为 commit_orphan）。
	type evTypeStat struct {
		typ       string
		total     int
		processed int
	}
	var evStats []evTypeStat
	if rows, err := db.Query("SELECT type, COUNT(*), COALESCE(SUM(processed_by_agent),0) FROM events"+evWhere+" GROUP BY type ORDER BY COUNT(*) DESC", evArgs...); err == nil {
		for rows.Next() {
			var st evTypeStat
			if err := rows.Scan(&st.typ, &st.total, &st.processed); err == nil {
				evStats = append(evStats, st)
			}
		}
		rows.Close()
	}
	freeNames := map[string]bool{"tentative_link": true, "task_created": true, "plan_created": true}
	actionNames := map[string]bool{"commit_orphan": true, "mcp_error": true, "hotspot_untracked": true}
	for _, st := range evStats {
		switch {
		case freeNames[st.typ]:
			es.free += st.total
		case actionNames[st.typ]:
			es.action += st.total
			es.actionProc += st.processed
			es.actionDist = append(es.actionDist, fmt.Sprintf("%s %d/%d", st.typ, st.processed, st.total))
		}
	}
	if es.action > 0 {
		es.actionRate = float64(es.actionProc) / float64(es.action)
	}
	return es, nil
}
