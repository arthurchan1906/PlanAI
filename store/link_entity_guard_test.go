package store

import (
	"strings"
	"testing"
)

// 反馈 #43：record_bug(commit_id="ba6a67e") 此前只做 PM id 精确匹配，短
// hash 直接报「commit not found」且不给格式指引。
func TestResolveCommitRef(t *testing.T) {
	d := openTestDB(t)
	insertCommit(t, d, "commit-20260916-172333-06c77e", "eb251bc06233292138090355acd9a66bc591987")
	insertCommit(t, d, "commit-20260916-172359-a05e69", "76276cfaabbccdd00112233445566778899aabbcc")
	// 前缀冲突：两条记录共享 7 位前缀，必须要求更长 hash 而不是随便挑一条。
	insertCommit(t, d, "commit-dup-1", "aaaaaaa1111111111111111111111111111111111")
	insertCommit(t, d, "commit-dup-2", "aaaaaaa2222222222222222222222222222222222")

	cases := []struct {
		name    string
		ref     string
		want    string
		wantErr []string
	}{
		{name: "pm id 精确命中", ref: "commit-20260916-172333-06c77e", want: "commit-20260916-172333-06c77e"},
		{name: "7 位短 hash 唯一命中", ref: "eb251bc", want: "commit-20260916-172333-06c77e"},
		{name: "完整 hash 命中", ref: "76276cfaabbccdd00112233445566778899aabbcc", want: "commit-20260916-172359-a05e69"},
		{name: "前缀冲突报错", ref: "aaaaaaa", wantErr: []string{"命中 2 条"}},
		{name: "未知 ref 报格式指引", ref: "ffffff", wantErr: []string{"commit 不存在", "commit-20260922-140451-e58f9d", "git SHA", "aipm_list_commits"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveCommitRef(d, c.ref)
			if len(c.wantErr) > 0 {
				if err == nil {
					t.Fatalf("resolveCommitRef(%q) = %q, want error", c.ref, got)
				}
				for _, w := range c.wantErr {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("error %q missing %q", err.Error(), w)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveCommitRef(%q) unexpected error: %v", c.ref, err)
			}
			if got != c.want {
				t.Errorf("resolveCommitRef(%q) = %q, want %q", c.ref, got, c.want)
			}
		})
	}
}

// 反馈 #38：link_entities 对不存在的实体 id 不校验即建边，静默产生悬空边，
// 之后 trace_context 会显示「某 bug 被两个 commit 修复」其中一条不存在。
func TestValidateLinkEntity(t *testing.T) {
	db := newIsolatedTestDB(t)
	insertTestDecision(t, db)

	if err := validateLinkEntity(db, "decision", testDecisionID); err != nil {
		t.Errorf("已存在实体被拒: %v", err)
	}
	// 大小写不敏感（MCP 侧可能传 Bug/TASK）
	if err := validateLinkEntity(db, "Decision", testDecisionID); err != nil {
		t.Errorf("大小写不应影响校验: %v", err)
	}

	err := validateLinkEntity(db, "bug", "bug-20260911-120532-da3e70")
	if err == nil {
		t.Fatal("幻影 bug id 必须被拒绝")
	}
	if !strings.Contains(err.Error(), "不存在") || !strings.Contains(err.Error(), "aipm_search_context") {
		t.Errorf("错误信息缺少定位指引: %v", err)
	}

	if err := validateLinkEntity(db, "commit", "commit-20260911-120526-2798b6"); err == nil {
		t.Error("幻影 commit id（反馈 #38 的实例）必须被拒绝")
	}

	// 非 PM 实体类型（discussion/session 等自动关联路径）必须放行。
	for _, typ := range []string{"discussion", "session", "unknown-type"} {
		if err := validateLinkEntity(db, typ, "whatever-1"); err != nil {
			t.Errorf("非 PM 实体类型 %q 应放行，却报错: %v", typ, err)
		}
	}
}
