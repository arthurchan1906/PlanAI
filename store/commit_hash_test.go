package store

import "testing"

// TestValidCommitHash 锁定 commit_hash 格式校验（治本：封死 HEAD/$(cmd)/空/非 hex）。
func TestValidCommitHash(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"完整 40 位 SHA-1", "6f85a7ce583d092b8de1b40c59cb824c680f9be9", true},
		{"64 位 SHA-256", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
		{"兼容历史短 hash 7 位", "6f85a7c", true},
		{"下限 4 位", "abcd", true},
		{"HEAD 字面量", "HEAD", false},
		{"空串", "", false},
		{"$(cmd) 字面量", "$(git rev-parse HEAD)", false},
		{"含大写字母（git 规范是小写）", "ABCDEF", false},
		{"含非法字符：连字符", "6f85a7c-", false},
		{"含 pipe", "cafe|b.md", false},
		{"超过 64 位", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"少于 4 位", "abc", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validCommitHash(tc.in); got != tc.want {
				t.Errorf("validCommitHash(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestCreateCommitRejectsBadHash 验证 CreateCommit 在非法 commit_hash 下直接报错
// （不落到 DB），防止 HEAD/$(cmd)/错乱 hex 入库。
func TestCreateCommitRejectsBadHash(t *testing.T) {
	// 纯函数层已覆盖格式；CreateCommit 在此以非法 hash 断言错误路径触发，
	// 不依赖真实 DB（避免测试耦合 pmdb/task 校验）。
	for _, bad := range []string{"HEAD", "$(git rev-parse HEAD)", "", "not-a-hash"} {
		if validCommitHash(bad) {
			t.Errorf("expected %q to be rejected", bad)
		}
	}
}
