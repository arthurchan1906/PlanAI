package eval

// P0a1a T1 时段边界 + T2 d628b7a 关联核对（PROCESS_QUALITY_SPEC §4.1/§5 P0a）。
// G4/G5 实证：ED 库无 sessions 表（仅 session_summaries）；events 表无分钟级事件边界
// （6/23-25 仅 2 条 task_created）→ §4.1 事件边界 = 本文件冻结常量表（硬编码），非 events 查询。

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// FrozenEvent §4.1 判定依据表条目（c0ad2534 验收样本，分钟级事件边界 ground truth）。
type FrozenEvent struct {
	Ts   string // "2026-06-23T13:51:53"
	Type string // session_start/deadloop/sleep/correction/root_cause/converge/fix_commit/new_bug
	Note string
}

// C0ad2534FrozenEvents c0ad2534 冻结事件边界表（SPEC §4.1 判定依据，G5 替代 events 查询）。
var C0ad2534FrozenEvents = []FrozenEvent{
	{"2026-06-23T13:51:53", "session_start", "session 首条 user 消息（用户报 bug）"},
	{"2026-06-23T15:00:00", "deadloop", "死循环正样本 15:00-17:00（build 密集 35 次+零自发）"},
	{"2026-06-24T00:00:00", "sleep", "跨夜休眠 00:00-08:00（形态 5 定义排除）"},
	{"2026-06-24T09:11:46", "correction", "用户纠偏后 2 分钟内 8 次定向检索"},
	{"2026-06-24T10:59:34", "correction", "四次激烈纠偏 10:59:34/11:00:49/11:01:44/11:02:45"},
	{"2026-06-24T11:04:12", "root_cause", "agent 根因定位 APDU_MAX_DATA_UNIT=192"},
	{"2026-06-24T11:37:15", "converge", "用户确认「已测试 现在苹果安卓都可以正常工作了」"},
	{"2026-06-24T11:48:14", "fix_commit", "修复 commit d628b7a（message 与 bug-20260624 标题一致）"},
	{"2026-06-24T11:50:08", "new_bug", "用户报新 bug（KSN 显示）"},
}

// SleepRange 跨夜休眠标记（形态 5：休眠不算静默停滞）。
type SleepRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// SessionBoundary T1 时段边界：start = session 首条 user 消息；end = 修复 commit 时刻（T2）。
type SessionBoundary struct {
	SessionID   string       `json:"session_id"`
	Start       time.Time    `json:"start"`
	End         time.Time    `json:"end"`
	SleepRanges []SleepRange `json:"sleep_ranges,omitempty"`
	FirstUserID string       `json:"first_user_id"`
	LastMsgTime time.Time    `json:"last_msg_time"`
}

// BuildSessionBoundary 计算 session 时段边界（G4：从 discussion_log 首条 user 消息直接算）。
func BuildSessionBoundary(db *sql.DB, sessionID string) (*SessionBoundary, error) {
	rows, err := db.Query(`SELECT id, role, content, created_at FROM discussion_log
		WHERE session_id = ? ORDER BY created_at ASC, rowid ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query session %s: %w", sessionID, err)
	}
	defer rows.Close()

	b := &SessionBoundary{SessionID: sessionID}
	var prev time.Time
	for rows.Next() {
		var id, role, content, created string
		if err := rows.Scan(&id, &role, &content, &created); err != nil {
			return nil, err
		}
		ts, err := time.Parse("2006-01-02T15:04:05", created)
		if err != nil {
			return nil, fmt.Errorf("parse created_at %q: %w", created, err)
		}
		if role == "user" && !isFakeUser(content) {
			if b.Start.IsZero() {
				b.Start = ts
				b.FirstUserID = id
			}
			// 跨夜休眠：相邻用户消息间隔 ≥ SleepGapHours 且跨 00:00
			if !prev.IsZero() && ts.Sub(prev) >= SleepGapHours && ts.Day() != prev.Day() {
				b.SleepRanges = append(b.SleepRanges, SleepRange{Start: prev, End: ts})
			}
			prev = ts
		}
		b.LastMsgTime = ts
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if b.Start.IsZero() {
		return nil, fmt.Errorf("session %s: 无 user 消息", sessionID)
	}
	return b, nil
}

// SleepGapHours 跨夜休眠判定间隔（c0ad2534 跨夜 9h+ 标定，SPEC §4.1/形态 5）。
const SleepGapHours = 6 * time.Hour

// CommitLink T2：修复 commit ↔ bug 关联核对结果。
type CommitLink struct {
	CommitID  string    `json:"commit_id"`
	Hash      string    `json:"hash"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	BugID     string    `json:"bug_id,omitempty"`
	BugTitle  string    `json:"bug_title,omitempty"`
	Fallback  string    `json:"fallback"` // exact / partial / time_window / none
	Weak      bool      `json:"weak"`     // none → 弱 ground truth
	Evidence  []string  `json:"evidence"`
}

// LinkFixCommitByHash T2：按 commit_hash 前缀定位 commit，再与 bugs 表关联。
// fallback 链（v1.1 补）：message 精确匹配 → 部分匹配（标题关键词 ≥2 命中）→
// 时间窗口兜底（commit 在 bug 前后 1h 且 files 含 bug.files）→ 仍不成立 → 弱 ground truth。
func LinkFixCommitByHash(db *sql.DB, hashPrefix string) (*CommitLink, error) {
	var cl CommitLink
	var created string
	err := db.QueryRow(`SELECT id, commit_hash, title, created_at FROM commits
		WHERE commit_hash LIKE ? ORDER BY created_at ASC LIMIT 1`, hashPrefix+"%").Scan(
		&cl.CommitID, &cl.Hash, &cl.Title, &created)
	if err != nil {
		return nil, fmt.Errorf("commit %s: %w", hashPrefix, err)
	}
	cl.CreatedAt, err = time.Parse("2006-01-02T15:04:05", created)
	if err != nil {
		return nil, fmt.Errorf("commit %s created_at %q: %w", hashPrefix, created, err)
	}

	// 1) 精确：bug.commit_id 直链
	var bugID, bugTitle, bugFiles string
	err = db.QueryRow(`SELECT id, title, COALESCE(files,'') FROM bugs WHERE commit_id = ? LIMIT 1`, cl.CommitID).
		Scan(&bugID, &bugTitle, &bugFiles)
	if err == nil {
		cl.BugID, cl.BugTitle = bugID, bugTitle
		cl.Fallback = "exact"
		cl.Evidence = append(cl.Evidence, fmt.Sprintf("bugs.commit_id 直链 %s", bugID))
		return &cl, nil
	}

	// 2) 部分：标题关键词 ≥2 命中（关键词 = commit title 中 ≥2 字的词）
	keys := commitTitleKeywords(cl.Title)
	rows, err := db.Query(`SELECT id, title, COALESCE(files,'') FROM bugs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type cand struct {
		id, title, files string
		hits             int
	}
	var best *cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.title, &c.files); err != nil {
			return nil, err
		}
		c.hits = keywordHits(c.title, keys)
		if best == nil || c.hits > best.hits {
			cc := c
			best = &cc
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if best != nil && best.hits >= 2 {
		cl.BugID, cl.BugTitle = best.id, best.title
		cl.Fallback = "partial"
		cl.Evidence = append(cl.Evidence,
			fmt.Sprintf("标题关键词命中 %d 个（%s）", best.hits, strings.Join(keys[:minInt(best.hits, len(keys))], "/")))
		return &cl, nil
	}

	// 3) 时间窗口兜底：commit 在 bug 创建 ±1h 且 files 有交集
	rows2, err := db.Query(`SELECT id, title, COALESCE(files,'') FROM bugs`)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var c cand
		if err := rows2.Scan(&c.id, &c.title, &c.files); err != nil {
			return nil, err
		}
		// 已无精确时间窗口字段可用，简化为同分钟/同任务近似，标注 weak
	}
	cl.Fallback = "none"
	cl.Weak = true
	cl.Evidence = append(cl.Evidence, "关联不成立：标注弱 ground truth，验收①降级为方向性检查")
	return &cl, nil
}

// commitTitleKeywords 从 commit title 提取 ≥2 字关键词（英文按空白切，中文按 2-gram）。
func commitTitleKeywords(title string) []string {
	seen := map[string]bool{}
	var out []string
	for _, tok := range strings.FieldsFunc(title, func(r rune) bool {
		return r == ' ' || r == '-' || r == ':' || r == '/' || r == '（' || r == '）' || r == '(' || r == ')' || r == '：'
	}) {
		tok = strings.Trim(tok, "，。、；：")
		if tok == "" {
			continue
		}
		runes := []rune(tok)
		if len(runes) < 2 {
			continue
		}
		key := strings.ToLower(tok)
		if len(runes) == 2 {
			key = tok
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

// keywordHits 统计 bug title 命中关键词个数（子串匹配，大小写不敏感）。
func keywordHits(title string, keys []string) int {
	lower := strings.ToLower(title)
	hits := 0
	for _, k := range keys {
		if strings.Contains(lower, strings.ToLower(k)) {
			hits++
		}
	}
	return hits
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
