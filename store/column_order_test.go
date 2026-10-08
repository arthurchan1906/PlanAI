package store

import (
	"testing"

	pmdb "aipmc/db"
)

// 反馈 #48：显式列清单除了「列名对不对」，还必须「列序对」——位置 Scan 是按
// 声明顺序取值的。本机这几个表在真实库里是 0 行（快照 diff 覆盖不到），
// 这里用可区分的值做一次往返，任何位置错位都会让字段落到错误的 key 上。
func TestColumnOrderRoundTripOnEmptyTables(t *testing.T) {
	setupDailyDB(t)

	// principles：全字段给不同值
	p, err := CreatePrinciple("标题P", "摘要P", "工程原则", "active")
	if err != nil {
		t.Fatalf("CreatePrinciple: %v", err)
	}
	got, err := GetPrinciple(p["id"].(string))
	if err != nil {
		t.Fatalf("GetPrinciple: %v", err)
	}
	for k, want := range map[string]any{"title": "标题P", "summary": "摘要P", "kind": "工程原则", "status": "active"} {
		if got[k] != want {
			t.Errorf("principles.%s = %v, want %v（列序错位？）", k, got[k], want)
		}
	}
	list, err := ListPrinciples("", "")
	if err != nil {
		t.Fatalf("ListPrinciples: %v", err)
	}
	if len(list) != 1 || list[0]["kind"] != "工程原则" {
		t.Errorf("ListPrinciples = %v, want one row with kind=工程原则", list)
	}

	// doc_records：store 里只有读路径（无写路径），直接 INSERT 验列序
	d, err := pmdb.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO doc_records (path, type, status, layer, source_of_truth, last_reviewed, superseded_by) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"docs/path.md", "typeX", "statusX", "layerX", 1, "2026-01-02", "superX"); err != nil {
		t.Fatal(err)
	}
	docs, err := ListDocRecords("", "")
	if err != nil {
		t.Fatalf("ListDocRecords: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("ListDocRecords = %d rows, want 1", len(docs))
	}
	doc := docs[0]
	for k, want := range map[string]any{
		"path": "docs/path.md", "type": "typeX", "status": "statusX", "layer": "layerX",
		"source_of_truth": true, "last_reviewed": "2026-01-02", "superseded_by": "superX",
	} {
		if doc[k] != want {
			t.Errorf("doc_records.%s = %v, want %v（列序错位？）", k, doc[k], want)
		}
	}

	// daily_notes：四段 JSON 各自可区分
	if _, err := UpsertDaily("2026-01-02", map[string][]string{
		"completed": {"c1"}, "problems": {"p1"}, "risks": {"r1"}, "next": {"n1"},
	}, false); err != nil {
		t.Fatalf("UpsertDaily: %v", err)
	}
	dn, err := GetDailyNote("2026-01-02")
	if err != nil {
		t.Fatalf("GetDailyNote: %v", err)
	}
	for k, want := range map[string]string{"completed": "c1", "problems": "p1", "risks": "r1", "next": "n1"} {
		vals, ok := dn[k].([]any)
		if !ok || len(vals) != 1 || vals[0] != want {
			t.Errorf("daily_notes.%s = %v, want [%s]（列序错位？）", k, dn[k], want)
		}
	}
}
