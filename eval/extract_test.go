package eval

// EVAL_PIPELINE S3 测试：阶段 5 行为提取（命令语义/读写归类/自证信号/重试）。

import "testing"

func epWith(recs ...Record) *Episode {
	t := Turn{Records: recs}
	return &Episode{Turns: []*Turn{&t}}
}

func rec(tool, cmd, content string, exit *int, files ...string) Record {
	return Record{Role: "assistant", Content: content, Tool: ToolRecord{Tool: tool, Command: cmd, ExitCode: exit, Files: files}}
}

func ip(v int) *int { return &v }

func TestClassifyCommand(t *testing.T) {
	cases := map[string]string{
		"go test ./eval/":     "test",
		"go vet ./...":        "vet",
		"go build ./...":      "build",
		"npm run build":       "build",
		"git push origin main": "git",
		"git status":          "git",
		"sqlite3 pmai.db SELECT 1": "query",
		"rg -n foo":           "query",
		"docker push x":       "deploy",
		"echo hello":          "other",
	}
	for cmd, want := range cases {
		if got := classifyCommand(cmd); got != want {
			t.Errorf("classifyCommand(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestExtractBehavior(t *testing.T) {
	ep := epWith(
		rec("bash", "go test ./...", "", nil),
		rec("bash", "git status", "", ip(1)),
		rec("bash", "git status", "", ip(1)), // 同命令重试
		rec("bash", "git status", "", ip(0)), // 重试成功
		rec("bash", "go vet ./...", "", nil),
		rec("edit", "", "改文件", nil, "/repo/a.go"),
		rec("read", "", "读文件", nil, "/repo/b.go"),
		rec("bash", "", "grep 关联", nil, "/repo/c.go"),
		rec("unknown", "", "实现完成，测试通过 ✅", nil),
		rec("unknown", "", "🔧 grep foo", nil), // 工具前缀行不计
		rec("unknown", "", "(turn stopped)", nil),
	)
	ep.Commits = []string{"c1"}

	b := ExtractBehavior(ep, "/repo")
	if b.ToolUsage["bash"] != 6 || b.ToolUsage["edit"] != 1 || b.ToolUsage["read"] != 1 {
		t.Errorf("ToolUsage = %v", b.ToolUsage)
	}
	if b.CmdSemantics["test"] != 1 || b.CmdSemantics["vet"] != 1 || b.CmdSemantics["git"] != 3 {
		t.Errorf("CmdSemantics = %v", b.CmdSemantics)
	}
	if len(b.Files.Write) != 1 || b.Files.Write[0] != "/repo/a.go" {
		t.Errorf("Write = %v, want [/repo/a.go]", b.Files.Write)
	}
	if len(b.Files.Read) != 2 { // b.go + c.go（bash 关联保守归读）
		t.Errorf("Read = %v, want 2", b.Files.Read)
	}
	if b.ExitCode.Failures != 2 || b.ExitCode.Retries != 1 || b.ExitCode.RetrySuccess != 1 {
		t.Errorf("ExitCode = %+v", b.ExitCode)
	}
	if !b.Verification.RanTest || !b.Verification.RanVet || !b.Verification.HasCommit || b.Verification.RanBuild {
		t.Errorf("Verification = %+v", b.Verification)
	}
	if b.TextSignals.ClaimedTestPassed != 1 {
		t.Errorf("ClaimedTestPassed = %d, want 1", b.TextSignals.ClaimedTestPassed)
	}
	if b.TextSignals.ClaimedDone != 1 {
		t.Errorf("ClaimedDone = %d, want 1", b.TextSignals.ClaimedDone)
	}
	if b.OutOfScopeFiles != 0 {
		t.Errorf("OutOfScopeFiles = %v, want 0（全在 /repo 内）", b.OutOfScopeFiles)
	}
}

func TestExtractOutOfScope(t *testing.T) {
	ep := epWith(
		rec("edit", "", "", nil, "/repo/a.go"),
		rec("read", "", "", nil, "/tmp/x.go"),
		rec("edit", "", "", nil, "rel.go"), // 相对路径视为段内
	)
	b := ExtractBehavior(ep, "/repo")
	if b.OutOfScopeFiles != 1.0/3.0 {
		t.Errorf("OutOfScopeFiles = %v, want 1/3", b.OutOfScopeFiles)
	}
}

func TestIsLLMText(t *testing.T) {
	if !isLLMText("实现完成", "unknown") {
		t.Error("普通文本应计入")
	}
	if !isLLMText("通过aipmc了解当前项目", "llm_message") {
		t.Error("llm_message 应计入")
	}
	if isLLMText("🔧 grep foo", "unknown") {
		t.Error("🔧 工具前缀行不应计入")
	}
	if isLLMText("(turn stopped)", "unknown") {
		t.Error("(turn stopped) 不应计入")
	}
	if isLLMText("ls -la", "bash") {
		t.Error("bash 记录不应计入文本信号")
	}
}
