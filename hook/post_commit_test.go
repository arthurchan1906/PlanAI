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
