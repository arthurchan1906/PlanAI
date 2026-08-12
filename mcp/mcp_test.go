package mcp

import "testing"

func TestClassifyMCPErr(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"done-gate reject", "task cannot be marked done without at least one verified approved commit", "business_reject"},
		{"already bound commit", "Commit 已存在且已绑定 task t-1，与传入 c-2 冲突", "idempotent"},
		{"system fault", "更新失败: SQLITE_BUSY", "system_fault"},
		{"empty text", "", "system_fault"},
		{"unrelated text", "some random error", "system_fault"},
	}
	for _, c := range cases {
		if got := classifyMCPErr(c.text); got != c.want {
			t.Errorf("%s: classifyMCPErr(%q) = %q, want %q", c.name, c.text, got, c.want)
		}
	}
}
