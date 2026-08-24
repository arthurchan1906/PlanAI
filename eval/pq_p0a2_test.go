package eval

// P0a2 方向性报告测试：主动触发（工具采用）/ 静态可核对 / P3 计数基线（重复验证点/自建记录利用）。
// 实证锚点：c0ad2534（死循环时段零自发 aipm + 17:20 提示响应）、01a013f3（10:16 重复验证抗议、
// 10:52→10:56→11:15 静态核对教训、15:32 record_bug 后 17:29 才检索）。

import (
	"testing"
)

// ── 主动触发（工具采用）──

func TestDetectProactiveTriggers(t *testing.T) {
	turns := []Turn{
		pqTurn("u1", "用户报 bug", "2026-06-23T15:00:00"),
		{Records: []Record{
			pqRec("bash", "go build ./...", "2026-06-23T15:10:00"),
			pqRec("bash", "go build ./...", "2026-06-23T15:20:00"),
		}},
		// 16h 死循环桶（零 aipm）
		{Records: []Record{
			pqRec("bash", "go build ./...", "2026-06-23T16:10:00"),
			pqRec("bash", "go build ./...", "2026-06-23T16:20:00"),
		}},
		// 17h：用户提示后 aipm 响应
		pqTurn("u2", "每次在你修改代码之前 你或许最好可以查看搜索aipm中有没有相关记录", "2026-06-23T17:20:02"),
		{Records: []Record{
			pqRec("mcp_aipm_search", "", "2026-06-23T17:20:07"),
		}},
		// 提示未响应（后续无 aipm）
		pqTurn("u3", "你最好查看一下aipm中的有关记录", "2026-06-24T09:11:46"),
	}
	// T5 死循环候选：15h/16h build 密集零自发（SpontRetr=0），17h 有被动检索不构成候选
	deadloops := []DeadloopCandidate{
		{Start: mustTs("2026-06-23T15:00:00"), End: mustTs("2026-06-23T16:00:00"), Builds: 2, SpontRetr: 0},
		{Start: mustTs("2026-06-23T16:00:00"), End: mustTs("2026-06-23T17:00:00"), Builds: 2, SpontRetr: 0},
		{Start: mustTs("2026-06-23T17:00:00"), End: mustTs("2026-06-23T18:00:00"), Builds: 3, SpontRetr: 0, Excluded: true, Reason: "被动检索>0"},
	}
	cands := DetectProactiveTriggers(turns, deadloops, DefaultProactiveParams())

	kinds := map[string]int{}
	for _, c := range cands {
		kinds[c.SceneKind]++
	}
	if kinds["deadloop_no_aipm"] != 2 {
		t.Errorf("deadloop_no_aipm = %d, want 2（15h/16h 零自发 = 该用未用）", kinds["deadloop_no_aipm"])
	}
	if kinds["deadloop_used_aipm"] != 0 {
		t.Errorf("deadloop_used_aipm = %d, want 0", kinds["deadloop_used_aipm"])
	}
	if kinds["hint_responded"] != 1 {
		t.Errorf("hint_responded = %d, want 1（17:20 提示 → 30 秒内 aipm 响应）", kinds["hint_responded"])
	}
	if kinds["hint_missed"] != 1 {
		t.Errorf("hint_missed = %d, want 1（09:11 提示未响应）", kinds["hint_missed"])
	}
	// 排除的 near-miss（17h，被动检索>0）不算该用未用场景
	for _, c := range cands {
		if c.SceneAt.Format("2006-01-02T15") == "2026-06-23T17" && c.SceneKind == "deadloop_no_aipm" {
			t.Errorf("排除候选不应出 deadloop_no_aipm: %+v", c)
		}
	}
}

func TestRecordHintReAnchors(t *testing.T) {
	// 实证锚点（c0ad2534 + 01a013f3 用户原话）必须命中；普通含「查看/查」消息不得误命中
	hit := []string{
		"每次在你修改代码之前 你或许最好可以查看搜索aipm中有没有相关记录",
		"此前是可以工作的 我看到有邮箱显示的 你可以查看aipm中 有密友详情的相关记录",
		"关于邮箱和备注乱码的问题 此前也出现过 aipm中应该有提交记录",
		"关于ble此前也是重点强度公关过 你最好查看一下aipm中的有关记录",
		"好的 不过我认为你最好还是查看一下aipm中关于密友模块的设计",
		"你是搜索discussion 你应该搜索aipm中的提交 disucssion中目前还没有记录",
		"你就说aipm中又记录 但是可能你搜索错了位置 很好 继续",
		"你需要在aipm bug中好好的记录下来 前因后果",
		"查看刚刚Claude的讨论 我需要你调查研究一下",
		"另外你可以查看Claude最新的分析",
		"查看Claude的意见 他有一点分析是对的",
		"你有必要时常查看一下Claude的建议 寻求指导",
	}
	miss := []string{
		"你的修改似乎不够完善 现在ios自己扫描添加了两个密友 可以在设备中查询到已经创建了相关文件",
		"最好的办法就是查看一下直接解密出来的明文 在明文中查看邮箱是不是",
		"刚刚的linux dzsdk没有更新 现在更新了 你可以重新查看一下",
		"我只需要点击之后查看一下显示效果 并不需要完全接入整个流程",
		"我查看了微信中的添加图片的实现 也是自定义的",
		"你最好使用aipmc_vision  mcp查看图片",
		"Claude已经查看了 不过目前不着急动手 我现在先恢复以前的版本",
		"在app connect上查看了那个版本是7/24日",
	}
	for _, m := range hit {
		if !recordHintRe.MatchString(m) {
			t.Errorf("recordHintRe 漏命中（正例）: %s", m)
		}
	}
	for _, m := range miss {
		if recordHintRe.MatchString(m) {
			t.Errorf("recordHintRe 误命中（负例）: %s", m)
		}
	}
}

// ── 静态可核对 ──

func TestDetectStaticCheckMisses(t *testing.T) {
	turns := []Turn{
		// 10:44 改造后 10:50 直接真机构建（无前置 SDK 头文件核对）→ 候选（open: vs openURL: 教训）
		pqTurn("u0", "修复一下", "2026-08-20T10:40:00"),
		{Records: []Record{
			pqRec("edit", "ShareViewController.swift", "2026-08-20T10:44:00"),
			pqRec("bash", "xcodebuild -project EncryptDrive.xcodeproj -scheme EncryptDrive -configuration Debug -sdk iphoneos build", "2026-08-20T10:50:00"),
		}},
		// 10:56 用户崩溃栈（同一轮次 10 分钟内去重）
		pqTurn("u1", "-[_EXSinkLoadOperator loadItemForTypeIdentifier:completionHandler:] Bug in client", "2026-08-20T10:56:26"),
		// 11:15 才 grep SDK 头文件（在 10:56 轮次窗口外）
		{Records: []Record{
			pqRec("bash", "grep -n \"openURL:options:completionHandler\" /Applications/Xcode.app/Contents/Developer/Platforms/iPhoneOS.platform/Developer/SDKs/iPhoneOS26.5.sdk/System/Library/Frameworks/UIKit.framework/Headers/UIScene.h", "2026-08-20T11:15:34"),
			pqRec("edit", "ShareViewController.swift", "2026-08-20T11:16:00"),
		}},
		// 11:23 第二次崩溃：窗口内有 11:15 核对 → 不候选（「查了但查错 API」归 L2）
		pqTurn("u2", "-[_EXSinkLoadOperator ...] nil expectedValueClass", "2026-08-20T11:23:11"),
		// 模拟器命令不算真机轮次
		{Records: []Record{
			pqRec("bash", "xcodebuild -scheme EncryptDrive -sdk iphonesimulator build", "2026-08-20T11:30:00"),
		}},
	}
	cands := DetectStaticCheckMisses(turns, DefaultStaticCheckParams())
	if len(cands) != 1 {
		t.Fatalf("静态可核对候选 = %d, want 1（10:50 真机构建无前置核对；10:56 同轮次去重；11:23 有核对；sim 不算）", len(cands))
	}
	if cands[0].RoundKind != "device_cmd" {
		t.Errorf("候选 round_kind = %s, want device_cmd（10:50 真机构建）", cands[0].RoundKind)
	}
}

func TestStaticCheckDeviceCmd(t *testing.T) {
	turns := []Turn{
		pqTurn("u0", "部署一下", "2026-08-21T09:00:00"),
		{Records: []Record{
			pqRec("bash", "xcrun simctl install booted /tmp/app.app", "2026-08-21T09:05:00"),
		}},
	}
	cands := DetectStaticCheckMisses(turns, DefaultStaticCheckParams())
	if len(cands) != 1 {
		t.Fatalf("设备安装命令无前置核对应出候选, got %d", len(cands))
	}
	if cands[0].RoundKind != "device_cmd" {
		t.Errorf("round_kind = %s, want device_cmd", cands[0].RoundKind)
	}
}

// ── P3 计数基线：重复验证点 ──

func TestDetectRepeatedVerification(t *testing.T) {
	turns := []Turn{
		pqTurn("u0", "问题：资云集打开方式不跳转", "2026-08-19T15:00:00"),
		{Records: []Record{
			pqRec("edit", "ShareViewController.swift", "2026-08-19T15:10:00"),
			pqRec("bash", "git commit -m \"fix: SceneDelegate 转发 URL\"", "2026-08-19T15:33:05"),
			// 同轮次（无 commit、无跨夜）内两次验证请求 → 候选
			{Role: "assistant", Content: "现在请 Xcode Run 一次，然后用文件 App → 长按文件 → 共享 → 打开方式 → 资云集再测。", Tool: ToolRecord{Tool: "llm_message"}, CreatedAt: mustTs("2026-08-19T17:25:14")},
			{Role: "assistant", Content: "请再测一次：Xcode Run 最新代码", Tool: ToolRecord{Tool: "llm_message"}, CreatedAt: mustTs("2026-08-19T17:40:57")},
			pqRec("bash", "git commit -m \"fix: 移除 Share Extension\"", "2026-08-19T17:50:00"),
		}},
		// 新 episode 单次请求 → 不候选
		{Records: []Record{
			{Role: "assistant", Content: "已动手完成改造，等你真机验证。", Tool: ToolRecord{Tool: "llm_message"}, CreatedAt: mustTs("2026-08-19T18:00:00")},
		}},
	}
	cands := DetectRepeatedVerification(turns, DefaultRepeatedVerificationParams())
	if len(cands) != 1 {
		t.Fatalf("重复验证点候选 = %d, want 1", len(cands))
	}
	c := cands[0]
	if c.Count != 2 {
		t.Errorf("count = %d, want 2（17:25 + 17:40 同轮次两次真机验证请求）", c.Count)
	}
	if c.EpisodeStart.Format("15:04") != "17:25" {
		t.Errorf("episode_start = %s, want 17:25", c.EpisodeStart.Format("15:04"))
	}
}

func TestDetectRepeatedVerificationSleepSplit(t *testing.T) {
	// 跨夜两自然轮次各自重复（Claude challenge 1）：8/19 晚 2 次 + 8/20 上午 2 次 → 2 候选，
	// 不做跨天合并（合并会把「12 次」跨天失真）
	turns := []Turn{
		pqTurn("u0", "问题：资云集打开方式不跳转", "2026-08-19T15:00:00"),
		{Records: []Record{
			pqRec("bash", "git commit -m \"fix: SceneDelegate 转发 URL\"", "2026-08-19T15:33:05"),
			{Role: "assistant", Content: "现在请 Xcode Run 一次，然后打开方式 → 资云集再测。", Tool: ToolRecord{Tool: "llm_message"}, CreatedAt: mustTs("2026-08-19T17:25:14")},
			{Role: "assistant", Content: "请再测一次：Xcode Run 最新代码", Tool: ToolRecord{Tool: "llm_message"}, CreatedAt: mustTs("2026-08-19T17:40:57")},
			// 跨夜休眠：17:40 → 次日 09:05（gap ≥6h 且跨日）
			{Role: "assistant", Content: "你直接 Xcode Run 到真机测，注意区分两条链路", Tool: ToolRecord{Tool: "llm_message"}, CreatedAt: mustTs("2026-08-20T09:05:40")},
			{Role: "assistant", Content: "请你真机验证 1. 彻底划掉资云集", Tool: ToolRecord{Tool: "llm_message"}, CreatedAt: mustTs("2026-08-20T09:38:34")},
		}},
	}
	cands := DetectRepeatedVerification(turns, DefaultRepeatedVerificationParams())
	if len(cands) != 2 {
		t.Fatalf("重复验证点候选 = %d, want 2（跨夜切分两轮各 2 次）", len(cands))
	}
	got := map[string]int{}
	for _, c := range cands {
		got[c.EpisodeStart.Format("2006-01-02")] = c.Count
	}
	if got["2026-08-19"] != 2 {
		t.Errorf("8/19 轮次请求 = %d, want 2", got["2026-08-19"])
	}
	if got["2026-08-20"] != 2 {
		t.Errorf("8/20 轮次请求 = %d, want 2", got["2026-08-20"])
	}
}

// ── P3 计数基线：自建记录利用 ──

func TestDetectSelfRecordUsage(t *testing.T) {
	turns := []Turn{
		pqTurn("u0", "从第三方应用选择图片 打开方式 资云集 没有跳转", "2026-08-19T15:20:00"),
		{Records: []Record{
			pqRec("mcp_aipm_other", "aipm_record_bug", "2026-08-19T15:32:22"),
			pqRec("bash", "git add ShareViewController.swift", "2026-08-19T15:32:45"),
			pqRec("bash", "git commit -m \"fix\"", "2026-08-19T15:33:05"),
			pqRec("edit", "ShareViewController.swift", "2026-08-19T16:48:00"),
			pqRec("bash", "grep -rn \"openURL\" ShareExtension", "2026-08-19T16:49:00"),
			pqRec("bash", "xcodebuild build", "2026-08-19T16:50:00"),
			pqRec("bash", "grep -n \"shareInbox\" ContentView.swift", "2026-08-19T16:51:00"),
			pqRec("edit", "ContentView.swift", "2026-08-19T16:52:00"),
			pqRec("bash", "xcodebuild build", "2026-08-19T16:53:00"),
			pqRec("mcp_aipm_search", "aipm_search_discussions", "2026-08-19T17:29:34"),
		}},
	}
	cands := DetectSelfRecordUsage(turns, DefaultSelfRecordParams())
	if len(cands) != 1 {
		t.Fatalf("自建记录利用候选 = %d, want 1", len(cands))
	}
	c := cands[0]
	if c.Kind != "aipm_record_bug" {
		t.Errorf("kind = %s, want aipm_record_bug", c.Kind)
	}
	if c.FirstConsultAt.Format("15:04") != "17:29" {
		t.Errorf("first_consult_at = %s, want 17:29", c.FirstConsultAt.Format("15:04"))
	}
	if c.WorkRecords < 5 {
		t.Errorf("work_records = %d, want ≥5", c.WorkRecords)
	}
	if c.DelayMin <= 0 {
		t.Errorf("delay_min = %d, want >0", c.DelayMin)
	}
}

func TestDetectSelfRecordUsageConsultedImmediately(t *testing.T) {
	turns := []Turn{
		pqTurn("u0", "测试", "2026-08-19T15:20:00"),
		{Records: []Record{
			pqRec("mcp_aipm_other", "aipm_record_bug", "2026-08-19T15:32:22"),
			pqRec("mcp_aipm_get", "aipm_get_bug", "2026-08-19T15:32:40"),
			pqRec("edit", "ShareViewController.swift", "2026-08-19T15:35:00"),
		}},
	}
	cands := DetectSelfRecordUsage(turns, DefaultSelfRecordParams())
	if len(cands) != 0 {
		t.Fatalf("记录后立即检索不应出候选, got %d", len(cands))
	}
}

func TestDetectSelfRecordUsageSleepDeduct(t *testing.T) {
	// Claude challenge 2：8/18 17:54 create_task → 8/19 09:07 检索，912min 含 14h 跨夜休眠；
	// DelayMin 应扣休眠 ≈ 工作延迟（小时级），不得报 912 误导
	turns := []Turn{
		pqTurn("u0", "调查", "2026-08-18T17:00:00"),
		{Records: []Record{
			pqRec("mcp_aipm_other", "aipm_create_task", "2026-08-18T17:54:40"),
			pqRec("edit", "ShareViewController.swift", "2026-08-18T17:55:00"),
			pqRec("bash", "xcodebuild build", "2026-08-18T17:56:00"),
			pqRec("bash", "grep -n \"shareInbox\" ContentView.swift", "2026-08-18T17:57:00"),
			pqRec("edit", "ContentView.swift", "2026-08-18T17:58:00"),
			pqRec("bash", "xcodebuild build", "2026-08-18T17:59:00"),
			// 跨夜休眠（17:59 → 次日 08:00+）
			pqRec("bash", "git status --short", "2026-08-19T08:30:00"),
			pqRec("mcp_aipm_search", "aipm_search_discussions", "2026-08-19T09:07:00"),
		}},
	}
	cands := DetectSelfRecordUsage(turns, DefaultSelfRecordParams())
	if len(cands) != 1 {
		t.Fatalf("自建记录利用候选 = %d, want 1", len(cands))
	}
	c := cands[0]
	// 总跨度 912min，休眠 ≈ 8/18 17:59→8/19 08:30（12.5h=750min）→ 工作延迟 ≈ 162min
	if c.DelayMin >= 900 {
		t.Errorf("delay_min = %d, want 扣休眠后 < 900（不得含跨夜休眠夸大）", c.DelayMin)
	}
	if c.DelayMin <= 0 {
		t.Errorf("delay_min = %d, want >0（8/19 08:30 开工后 09:07 才检索，仍有工作延迟）", c.DelayMin)
	}
}
