package hook

import "testing"

func TestShouldHintRecordBug(t *testing.T) {
	cases := []struct {
		name      string
		fileCount int
		recentBug bool
		want      bool
	}{
		{"大变更+无bug应提示", 8, false, true},
		{"大变更+有bug不提示", 8, true, false},
		{"超大变更+有bug不提示", 30, true, false},
		{"小变更+无bug不提示", 3, false, false},
		{"正好阈值-无bug提示", 8, false, true},
		{"低于阈值-无bug不提示", 7, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldHintRecordBug(c.fileCount, c.recentBug); got != c.want {
				t.Fatalf("shouldHintRecordBug(%d,%v)=%v want %v", c.fileCount, c.recentBug, got, c.want)
			}
		})
	}
}

// P0 ①（8/31）：resolveAutoBindTask 高置信绑定规则——
// 消息含 task 引用直接绑；文件唯一命中 in_progress 任务才绑；0/多命中留空（不替 agent 决策）。
func TestResolveAutoBindTask(t *testing.T) {
	taskFiles := map[string][]string{
		"task-20260828-171120-f289f0": {"hook/hook_claude.go", "hook/hook_claude_test.go"},
		"task-20260828-171122-a576b3": {"proxy/context_inject.go"},
	}
	cases := []struct {
		name  string
		title string
		files []string
		want  string
	}{
		{"消息含 task 引用直接绑", "feat(p0-1): foo task-20260828-171120-f289f0", nil, "task-20260828-171120-f289f0"},
		{"文件唯一命中 in_progress 任务", "feat: bar", []string{"hook/hook_claude.go"}, "task-20260828-171120-f289f0"},
		{"文件命中两个任务不绑", "feat: baz", []string{"hook/hook_claude.go", "proxy/context_inject.go"}, ""},
		{"文件无命中不绑", "feat: qux", []string{"unknown.go"}, ""},
		{"无文件无引用不绑", "feat: quux", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveAutoBindTask(c.title, c.files, taskFiles); got != c.want {
				t.Fatalf("resolveAutoBindTask(%q,%v)=%q want %q", c.title, c.files, got, c.want)
			}
		})
	}
}
