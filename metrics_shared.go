package main

import (
	"database/sql"
	"fmt"
	"time"
)

// eventFreshness 是「可行动事件」的状态代理窗口：mcp_error / hotspot_untracked
// 仅在近 eventFreshness 内发生才视为「当前仍待处理」。9/9 Claude 复核纠正：D2 曾把
// 7/30-8/31 的历史事件长期占分母（mcp_error 71 条中近 7 天仅 4 条），使 D2 失真。
const eventFreshness = 7 * 24 * time.Hour

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
		if freeNames[st.typ] {
			es.free += st.total
		}
	}
	// 可行动事件必须是「当前仍待处理」的：D2 曾把所有历史事件计入分母，导致
	// commit_orphan 在 commit 早已绑定 task 后仍占位（9/9 实测 227/228 为 stale），
	// 使 D2 处理率失真。此处按事件当前状态过滤后再聚合（MVP：commit_orphan 状态感知）。
	type evAcc struct{ total, processed int }
	actionAcc := map[string]evAcc{}
	now := time.Now()
	if rows, err := db.Query("SELECT type, entity_type, entity_id, processed_by_agent, created_at FROM events"+evWhere, evArgs...); err == nil {
		for rows.Next() {
			var typ, etype, eid, createdAt string
			var processed int
			if err := rows.Scan(&typ, &etype, &eid, &processed, &createdAt); err == nil && actionNames[typ] {
				if !eventStillActionable(db, typ, etype, eid, createdAt, now) {
					continue // 事件所指问题已解决 → 不再纳入可行动
				}
				a := actionAcc[typ]
				a.total++
				if processed > 0 {
					a.processed++
				}
				actionAcc[typ] = a
			}
		}
		rows.Close()
	}
	// 按总量降序输出处理分布（保持诊断可读性）。
	actionOrder := []string{"commit_orphan", "mcp_error", "hotspot_untracked"}
	for i := 1; i < len(actionOrder); i++ {
		for j := i; j > 0 && actionAcc[actionOrder[j]].total > actionAcc[actionOrder[j-1]].total; j-- {
			actionOrder[j], actionOrder[j-1] = actionOrder[j-1], actionOrder[j]
		}
	}
	for _, typ := range actionOrder {
		a := actionAcc[typ]
		if a.total == 0 {
			continue
		}
		es.action += a.total
		es.actionProc += a.processed
		es.actionDist = append(es.actionDist, fmt.Sprintf("%s %d/%d", typ, a.processed, a.total))
	}
	if es.action > 0 {
		es.actionRate = float64(es.actionProc) / float64(es.action)
	}
	return es, nil
}

// eventStillActionable 判断一个「可行动」事件是否仍然代表未解决的当前状态。
// 9/9 根因：D2 曾按事件类型全量计数，使得已解决的历史事件长期占分母
// （commit_orphan 227/228 stale、mcp_error 近 7 天仅 4/71），观测量失真。
// 只有「当前仍待处理」的才计入：commit_orphan 用真状态；mcp_error/hotspot 用新鲜度代理。
func eventStillActionable(db *sql.DB, typ, entityType, entityID, createdAt string, now time.Time) bool {
	switch typ {
	case "commit_orphan":
		if entityType != "commit" {
			return false
		}
		var taskID sql.NullString
		// commit 已被删除 → 视为已解决；task_id 仍为空 → 仍是孤儿，可行动。
		if err := db.QueryRow("SELECT task_id FROM commits WHERE id = ?", entityID).Scan(&taskID); err != nil {
			return false
		}
		return taskID.String == ""
	case "mcp_error", "hotspot_untracked":
		t, err := time.ParseInLocation("2006-01-02T15:04:05", createdAt, time.Local)
		if err != nil {
			return false // 无法解析时间 → 保守视为 stale，不计入
		}
		return now.Sub(t) <= eventFreshness
	default:
		return true
	}
}
