package analyze

import (
	"fmt"
	"sort"
	"strings"

	"aipmc/store"
	"aipmc/u"
)

// ── P0 ④a：agent_briefing 上下文卡 MVP（8/31）─────────────────────
// 目标：给 agent 一张「当前任务 × 文件 × 决策」的确定性上下文卡，让它在
// 开工前就能看到 PM 状态，而不是靠语义「相关」去猜。
//
// 设计（Claude 8/28 17:01 从 AIPM 数据地基背书的可行性）：
//   - 零语义：全部是确定性关联（不走 L2 语义/理解层，D1 核心纪律）。
//   - task×file：复用 resolveFileContext 的 graph_edges(file_touch) →
//     commit → task 映射，文件→task 字典确定性排序。
//   - task×decision：tasks.related_decisions_json（JSON 数组）→
//     get_decision 取 title/status/date。
//   - task×bug：bugs.task_id 直连（P0④b，去 bugs→commits→task 两跳）。
//   - task×verification：verification_log.task_id 直连（P0④b 验证台账）。

// 上下文卡输出上限（防 token 膨胀，MVP 从紧）。
const (
	agentBriefingTasksCap            = 3 // 进行中任务上限（与 anchor cap 同量级）
	agentBriefingFilesPerTask        = 6 // 每任务文件上限
	agentBriefingDecisionsPerTask    = 5 // 每任务决策上限
	agentBriefingBugsPerTask         = 4 // 每任务「相关 bug」上限（task_id 直连）
	agentBriefingVerificationsPerTask = 3 // 每任务「验证台账」上限
)

// BuildAgentBriefing 生成确定性 agent_briefing 上下文卡 Markdown。
// 返回空串表示无进行中任务（调用方直接跳过，不占注入预算）。
func BuildAgentBriefing() string {
	tasks, err := store.ListTasks("in_progress", "")
	if err != nil {
		u.LogShared("BRIEFING", "agent_briefing list_tasks_err=%v", err)
		return ""
	}
	if len(tasks) == 0 {
		u.LogShared("BRIEFING", "agent_briefing tasks=0")
		return ""
	}
	if len(tasks) > agentBriefingTasksCap {
		tasks = tasks[:agentBriefingTasksCap]
	}

	// task×file：store.ListTaskFileAssoc 按 task 状态直连，不受
	// ListGraphEdges 的 LIMIT 200 影响（P0 ④a 需全部当前任务关联）。
	taskFiles := store.ListTaskFileAssoc("in_progress")

	var b strings.Builder
	b.WriteString("📇 上下文卡 — 当前任务 × 文件 × 决策（确定性关联）\n\n")
	b.WriteString("## 进行中任务\n\n")
	for _, t := range tasks {
		b.WriteString(fmt.Sprintf("**%s** (%s, %s) 《%s》\n", t.ID, t.Priority, t.Status, t.Title))

		files := taskFiles[t.ID]
		if len(files) > 0 {
			if len(files) > agentBriefingFilesPerTask {
				files = files[:agentBriefingFilesPerTask]
			}
			b.WriteString("  📄 文件: " + strings.Join(files, ", ") + "\n")
		} else {
			// 无文件关联：明确标注而非静默缺省（P0 ④a 复核建议，Claude 8/31）。
			b.WriteString("  📄 文件: (无文件关联)\n")
		}

		if decs := formatTaskDecisions(t.RelatedDecisions); len(decs) > 0 {
			b.WriteString("  📌 决策: " + strings.Join(decs, "; ") + "\n")
		}
		if bugs := formatTaskBugs(t.ID); len(bugs) > 0 {
			b.WriteString("  🐞 Bug: " + strings.Join(bugs, "; ") + "\n")
		}
		if vlogs := formatTaskVerifications(t.ID); len(vlogs) > 0 {
			b.WriteString("  ✅ 验证: " + strings.Join(vlogs, "; ") + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// formatTaskDecisions 将 task.related_decisions 的决策 ID 解析为可读条目
// 「decision-id (status) 《title》」。确定性：按 ID 排序；get_decision 失败记为
// 「decision-id (unknown)」而非丢弃——保持「该任务有决策」可见。上限 5。
func formatTaskDecisions(ids []any) []string {
	if len(ids) == 0 {
		return nil
	}
	norm := make([]string, 0, len(ids))
	for _, id := range ids {
		s := u.Str(id)
		if s != "" {
			norm = append(norm, s)
		}
	}
	sort.Strings(norm)
	if len(norm) > agentBriefingDecisionsPerTask {
		norm = norm[:agentBriefingDecisionsPerTask]
	}
	out := make([]string, 0, len(norm))
	for _, id := range norm {
		d, err := store.GetDecision(id)
		if err != nil || d == nil {
			out = append(out, fmt.Sprintf("%s (unknown)", id))
			continue
		}
		status, _ := d["status"].(string)
		title, _ := d["title"].(string)
		tag := id
		if status != "" {
			tag += " (" + status + ")"
		}
		if title != "" {
			tag += " 《" + title + "》"
		}
		out = append(out, tag)
	}
	return out
}


// formatTaskBugs 取该 task_id 直连的 bug（P0④b，去两跳），格式化为
// 「bug-id (severity, status) 《title》」。确定性：按 id 排序，上限 4。
func formatTaskBugs(taskID string) []string {
	bugs, err := store.ListBugsByTask(taskID, 0)
	if err != nil || len(bugs) == 0 {
		return nil
	}
	sort.Slice(bugs, func(i, j int) bool {
		return u.Str(bugs[i]["id"]) < u.Str(bugs[j]["id"])
	})
	if len(bugs) > agentBriefingBugsPerTask {
		bugs = bugs[:agentBriefingBugsPerTask]
	}
	out := make([]string, 0, len(bugs))
	for _, b := range bugs {
		id := u.Str(b["id"])
		severity, _ := b["severity"].(string)
		status, _ := b["status"].(string)
		title, _ := b["title"].(string)
		tag := id
		if severity != "" {
			tag += " (" + severity + ", " + status + ")"
		}
		if title != "" {
			tag += " 《" + title + "》"
		}
		out = append(out, tag)
	}
	return out
}

// formatTaskVerifications 取该 task_id 关联的验证台账（P0④b），格式化为
// 「scene/device/ksn → result」。确定性：按 id 排序，上限 3。
func formatTaskVerifications(taskID string) []string {
	if taskID == "" {
		return nil
	}
	logs, err := store.ListVerificationLogs("", taskID, 0)
	if err != nil || len(logs) == 0 {
		return nil
	}
	sort.Slice(logs, func(i, j int) bool {
		return u.Str(logs[i]["id"]) < u.Str(logs[j]["id"])
	})
	if len(logs) > agentBriefingVerificationsPerTask {
		logs = logs[:agentBriefingVerificationsPerTask]
	}
	out := make([]string, 0, len(logs))
	for _, l := range logs {
		scene, _ := l["scene"].(string)
		device, _ := l["device"].(string)
		ksn, _ := l["ksn"].(string)
		result, _ := l["result"].(string)
		key := scene
		if device != "" {
			key += "/" + device
		}
		if ksn != "" {
			key += "/" + ksn
		}
		if result != "" {
			key += " → " + result
		}
		out = append(out, key)
	}
	return out
}
