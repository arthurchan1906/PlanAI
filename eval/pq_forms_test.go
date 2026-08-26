package eval

// P1a 形态 5-10 L1 扫描器测试（PROCESS_QUALITY_SPEC §2.1 形态分类学）。
// 正样本样式取自实证：019ff89b（形态 7/8 LocalVaultStore×42/9h）、c0ad2534（形态 5/9 跨夜
// 休眠 + 死循环时段）、8/14 019ffdce（形态 10 8 处日志改动全撤销）。

import (
	"strings"
	"testing"
	"time"
)

func ts(s string) time.Time {
	t, err := time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		panic(err)
	}
	return t
}

// ── 形态 5：静默停滞 ──

func TestDetectStagnationPositive(t *testing.T) {
	// 用户 10:00 指令 → 14:30 才有首个 edit（无休眠、中间无 user 消息）→ 候选
	tr := pqTurn("u1", "修 bug", "2026-08-26T10:00:00")
	tr.Records = append(tr.Records, pqRec("bash", "go build ./...", "2026-08-26T10:05:00")) // 无对象非产出
	tr.Records = append(tr.Records, pqRec("edit", "", "2026-08-26T14:30:00"))

	cands := DetectStagnation([]Turn{tr}, DefaultStagnationParams())
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
	c := cands[0]
	if !c.FromUser {
		t.Errorf("from_user = false, want true（起点是用户消息）")
	}
	if c.GapMin < 260 || c.GapMin > 280 {
		t.Errorf("gap_min = %d, want ~270", c.GapMin)
	}
	if c.Production != "edit" {
		t.Errorf("production = %s, want edit", c.Production)
	}
	if c.SleepMin != 0 {
		t.Errorf("sleep_min = %d, want 0（同日无休眠）", c.SleepMin)
	}
}

func TestDetectStagnationSleepExcluded(t *testing.T) {
	// 跨夜大间隔（c0ad2534 9h+ 负样本口径）：23:00 edit → 次日 10:00 edit，休眠扣除后不标记
	tr := pqTurn("u1", "继续", "2026-08-25T22:00:00")
	tr.Records = append(tr.Records, pqRec("edit", "", "2026-08-25T23:00:00"))
	tr2 := pqTurn("", "", "2026-08-26T10:00:00")
	tr2.Records = append(tr2.Records, pqRec("edit", "", "2026-08-26T10:00:00"))

	cands := DetectStagnation([]Turn{tr, tr2}, DefaultStagnationParams())
	if len(cands) != 0 {
		t.Fatalf("candidates = %d, want 0（跨夜 9h+ 为休眠负样本，§2.1）", len(cands))
	}
}

func TestDetectStagnationWaitingUserExcluded(t *testing.T) {
	// 11:00 产出 → 用户 16:00 重新介入 → 16:30 产出：等待用户段不计停滞
	tr := pqTurn("u1", "继续", "2026-08-26T10:00:00")
	tr.Records = append(tr.Records, pqRec("edit", "", "2026-08-26T11:00:00"))
	tr2 := pqTurn("u2", "你看下结果", "2026-08-26T16:00:00")
	tr2.Records = append(tr2.Records, pqRec("edit", "", "2026-08-26T16:30:00"))

	cands := DetectStagnation([]Turn{tr, tr2}, DefaultStagnationParams())
	if len(cands) != 0 {
		t.Fatalf("candidates = %d, want 0（等待用户重介入不算停滞）", len(cands))
	}
}

// ── 形态 6：频繁换方案 ──

func readRecAt(file, s string) Record {
	r := pqRec("read", "", s)
	r.Tool.Files = []string{file}
	return r
}

func TestDetectDirectionShiftsPositive(t *testing.T) {
	// 6 对象各读一次后反复横跳 a/b，新对象占比低 → 候选（019ff89b 打转样式）
	tr := pqTurn("u1", "排查", "2026-08-26T10:00:00")
	base := ts("2026-08-26T10:00:00")
	seq := []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go"}
	for i := 0; i < 26; i++ {
		obj := seq[i%6]
		if i >= 6 {
			obj = []string{"a.go", "b.go"}[i%2]
		}
		tr.Records = append(tr.Records, readRecAt(obj, base.Add(time.Duration(i)*time.Minute).Format("2006-01-02T15:04:05")))
	}

	cands := DetectDirectionShifts([]Turn{tr}, DefaultDirectionShiftParams())
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
	if cands[0].Switches < 5 {
		t.Errorf("switches = %d, want ≥5", cands[0].Switches)
	}
	if cands[0].NewRatio >= 0.35 {
		t.Errorf("new_ratio = %.2f, want <0.35", cands[0].NewRatio)
	}
}

func TestDetectDirectionShiftsNegative(t *testing.T) {
	// 健康探索：12 个不同对象各读一次 → 新对象占比高，不标记
	tr := pqTurn("u1", "排查", "2026-08-26T10:00:00")
	base := ts("2026-08-26T10:00:00")
	for i := 0; i < 12; i++ {
		tr.Records = append(tr.Records, readRecAt(string(rune('a'+i))+".go",
			base.Add(time.Duration(i)*time.Minute).Format("2006-01-02T15:04:05")))
	}
	cands := DetectDirectionShifts([]Turn{tr}, DefaultDirectionShiftParams())
	if len(cands) != 0 {
		t.Fatalf("candidates = %d, want 0（扩展健康）", len(cands))
	}
}

// ── 形态 7：重复调查 ──

func TestDetectRepeatInvestigationPositive(t *testing.T) {
	// 019ff89b 样式：LocalVaultStore.swift 重复读 10 次 + 扩展率低 + 无 edit/commit
	tr := pqTurn("u1", "查问题", "2026-08-26T10:00:00")
	base := ts("2026-08-26T10:00:00")
	for i := 0; i < 10; i++ {
		tr.Records = append(tr.Records, readRecAt("LocalVaultStore.swift",
			base.Add(time.Duration(i)*30*time.Minute).Format("2006-01-02T15:04:05")))
	}
	tr.Records = append(tr.Records, readRecAt("VaultItem.swift", "2026-08-26T15:05:00"))

	cands := DetectRepeatInvestigation([]Turn{tr}, DefaultRepeatInvestigationParams())
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
	c := cands[0]
	if c.Object != "LocalVaultStore.swift" || c.Reads != 10 {
		t.Errorf("object/reads = %s/%d, want LocalVaultStore.swift/10", c.Object, c.Reads)
	}
	if c.NoProdSpan < 300 {
		t.Errorf("no_prod_span = %d, want ≥300（首读 10:00 → 末读+30min）", c.NoProdSpan)
	}
}

func TestDetectRepeatInvestigationEditExcluded(t *testing.T) {
	// 同一对象重复读但中途有 edit（有产出进展）→ 不标记（正当排查）
	tr := pqTurn("u1", "查问题", "2026-08-26T10:00:00")
	base := ts("2026-08-26T10:00:00")
	for i := 0; i < 10; i++ {
		tr.Records = append(tr.Records, readRecAt("LocalVaultStore.swift",
			base.Add(time.Duration(i)*30*time.Minute).Format("2006-01-02T15:04:05")))
	}
	tr.Records = append(tr.Records, pqRec("edit", "", "2026-08-26T12:00:00"))

	cands := DetectRepeatInvestigation([]Turn{tr}, DefaultRepeatInvestigationParams())
	if len(cands) != 0 {
		t.Fatalf("candidates = %d, want 0（时限内有 edit）", len(cands))
	}
}

// ── 形态 8：单点死磕 ──

func TestDetectSingleFocusPositive(t *testing.T) {
	// 20 次访问 18 次同一文件、扩展率低、转向少 → 候选（019ff89b 同文件域实证）
	tr := pqTurn("u1", "排查", "2026-08-26T10:00:00")
	base := ts("2026-08-26T10:00:00")
	for i := 0; i < 18; i++ {
		tr.Records = append(tr.Records, readRecAt("LocalVaultStore.swift",
			base.Add(time.Duration(i)*10*time.Minute).Format("2006-01-02T15:04:05")))
	}
	tr.Records = append(tr.Records, readRecAt("VaultItem.swift", "2026-08-26T13:10:00"))
	tr.Records = append(tr.Records, readRecAt("VaultDetail.swift", "2026-08-26T13:20:00"))

	cands := DetectSingleFocus([]Turn{tr}, DefaultSingleFocusParams())
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
	c := cands[0]
	if c.TopObject != "LocalVaultStore.swift" || c.TopCount != 18 {
		t.Errorf("top = %s×%d, want LocalVaultStore.swift×18", c.TopObject, c.TopCount)
	}
	if c.TopShare < 0.8 {
		t.Errorf("top_share = %.2f, want ≥0.8", c.TopShare)
	}
}

func TestDetectSingleFocusNegative(t *testing.T) {
	// 扩展健康（12 对象各读一次）→ 不标记
	tr := pqTurn("u1", "排查", "2026-08-26T10:00:00")
	base := ts("2026-08-26T10:00:00")
	for i := 0; i < 12; i++ {
		tr.Records = append(tr.Records, readRecAt(string(rune('a'+i))+".go",
			base.Add(time.Duration(i)*time.Minute).Format("2006-01-02T15:04:05")))
	}
	cands := DetectSingleFocus([]Turn{tr}, DefaultSingleFocusParams())
	if len(cands) != 0 {
		t.Fatalf("candidates = %d, want 0（扩展健康）", len(cands))
	}
}

// ── 形态 9：验证循环 ──

func TestDetectVerifyLoopsLegacy(t *testing.T) {
	// legacy 强信号：exit_code != 0 → 同命令重试 → 中间无 edit/分析 → 候选
	tr := pqTurn("u1", "编译", "2026-08-26T10:00:00")
	fail := pqRec("bash", "go build ./...", "2026-08-26T10:01:00")
	ec := 1
	fail.Tool.ExitCode = &ec
	tr.Records = append(tr.Records, fail)
	tr.Records = append(tr.Records, pqRec("bash", "go build ./...", "2026-08-26T10:02:00"))

	cands := DetectVerifyLoops([]Turn{tr}, DefaultVerifyLoopParams())
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
	if cands[0].FailSig != "exit_code" {
		t.Errorf("fail_sig = %s, want exit_code", cands[0].FailSig)
	}
	if !strings.Contains(cands[0].Command, "go build") {
		t.Errorf("command = %s, want go build", cands[0].Command)
	}
}

func TestDetectVerifyLoopsAnalysisExcluded(t *testing.T) {
	// 失败 → 看日志 → 重试：中间有日志分析 → 不标记（有依据非盲试）
	tr := pqTurn("u1", "编译", "2026-08-26T10:00:00")
	fail := pqRec("bash", "go build ./...", "2026-08-26T10:01:00")
	ec := 1
	fail.Tool.ExitCode = &ec
	tr.Records = append(tr.Records, fail)
	tr.Records = append(tr.Records, pqRec("bash", "tail -100 Logs/build.log", "2026-08-26T10:02:00"))
	tr.Records = append(tr.Records, pqRec("bash", "go build ./...", "2026-08-26T10:03:00"))

	cands := DetectVerifyLoops([]Turn{tr}, DefaultVerifyLoopParams())
	if len(cands) != 0 {
		t.Fatalf("candidates = %d, want 0（中间有日志分析）", len(cands))
	}
}

func TestDetectVerifyLoopsHookWeak(t *testing.T) {
	// hook 弱信号：tool_response 文本错误词（§2.1：L1 候选 + L2 确认）
	tr := pqTurn("u1", "编译", "2026-08-26T10:00:00")
	fail := pqRec("bash", "swift build", "2026-08-26T10:01:00")
	fail.Content = "Build error: cannot find type 'Foo'"
	tr.Records = append(tr.Records, fail)
	tr.Records = append(tr.Records, pqRec("bash", "swift build", "2026-08-26T10:02:00"))

	cands := DetectVerifyLoops([]Turn{tr}, DefaultVerifyLoopParams())
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1（弱信号也应 L1 候选）", len(cands))
	}
	if cands[0].FailSig != "error_word" {
		t.Errorf("fail_sig = %s, want error_word", cands[0].FailSig)
	}
}

// ── 形态 10：伪进展 ──

func applyPatch(file, patch, s string) Record {
	r := pqRec("bash", "apply_patch <<'PATCH'\n"+patch+"\nPATCH", s)
	r.Tool.Files = []string{file}
	return r
}

func TestDetectFakeProgressPositive(t *testing.T) {
	// 8/14 019ffdce 样式：apply_patch 打点 println 添加后又撤销、无根因/commit → 候选
	tr := pqTurn("u1", "排查", "2026-08-26T10:00:00")
	tr.Records = append(tr.Records, applyPatch("eval/pq.go", "*** Update File: eval/pq.go\n+\tprintln(\"debug: step1\")", "2026-08-26T10:01:00"))
	tr.Records = append(tr.Records, applyPatch("eval/pq.go", "*** Update File: eval/pq.go\n-\tprintln(\"debug: step1\")", "2026-08-26T10:30:00"))

	cands := DetectFakeProgress([]Turn{tr}, DefaultFakeProgressParams())
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
	c := cands[0]
	if c.File != "eval/pq.go" || c.Edits != 2 {
		t.Errorf("file/edits = %s/%d, want eval/pq.go/2", c.File, c.Edits)
	}
	if !c.NoRootCause || !c.NoCommit {
		t.Errorf("no_root_cause/no_commit = %v/%v, want true/true", c.NoRootCause, c.NoCommit)
	}
}

func TestDetectFakeProgressRootCauseExcluded(t *testing.T) {
	// 加日志 → 根因定位 → 撤销：正当排查，不标记（判别式：有根因定位）
	tr := pqTurn("u1", "排查", "2026-08-26T10:00:00")
	tr.Records = append(tr.Records, applyPatch("eval/pq.go", "*** Update File: eval/pq.go\n+\tprintln(\"debug: step1\")", "2026-08-26T10:01:00"))
	root := pqRec("unknown", "", "2026-08-26T10:20:00")
	root.Content = "根因已定位：APDU_MAX_DATA_UNIT=192 分块"
	tr.Records = append(tr.Records, root)
	tr.Records = append(tr.Records, applyPatch("eval/pq.go", "*** Update File: eval/pq.go\n-\tprintln(\"debug: step1\")", "2026-08-26T10:30:00"))

	cands := DetectFakeProgress([]Turn{tr}, DefaultFakeProgressParams())
	if len(cands) != 0 {
		t.Fatalf("candidates = %d, want 0（有根因定位 = 正当排查）", len(cands))
	}
}

func TestDetectFakeProgressCommitExcluded(t *testing.T) {
	// 撤销打点后 30min 内 commit（正当收尾）→ 不标记
	tr := pqTurn("u1", "排查", "2026-08-26T10:00:00")
	tr.Records = append(tr.Records, applyPatch("eval/pq.go", "*** Update File: eval/pq.go\n+\tprintln(\"debug: step1\")", "2026-08-26T10:01:00"))
	tr.Records = append(tr.Records, applyPatch("eval/pq.go", "*** Update File: eval/pq.go\n-\tprintln(\"debug: step1\")", "2026-08-26T10:30:00"))
	tr.Records = append(tr.Records, pqRec("bash", "git commit -m 'fix: 根因修复'", "2026-08-26T10:45:00"))

	cands := DetectFakeProgress([]Turn{tr}, DefaultFakeProgressParams())
	if len(cands) != 0 {
		t.Fatalf("candidates = %d, want 0（撤销后 30min 内 commit = 正当收尾）", len(cands))
	}
}

// ── 聚合接入 ──

func TestBuildProcessReportFormsPopulated(t *testing.T) {
	// ProcessReport 接入冒烟：验证循环 + 静默停滞 候选从聚合入口可见
	d := pqDB(t)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('u1','s','user','codex-cli','修 bug','2026-08-26T10:00:00','')`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('r1','s','assistant','codex-cli','','2026-08-26T10:01:00','{"type":"bash","command":"go build ./...","exit_code":1}')`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('r2','s','assistant','codex-cli','','2026-08-26T10:02:00','{"type":"bash","command":"go build ./...","exit_code":1}')`)
	mustExec(t, d, `INSERT INTO discussion_log VALUES ('r3','s','assistant','codex-cli','','2026-08-26T14:30:00','{"type":"edit","file_path":"a.go"}')`)

	rep, err := BuildProcessReport(d, "s", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.VerifyLoops) != 1 {
		t.Errorf("verify_loops = %d, want 1（失败→同命令重试）", len(rep.VerifyLoops))
	}
	if len(rep.Stagnation) != 1 {
		t.Errorf("stagnation = %d, want 1（用户 10:00 后 14:30 才有 edit）", len(rep.Stagnation))
	}
	if rep.Stagnation[0].FromUser != true {
		t.Errorf("stagnation[0].from_user = %v, want true", rep.Stagnation[0].FromUser)
	}
}
