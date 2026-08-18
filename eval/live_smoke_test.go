//go:build smoke

package eval

import (
	"fmt"
	"testing"

	"aipmc/db"
)

// TestLiveSmoke 真实库端到端：BuildTurns + 兜底分类 + SegmentEpisodes。
// 运行：CGO_ENABLED=0 go test -tags smoke -run TestLiveSmoke -v ./eval/
func TestLiveSmoke(t *testing.T) {
	d, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var sid string
	var n int
	if err := d.QueryRow(`SELECT session_id, COUNT(*) FROM discussion_log WHERE role='user' GROUP BY session_id ORDER BY COUNT(*) DESC LIMIT 1`).Scan(&sid, &n); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("session=%s user_msgs=%d\n", sid, n)
	turns, err := BuildTurns(d, sid)
	if err != nil {
		t.Fatal(err)
	}
	classes := make([]IntentClass, len(turns))
	for i := range turns {
		classes[i] = ClassifyIntent(turns[i].UserMsg, nil)
	}
	eps := SegmentEpisodes(sid, "codex-cli", turns, classes, nil, DefaultSegParams())
	fmt.Printf("turns=%d episodes=%d\n", len(turns), len(eps))
	for i, ep := range eps {
		fmt.Printf("  ep%d [%s] %d turns files=%d commits=%d jaccard=%v intent=%q\n",
			i, ep.Boundary, len(ep.Turns), len(ep.Files), len(ep.Commits), ep.JaccardHit, trunc(ep.IntentText, 20))
	}
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
