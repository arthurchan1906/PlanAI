package store

import (
	"database/sql"
	"testing"

	pmdb "aipmc/db"
)

// 机制 1（8/28 claude）：read_discussions 附带相关实体——提取/查询/集成三组测试。
// 8/28 codex 审核 Ch2：查询/集成测试改独立临时库（PMAI_HOME 隔离），
// 原实现 db.Open() 碰 cwd 生产库，无库环境静默 skip、测试形同虚设。

// 提取：纯正则，只认完整实体 ID，容忍大小写，去重保序。
func TestExtractRelatedEntities(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "单类型多引用去重",
			in:   []string{"看 decision-20260812-174826-c13824 和 decision-20260812-174826-c13824 又一次"},
			want: []string{"decision-20260812-174826-c13824"},
		},
		{
			name: "多类型保序",
			in:   []string{"task-20260827-111103-939d0f 依赖 decision-20260826-172138-fb48b1"},
			want: []string{"task-20260827-111103-939d0f", "decision-20260826-172138-fb48b1"},
		},
		{
			name: "大小写容忍",
			in:   []string{"Decision-20260812-174826-C13824"},
			want: []string{"Decision-20260812-174826-C13824"},
		},
		{
			name: "不完整 ID 不匹配",
			in:   []string{"decision-20260812-174826（少了后缀）"},
			want: nil,
		},
		{
			name: "跨行",
			in:   []string{"第一行 task-20260827-111103-939d0f", "第二行 bug-20260824-101846-ab12cd"},
			want: []string{"task-20260827-111103-939d0f", "bug-20260824-101846-ab12cd"},
		},
		{
			name: "工具输出行照常提取（生产由 substantive 过滤兜底，见 ReadDiscussions）",
			in:   []string{"📡 aipm_update_task_status ✅ task-20260827-111103-939d0f →done"},
			want: []string{"task-20260827-111103-939d0f"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExtractRelatedEntities(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("ExtractRelatedEntities(%v) = %v, want %v", c.in, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("ExtractRelatedEntities(%v)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
				}
			}
		})
	}
}

// newIsolatedTestDB 建独立临时库（PMAI_HOME 隔离，Ch2）。
func newIsolatedTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PMAI_HOME", dir)
	if _, err := pmdb.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	db, err := pmdb.Open()
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

const testDecisionID = "decision-20260101-000000-abc123"

func insertTestDecision(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO decisions
		(id, title, date, status, background, decision_text, impact_json, alternatives_json, related_tasks_json, updates_canon)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		testDecisionID, "测试决策标题", "2026-01-01", "accepted", "背景", "决策内容", "{}", "[]", "[]", 0)
	if err != nil {
		t.Fatalf("insert decision: %v", err)
	}
}

// 查询：实体存在返回标题+状态，不存在静默跳过，不存在的 ID 不 panic。
func TestFetchRelatedEntities(t *testing.T) {
	db := newIsolatedTestDB(t)
	insertTestDecision(t, db)

	out := FetchRelatedEntities(db, []string{testDecisionID, "decision-19990101-000000-deadbe"})
	if len(out) != 1 {
		t.Fatalf("FetchRelatedEntities = %v, want 1 (存在=%s, 不存在跳过)", out, testDecisionID)
	}
	if out[0].ID != testDecisionID || out[0].Title != "测试决策标题" || out[0].Status != "accepted" {
		t.Fatalf("FetchRelatedEntities[0] = %+v, want id=%s title=测试决策标题 status=accepted", out[0], testDecisionID)
	}

	// 空输入
	if got := FetchRelatedEntities(db, nil); len(got) != 0 {
		t.Fatalf("FetchRelatedEntities(nil) = %v, want empty", got)
	}
}

// 集成：RelatedEntitiesFromRows 从 ReadDiscussions 风格的行提取并查询。
func TestRelatedEntitiesFromRows(t *testing.T) {
	db := newIsolatedTestDB(t)
	insertTestDecision(t, db)

	rows := []map[string]any{
		{"content": "这个方案和 " + testDecisionID + " 有关，注意其约束"},
		{"content": "纯文本无引用"},
	}
	out := RelatedEntitiesFromRows(db, rows)
	if len(out) != 1 || out[0].ID != testDecisionID {
		t.Fatalf("RelatedEntitiesFromRows = %v, want [%s]", out, testDecisionID)
	}
}
