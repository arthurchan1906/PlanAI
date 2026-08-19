package eval

// EVAL_PIPELINE §3.2 阶段 1：回合化（turn）。
// 一条 user 消息 + 其后所有 assistant/tool 记录（created_at 升序），直到下一条 user 消息。
// 单表扫描 + 游标，确定性规则，零 LLM 成本。

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Record 回合内一条 discussion 记录（含解析后的归一化工具信息）。
type Record struct {
	ID        string
	Role      string
	Content   string
	Tool      ToolRecord
	CreatedAt time.Time
}

// Turn 一个回合：user 消息 + 其后全部记录。
type Turn struct {
	UserMsg string
	Records []Record
	Start   time.Time
	End     time.Time
}

// Files 返回该回合引用的全部文件（去重，供阶段 3 Jaccard 计算）。
func (t *Turn) Files() []string {
	var out []string
	for _, r := range t.Records {
		for _, f := range r.Tool.Files {
			dup := false
			for _, o := range out {
				if o == f {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, f)
			}
		}
	}
	return out
}

// BuildTurns 读取某 session 全部记录并按 user 消息切分为回合。
func BuildTurns(db *sql.DB, sessionID string) ([]Turn, error) {
	rows, err := db.Query(`SELECT id, role, source, content, metadata, created_at FROM discussion_log WHERE session_id = ? ORDER BY created_at ASC, rowid ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query session %s: %w", sessionID, err)
	}
	defer rows.Close()

	var turns []Turn
	for rows.Next() {
		var id, role, source, content, metadata, createdAt string
		if err := rows.Scan(&id, &role, &source, &content, &metadata, &createdAt); err != nil {
			return nil, err
		}
		ts, err := time.Parse("2006-01-02T15:04:05", createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse created_at %q: %w", createdAt, err)
		}
		rec := Record{
			ID:        id,
			Role:      role,
			Content:   content,
			Tool:      ParseToolRecord(source, metadata),
			CreatedAt: ts,
		}
		if role == "user" {
			// 阶段 0 容错：系统日志（[Log]/[Progress]）偶发混入 user 角色（8/19 实测 13 行），
			// 过滤避免产生假回合（校准输入，见 task 备注）。
			if isFakeUser(content) {
				continue
			}
			turns = append(turns, Turn{UserMsg: content, Start: ts, End: ts})
			continue
		}
		if len(turns) == 0 {
			// 无 user 前置的孤立记录：自成一个回合，user 消息留空
			turns = append(turns, Turn{Start: ts, End: ts})
		}
		i := len(turns) - 1
		turns[i].Records = append(turns[i].Records, rec)
		if ts.After(turns[i].End) {
			turns[i].End = ts
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return turns, nil
}

// isFakeUser 判定是否为系统日志混入 user 角色的假消息。
func isFakeUser(content string) bool {
	c := strings.TrimSpace(content)
	return strings.HasPrefix(c, "[Log]") || strings.HasPrefix(c, "[Progress]")
}
