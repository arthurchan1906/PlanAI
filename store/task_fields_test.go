package store

import "testing"

// 反馈 #47：aipm_update_task 的 title/priority/phase 必须真正落库。
// 此前 handler 只读 status/note，字段被静默丢弃仍回 ✅。
func TestUpdateTaskFieldsAppliesTitlePriorityPhase(t *testing.T) {
	setupDailyDB(t)
	rm, err := CreateRoadmap("字段落库测试 roadmap", "", "", "active", "P1")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := CreatePlan("字段落库测试 plan", "goal", rm["id"].(string), "", "P1", "active", nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := CreateTask("", "原标题", "P1", "todo", "general", plan["id"].(string), nil)
	if err != nil {
		t.Fatal(err)
	}
	id := task["id"].(string)

	out, err := UpdateTaskFields(id, map[string]any{"title": "新标题", "priority": "P0", "phase": "measurement"})
	if err != nil {
		t.Fatalf("UpdateTaskFields: %v", err)
	}
	if out["title"] != "新标题" {
		t.Errorf("returned title = %v, want 新标题", out["title"])
	}
	got, err := GetTaskSimple(id)
	if err != nil {
		t.Fatal(err)
	}
	if got["title"] != "新标题" {
		t.Errorf("title = %v, want 新标题", got["title"])
	}
	if got["priority"] != "P0" {
		t.Errorf("priority = %v, want P0", got["priority"])
	}
	if got["phase"] != "measurement" {
		t.Errorf("phase = %v, want measurement", got["phase"])
	}
	// status 未传必须保持原值，不能被空串覆盖。
	if got["status"] != "todo" {
		t.Errorf("status = %v, want todo (untouched)", got["status"])
	}
}

// 未知 key 必须被白名单挡掉，不能拼进 SQL。
func TestUpdateTaskFieldsRejectsUnknownKey(t *testing.T) {
	setupDailyDB(t)
	rm, _ := CreateRoadmap("白名单 roadmap", "", "", "active", "P1")
	plan, _ := CreatePlan("白名单 plan", "goal", rm["id"].(string), "", "P1", "active", nil, nil, nil, nil)
	task, _ := CreateTask("", "原标题", "P1", "todo", "general", plan["id"].(string), nil)
	id := task["id"].(string)

	if _, err := UpdateTaskFields(id, map[string]any{"note": "x", "status; DROP TABLE tasks": "y"}); err == nil {
		t.Error("unknown keys must be rejected, got nil error")
	}
	got, err := GetTaskSimple(id)
	if err != nil {
		t.Fatal(err)
	}
	if got["title"] != "原标题" {
		t.Errorf("title must be untouched, got %v", got["title"])
	}
}
