package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"aipmc/store"
)

func setupUpdateTaskDB(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "data", "pmai.db"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PMAI_HOME", home)
}

func seedTask(t *testing.T) string {
	t.Helper()
	rm, err := store.CreateRoadmap("反馈47 roadmap", "", "", "active", "P1")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.CreatePlan("反馈47 plan", "goal", rm["id"].(string), "", "P1", "active", nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask("", "原标题", "P1", "todo", "general", plan["id"].(string), nil)
	if err != nil {
		t.Fatal(err)
	}
	return task["id"].(string)
}

// 反馈 #47：aipm_update_task 传 title/priority 后必须真正落库，而不是回 ✅ 但没改。
func TestHandleUpdateTaskAppliesTitlePriorityPhase(t *testing.T) {
	setupUpdateTaskDB(t)
	id := seedTask(t)
	s := &mcpServer{}

	res := s.handleUpdateTask(map[string]interface{}{
		"task_id": id, "title": "改后的标题", "priority": "P0", "phase": "measurement",
	})
	if res.IsError {
		t.Fatalf("handleUpdateTask error: %s", res.Content[0].Text)
	}
	got, err := store.GetTaskSimple(id)
	if err != nil {
		t.Fatal(err)
	}
	if got["title"] != "改后的标题" {
		t.Errorf("title = %v, want 改后的标题", got["title"])
	}
	if got["priority"] != "P0" {
		t.Errorf("priority = %v, want P0", got["priority"])
	}
	if got["phase"] != "measurement" {
		t.Errorf("phase = %v, want measurement", got["phase"])
	}
	if got["status"] != "todo" {
		t.Errorf("status = %v, want todo (untouched)", got["status"])
	}
}

// 一个字段都没传时必须报错，不能回 ✅ 静默 no-op（反馈面：调用成功但没改）。
func TestHandleUpdateTaskRejectsEmptyFieldSet(t *testing.T) {
	setupUpdateTaskDB(t)
	id := seedTask(t)
	s := &mcpServer{}

	res := s.handleUpdateTask(map[string]interface{}{"task_id": id})
	if !res.IsError {
		t.Errorf("empty field set must be an error, got %q", res.Content[0].Text)
	}
}

// status 走原路径（含 done-gate 旁路与状态变更备注），与 title/priority 并存。
func TestHandleUpdateTaskStatusStillWorksWithFields(t *testing.T) {
	setupUpdateTaskDB(t)
	id := seedTask(t)
	s := &mcpServer{}

	res := s.handleUpdateTask(map[string]interface{}{
		"task_id": id, "status": "in_progress", "title": "标题 v2",
	})
	if res.IsError {
		t.Fatalf("handleUpdateTask error: %s", res.Content[0].Text)
	}
	got, err := store.GetTaskSimple(id)
	if err != nil {
		t.Fatal(err)
	}
	if got["status"] != "in_progress" {
		t.Errorf("status = %v, want in_progress", got["status"])
	}
	if got["title"] != "标题 v2" {
		t.Errorf("title = %v, want 标题 v2", got["title"])
	}
}
