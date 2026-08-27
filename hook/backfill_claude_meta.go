package hook

// T3b 数据地基A：claude tool 行空 metadata 历史回填。
// 背景：hook_claude.go 早期版本对 Read 之外的工具只写 desc 不写 metadata，
// default 分支（mcp__aipm__*、WebSearch、ask* 等）至今仍空 → S2-claude
// tool-role 行空串率 33%（实测 2359/7162）。hook 修复（post_tool 格式）只
// 管新行，历史空行需本函数回填才能达成 H4-A「空串率 <10%」验收。
// 回填规则：
//   - 👁 read 行（content = "👁 <path> (N lines)"）→ {"type":"read",...}
//   - 🛠 default 行（content = "🛠 <tool_name>"）→ {"_type":"post_tool","tool_name":...}
// 幂等：仅更新 metadata 为空的行；无法解析的 content 跳过不写。

import (
	"encoding/json"
	"fmt"
	"regexp"

	pmdb "aipmc/db"
)

var (
	reClaudeReadLine = regexp.MustCompile(`^👁\s+(\S+)(?:\s+\(\d+\s+lines\))?$`)
	reClaudeToolLine = regexp.MustCompile(`^🛠\s+(\S.*)$`)
)

// BackfillClaudeToolMetadata 回填 claude tool 行历史空 metadata。
// 返回 (updated, skipped, error)——skipped = 无法解析/跳过行。
func BackfillClaudeToolMetadata() (updated, skipped int, err error) {
	db, err := pmdb.Open()
	if err != nil {
		return 0, 0, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, content FROM discussion_log
		WHERE source='claude-code' AND role='tool' AND (metadata IS NULL OR metadata='')`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	var targets []struct{ id, content string }
	for rows.Next() {
		var id, content string
		if err := rows.Scan(&id, &content); err != nil {
			return 0, 0, err
		}
		targets = append(targets, struct{ id, content string }{id, content})
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	for _, t := range targets {
		meta := backfillMetaFor(t.content)
		if meta == "" {
			skipped++
			continue
		}
		// 与 store.execBusy 同构的 BUSY 重试（D 线决策：retry 全覆盖）。
		err = pmdb.RetryBusy(func() error {
			_, e := db.Exec(`UPDATE discussion_log SET metadata=? WHERE id=? AND (metadata IS NULL OR metadata='')`, meta, t.id)
			return e
		})
		if err != nil {
			return updated, skipped, fmt.Errorf("update %s: %w", t.id, err)
		}
		updated++
	}
	return updated, skipped, nil
}

// backfillMetaFor 从 claude tool 行 content 构造可解析 metadata；
// 无法识别返回空串（调用方跳过）。
func backfillMetaFor(content string) string {
	if m := reClaudeReadLine.FindStringSubmatch(content); m != nil {
		path := m[1]
		rel := ToRelPath(path)
		b, err := json.Marshal(map[string]string{
			"type":      "read",
			"file_path": path,
			"rel_path":  rel,
			"source":    "backfill",
		})
		if err != nil {
			return ""
		}
		return string(b)
	}
	if m := reClaudeToolLine.FindStringSubmatch(content); m != nil {
		b, err := json.Marshal(map[string]string{
			"_type":     "post_tool",
			"tool_name": m[1],
		})
		if err != nil {
			return ""
		}
		return string(b)
	}
	return ""
}
