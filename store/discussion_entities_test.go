package store

import (
	"testing"

	"aipmc/db"
)

// 机制 1（8/28 claude）：read_discussions 附带相关实体——提取/查询/集成三组测试。

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
			name: "工具输出行里的 ID 不误提取（📡 前缀行内容）",
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

// 查询：实体存在返回标题+状态，不存在静默跳过，不存在的 ID 不 panic。
func TestFetchRelatedEntities(t *testing.T) {
	db, err := db.Open()
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer db.Close()

	// 用已知存在的实体（从库中任取一个 decision）
	var existingID string
	if err := db.QueryRow("SELECT id FROM decisions LIMIT 1").Scan(&existingID); err != nil {
		t.Skipf("无 decision 数据: %v", err)
	}

	out := FetchRelatedEntities(db, []string{existingID, "decision-19990101-000000-deadbe"})
	if len(out) != 1 {
		t.Fatalf("FetchRelatedEntities = %v, want 1 (存在=%s, 不存在跳过)", out, existingID)
	}
	if out[0].ID != existingID || out[0].Title == "" {
		t.Fatalf("FetchRelatedEntities[0] = %+v, want id=%s title 非空", out[0], existingID)
	}

	// 空输入
	if got := FetchRelatedEntities(db, nil); len(got) != 0 {
		t.Fatalf("FetchRelatedEntities(nil) = %v, want empty", got)
	}
}

// 集成：RelatedEntitiesFromRows 从 ReadDiscussions 风格的行提取并查询。
func TestRelatedEntitiesFromRows(t *testing.T) {
	db, err := db.Open()
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer db.Close()

	// 构造一行含已知 decision ID 的假讨论行
	var existingID string
	if err := db.QueryRow("SELECT id FROM decisions LIMIT 1").Scan(&existingID); err != nil {
		t.Skipf("无 decision 数据: %v", err)
	}
	rows := []map[string]any{
		{"content": "这个方案和 " + existingID + " 有关，注意其约束"},
		{"content": "纯文本无引用"},
	}
	out := RelatedEntitiesFromRows(db, rows)
	if len(out) != 1 || out[0].ID != existingID {
		t.Fatalf("RelatedEntitiesFromRows = %v, want [%s]", out, existingID)
	}
}
