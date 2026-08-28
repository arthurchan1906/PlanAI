package store

// 机制 1（v13.1 P1，8/28 claude 实现）：read_discussions 附带相关实体。
//
// 目标：agent 读到讨论时自动看到被引用的实体状态（如 decision-X accepted、
// task-Y done），无需再手动 search——治疗「read 代替 search」的低效模式
// （8/20 实测 read:search = 8:1）。
//
// 设计约束（v13.1 钉 1 + 8/27 十轮收敛）：
//   - 纯正则提取（复用 B 线 entityIDRe 模式），零语义、零 L2 依赖
//   - 只提取「引用」的实体并附带其当前标题+状态——引用即上下文，不扩大检索
//   - 输出附在 RelatedContext（结构化），不侵入正文（正文 token 预算敏感）
//   - 验证协议：30 条双标 precision≥90%（待样本）

import (
	"database/sql"
	"errors"
	"regexp"
	"strings"

	pmdb "aipmc/db"
	"aipmc/u"
)

// entityRefRe 匹配 AIPM 实体 ID：<type>-YYYYMMDD-HHMMSS-xxxxxx（容忍大小写）。
// 与 eval/feedback_detector.go 的 entityIDRe 同构（各自独立，避免跨包耦合）。
var entityRefRe = regexp.MustCompile(`(?i)\b(decision|task|bug|commit|plan|thread|idea)-\d{8}-\d{6}-[0-9a-f]{6}\b`)

// RelatedEntity 一个被讨论引用的实体的摘要信息。
type RelatedEntity struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// ExtractRelatedEntities 从讨论内容中提取引用的实体 ID（去重，保持出现顺序）。
// 纯正则，零语义——不判断引用意图（正引用/反引用/转述），只标记「被提及」。
func ExtractRelatedEntities(contents []string) []string {
	seen := map[string]bool{}
	var ids []string
	for _, c := range contents {
		for _, m := range entityRefRe.FindAllString(c, -1) {
			key := strings.ToLower(m)
			if seen[key] {
				continue
			}
			seen[key] = true
			ids = append(ids, m)
		}
	}
	return ids
}

// FetchRelatedEntities 查询实体 ID 列表的标题+状态。
// 按类型路由到对应表；找不到的实体静默跳过（引用可能已删除/过期）。
// 返回顺序与输入 ids 一致（去重后的）。
// 8/28 codex 审核 Ch1：查询统一走 fetchEntityTitle（BUSY 重试，D 线决策 B）；
// 查询失败记录 warn 日志（零告警原则），实体不存在（title 空）属正常跳过。
// 8/28 Claude 补充观察：返回 error 供调用方区分「无引用」与「查询失败」
// （related_entities_status 字段，M 线测量机制 1 有效性的前提）。
func FetchRelatedEntities(db *sql.DB, ids []string) ([]RelatedEntity, error) {
	var out []RelatedEntity
	var qerr error
	for _, id := range ids {
		lower := strings.ToLower(id)
		typ := entityType(lower)
		if typ == "" {
			continue
		}
		table := entityTable(typ)
		if table == "" {
			continue
		}
		title, status, err := fetchEntityTitle(db, table, lower)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // 实体不存在（已删除/过期）→ 正常跳过，不记日志
			}
			u.LogShared("MCP", "related_entity query failed type=%s id=%s err=%v", typ, lower, err)
			if qerr == nil {
				qerr = err
			}
			continue
		}
		if title == "" {
			continue // 不存在/查询失败 → 跳过
		}
		out = append(out, RelatedEntity{Type: typ, ID: lower, Title: title, Status: status})
	}
	return out, qerr
}

func entityType(id string) string {
	for _, t := range []string{"decision", "task", "bug", "plan", "thread", "idea", "commit"} {
		if strings.HasPrefix(id, t+"-") {
			return t
		}
	}
	return ""
}

// entityTable 实体类型 → 表名（包内常量映射，无外部输入，无注入面）。
func entityTable(typ string) string {
	switch typ {
	case "decision":
		return "decisions"
	case "task":
		return "tasks"
	case "bug":
		return "bugs"
	case "plan":
		return "plans"
	case "thread":
		return "threads"
	case "idea":
		return "ideas"
	case "commit":
		return "commits"
	}
	return ""
}

// fetchEntityTitle 查询实体标题+状态，带 BUSY 重试（D 线决策 B：读路径
// retry 全覆盖，decision-20260827-144909-c3d06b）。QueryRow 的延迟错误由
// Scan 返回，RetryBusy 对 SQLITE_BUSY 做 3 次指数退避（100/200/400ms）。
// 8/28 codex 审核 Ch1：原实现为裸 QueryRow，read_discussions 热路径在并发
// 写下会随机命中 BUSY 被静默跳过，机制 1 功能间歇失效且无观测。
func fetchEntityTitle(db *sql.DB, table, id string) (string, string, error) {
	var title, status string
	err := pmdb.RetryBusy(func() error {
		return db.QueryRow("SELECT title, status FROM "+table+" WHERE id = ?", id).Scan(&title, &status)
	})
	return title, status, err
}

// RelatedEntitiesFromRows 从 discussion 行提取并查询相关实体。
// rows 是 ReadDiscussions 的返回（map 含 "content" 键）。
func RelatedEntitiesFromRows(db *sql.DB, rows []map[string]any) ([]RelatedEntity, error) {
	contents := make([]string, 0, len(rows))
	for _, r := range rows {
		if c, ok := r["content"].(string); ok {
			contents = append(contents, c)
		}
	}
	ids := ExtractRelatedEntities(contents)
	return FetchRelatedEntities(db, ids)
}
