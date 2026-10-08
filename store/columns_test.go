package store

import (
	"database/sql"
	"strings"
	"testing"

	pmdb "aipmc/db"
)

// 反馈 #48：显式列清单必须与真实表结构一致——列名写错/漏列会让 #48 那类
// 「查询列数与 Scan 目标数不匹配」再次出现。集合相等性检查就是为了让
// 任何一次加列/改列在 CI 里立刻暴露，而不是等到某个旧二进制在用户机器上炸。
func TestExplicitColumnListsMatchSchema(t *testing.T) {
	setupDailyDB(t)
	d, err := pmdb.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	cases := []struct{ table, cols string }{
		{"tasks", taskColumns},
		{"plans", planColumns},
		{"commits", commitColumns},
		{"decisions", decisionColumns},
		{"bugs", bugColumns},
		{"ideas", ideaColumns},
		{"roadmap", roadmapColumns},
		{"principles", principleColumns},
		{"links", linkColumns},
		{"doc_records", docRecordColumns},
		{"daily_notes", dailyNoteColumns},
		{"visions", visionColumns},
		{"canon", canonColumns},
		{"threads", threadColumns},
		{"events", eventColumns},
	}
	for _, c := range cases {
		actual := tableColumnSet(t, d, c.table)
		for _, col := range splitColumns(c.cols) {
			if !actual[col] {
				t.Errorf("%s: 列清单里的 %q 在表里不存在", c.table, col)
			}
			delete(actual, col)
		}
		for missing := range actual {
			t.Errorf("%s: 表里的 %q 没进显式列清单（SELECT 会漏列 / Scan 会错位）", c.table, missing)
		}
	}
}

func splitColumns(cols string) []string {
	parts := strings.Split(cols, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func tableColumnSet(t *testing.T, d *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := d.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info(%s): %v", table, err)
		}
		set[name] = true
	}
	if len(set) == 0 {
		t.Fatalf("table %s not found (no columns)", table)
	}
	return set
}
