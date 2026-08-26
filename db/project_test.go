package db

import "testing"

// ProjectRoot 单点推导（8/26 C2）：正常 <proj>/.pmai/data/pmai.db 与 env 模式
// <PMAI_HOME>/data/pmai.db 统一——8/26 实测 Dir² 停在 .pmai 的错值回归锁定。
func TestProjectRoot(t *testing.T) {
	cases := []struct {
		name, dbPath, want string
	}{
		{"正常项目", "/Users/me/proj/.pmai/data/pmai.db", "/Users/me/proj"},
		{"env PMAI_HOME", "/Users/me/aipmc-data/data/pmai.db", "/Users/me/aipmc-data"},
		{"嵌套路径", "/a/b/c/.pmai/data/pmai.db", "/a/b/c"},
	}
	for _, c := range cases {
		if got := ProjectRoot(c.dbPath); got != c.want {
			t.Errorf("%s: ProjectRoot(%q) = %q, want %q", c.name, c.dbPath, got, c.want)
		}
	}
}
