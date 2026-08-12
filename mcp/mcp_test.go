package mcp

import (
	"strings"
	"testing"
	"unicode/utf8"
)

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

func TestTruncArgRuneSafe(t *testing.T) {
	args := map[string]interface{}{"title": "iOS UI 对齐 Android 策略：视觉差异优先、改动最小"}
	got := truncArg(args, "title", 30)
	if !utf8.ValidString(got) {
		t.Errorf("truncArg output invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncArg should append ..., got %q", got)
	}
	// 中文单字 3 字节：回退后长度 ≤ max+3，且不出现替换符
	if len(got) > 33 {
		t.Errorf("truncArg result too long: len=%d %q", len(got), got)
	}
	if strings.ContainsRune(got, '\uFFFD') {
		t.Errorf("truncArg output contains replacement char: %q", got)
	}
}
