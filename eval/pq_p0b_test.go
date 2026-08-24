package eval

import (
	"strings"
	"testing"
	"time"
)

// ── ⑤ 对象级加深方向性 ──

func TestDetectObjectDeepeningConcentrated(t *testing.T) {
	// 单点死磕实证形态：同一文件域高度集中 + 重复访问（019ff89b 方向性镜像）
	var recs []Record
	ts := mustTs("2026-08-13T09:10:00")
	for i := 0; i < 30; i++ {
		recs = append(recs, Record{
			Role:      "assistant",
			Tool:      ToolRecord{Tool: "bash", Files: []string{"EncryptDrive/Shared/Storage/LocalVaultStore.swift"}},
			CreatedAt: ts.Add(time.Duration(i) * 10 * time.Minute),
		})
	}
	for i := 0; i < 5; i++ {
		recs = append(recs, Record{
			Role:      "assistant",
			Tool:      ToolRecord{Tool: "edit", Files: []string{"EncryptDrive/Features/Main/ContentView.swift"}},
			CreatedAt: ts.Add(time.Duration(30+i) * 10 * time.Minute),
		})
	}
	turns := []Turn{{UserMsg: "设备下拉列表状态点…对比安卓实现", Start: ts, End: ts.Add(6 * time.Hour), Records: recs}}
	d := DetectObjectDeepening(turns, "019ff89b-test")
	if d.UniqueObjects != 2 {
		t.Fatalf("unique objects = %d, want 2", d.UniqueObjects)
	}
	if d.Top1Concentrate < 0.8 {
		t.Errorf("top1 concentration = %.2f, want ≥0.8（对象高度集中）", d.Top1Concentrate)
	}
	if d.DomainConcentrate < 1.0 {
		t.Errorf("domain concentration = %.2f, want 1.0（同文件域）", d.DomainConcentrate)
	}
	if !strings.Contains(d.Verdict, "加深✗") {
		t.Errorf("verdict = %q, want 含「加深✗」", d.Verdict)
	}
}

func TestDetectObjectDeepeningNoData(t *testing.T) {
	turns := []Turn{{UserMsg: "你好", Start: mustTs("2026-08-13T09:10:00"), Records: []Record{
		{Role: "assistant", Content: "无文件引用", Tool: ToolRecord{Tool: "llm_message"}, CreatedAt: mustTs("2026-08-13T09:11:00")},
	}}}
	d := DetectObjectDeepening(turns, "s")
	if d.Verdict != "不可判定" {
		t.Errorf("verdict = %q, want 不可判定（对象级数据为空）", d.Verdict)
	}
}

func TestDomainOf(t *testing.T) {
	cases := map[string]string{
		"EncryptDrive/Shared/Storage/LocalVaultStore.swift": "EncryptDrive",
		"app/src/main/java/x.java":                          "app",
		"EncryptDrive.xcodeproj":                            "EncryptDrive.xcodeproj",
		"/abs/path/a.go":                                    "abs",
	}
	for in, want := range cases {
		if got := domainOf(in); got != want {
			t.Errorf("domainOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// ── ⑥ 候选→人工确认闭环 ──

func TestSelectConfirmWindowsTen(t *testing.T) {
	// c0ad2534：死循环 2 + hint_responded 1 + hint_missed 1 + 自建记录 1
	c0 := &P0a2Report{Proactive: []ProactiveCandidate{
		{SceneAt: mustTs("2026-06-23T15:00:00"), SceneKind: "deadloop_no_aipm", WindowMin: 60, SelfRetrieval: 0},
		{SceneAt: mustTs("2026-06-23T16:00:00"), SceneKind: "deadloop_no_aipm", WindowMin: 60, SelfRetrieval: 0},
		{SceneAt: mustTs("2026-06-23T17:20:00"), SceneKind: "hint_responded", WindowMin: 30, SelfRetrieval: 13},
		{SceneAt: mustTs("2026-06-24T09:11:00"), SceneKind: "hint_missed", WindowMin: 30, SelfRetrieval: 0},
	}, SelfRecords: []SelfRecordCandidate{
		{CreatedAt: mustTs("2026-06-24T11:44:00"), Kind: "record_bug", WorkRecords: 94, DelayMin: 253},
	}}
	// 01a013f3：hint_responded 1 + 静态可核对 1 + 重复验证 2 + 自建记录 1
	o1 := &P0a2Report{Proactive: []ProactiveCandidate{
		{SceneAt: mustTs("2026-08-18T16:19:51"), SceneKind: "hint_responded", WindowMin: 30, SelfRetrieval: 5},
	}, StaticChecks: []StaticCheckCandidate{
		{RoundAt: mustTs("2026-08-20T10:50:00"), RoundKind: "device_cmd", WindowMin: 30},
	}, RepeatedVerif: []RepeatedVerificationCandidate{
		{EpisodeStart: mustTs("2026-08-19T15:33:00"), EpisodeEnd: mustTs("2026-08-20T08:49:00"), Count: 9},
		{EpisodeStart: mustTs("2026-08-20T08:49:00"), EpisodeEnd: mustTs("2026-08-20T10:40:00"), Count: 3},
	}, SelfRecords: []SelfRecordCandidate{
		{CreatedAt: mustTs("2026-08-19T15:32:00"), Kind: "record_bug", WorkRecords: 41, FirstConsultAt: mustTs("2026-08-19T17:29:00"), DelayMin: 117},
	}}
	allTurns := map[string][]Turn{
		"c0ad2534": {{
			UserMsg: "u", Start: mustTs("2026-06-23T13:51:00"),
			Records: []Record{{Role: "assistant", Content: "调试", Tool: ToolRecord{Tool: "bash"}, CreatedAt: mustTs("2026-06-23T15:05:00")}},
		}},
		"01a013f3": {{
			UserMsg: "u", Start: mustTs("2026-08-18T16:00:00"),
			Records: []Record{{Role: "assistant", Content: "改代码", Tool: ToolRecord{Tool: "edit"}, CreatedAt: mustTs("2026-08-19T16:51:00")}},
		}},
	}
	wins := SelectConfirmWindows(map[string]*P0a2Report{"c0ad2534": c0, "01a013f3": o1}, allTurns)
	if len(wins) != 10 {
		t.Fatalf("候选时段 = %d, want 10（覆盖 5 检测点）", len(wins))
	}
	det := map[string]int{}
	spots := 0
	for _, w := range wins {
		det[w.Detector]++
		if w.SpotCheck {
			spots++
		}
	}
	if det["死循环时段该用未用"] != 2 {
		t.Errorf("死循环时段候选 = %d, want 2", det["死循环时段该用未用"])
	}
	if det["用户提示后响应"]+det["用户提示后响应（missed）"] != 3 {
		t.Errorf("hint 候选 = %d, want 3（responded 2 + missed 1）", det["用户提示后响应"]+det["用户提示后响应（missed）"])
	}
	if spots != 3 {
		t.Errorf("抽检时段 = %d, want 3", spots)
	}
}

func TestWindowRecordsLimit(t *testing.T) {
	var recs []Record
	for i := 0; i < 15; i++ {
		recs = append(recs, Record{
			Role: "assistant", Content: "记录", Tool: ToolRecord{Tool: "bash"},
			CreatedAt: mustTs("2026-06-23T15:00:00").Add(time.Duration(i) * time.Minute),
		})
	}
	turns := []Turn{{UserMsg: "u", Records: recs}}
	rows := windowRecords(turns, mustTs("2026-06-23T14:00:00"), mustTs("2026-06-23T16:00:00"), 10)
	if len(rows) != 10 {
		t.Fatalf("window records = %d, want 10（上限截断）", len(rows))
	}
}
