package eval

// EVAL_PIPELINE §3.6 阶段 5：行为提取（纯代码、确定性，零 LLM）。
// 输入 Episode（阶段 1-3 产物），输出 EpisodeBehavior 结构化行为。
// 8/19 真实库核实约束：post_tool（codex/cursor 8540 行）无 exit_code，
// Failures/Retries 仅能统计 claude legacyBash；codex assistant 仅 "(turn stopped)"
// 占位，TextSignals 主要来自 claude/cursor/opencode LLM 文本。

import (
	"path/filepath"
	"strings"
)

// EpisodeBehavior 段级行为信号。
type EpisodeBehavior struct {
	ToolUsage           map[string]int // bash/edit/read/write/mcp/llm_message/unknown/其他
	CmdSemantics        map[string]int // build/test/vet/git/query/deploy/other
	Files               FilesSignal
	OutOfScopeFiles     float64          // 段外文件占比（相对 cwd，绝对路径判定）
	ExitCode            ExitCodeSignal
	Verification        VerificationSignal
	TextSignals         TextSignal
	SelfClaimWithoutProof bool // 声称测试通过但未运行测试（§3.6 自证检测）
}

type FilesSignal struct {
	Read  []string
	Write []string
}

type ExitCodeSignal struct {
	Failures     int // exit_code != 0 的记录数（仅 claude legacyBash 有数据）
	Retries      int // 同一命令失败后再次执行
	RetrySuccess int // 失败后重试成功（退出码 0）
}

type VerificationSignal struct {
	RanBuild  bool
	RanTest   bool
	RanVet    bool
	HasCommit bool
}

type TextSignal struct {
	ClaimedDone       int
	ClaimedTestPassed int
}

// ExtractBehavior 提取段内行为信号。cwd 为项目根（OutOfScopeFiles 判定用，空则跳过）。
func ExtractBehavior(ep *Episode, cwd string) EpisodeBehavior {
	b := EpisodeBehavior{
		ToolUsage:    map[string]int{},
		CmdSemantics: map[string]int{},
	}
	allFiles := map[string]bool{}
	seenFiles := map[string]bool{}
	failedCmd := "" // 当前未恢复的失败命令（重试判定）

	for _, turn := range ep.Turns {
		for _, rec := range turn.Records {
			tool := rec.Tool
			b.ToolUsage[tool.Tool]++
			if tool.Tool == "bash" {
				class := classifyCommand(tool.Command)
				b.CmdSemantics[class]++
				switch class {
				case "build":
					b.Verification.RanBuild = true
				case "test":
					b.Verification.RanTest = true
				case "vet":
					b.Verification.RanVet = true
				}
				if tool.ExitCode != nil && *tool.ExitCode != 0 {
					b.ExitCode.Failures++
					if tool.Command == failedCmd {
						b.ExitCode.Retries++
					} else {
						failedCmd = tool.Command
					}
				} else if tool.ExitCode != nil && *tool.ExitCode == 0 {
					// 重试成功 = 同一命令再次执行成功；不同命令成功只解除失败态不计成功
					if tool.Command == failedCmd {
						b.ExitCode.RetrySuccess++
					}
					failedCmd = ""
				}
			}
			for _, f := range tool.Files {
				if seenFiles[f] {
					continue
				}
				seenFiles[f] = true
				allFiles[f] = true
				switch tool.Tool {
				case "read", "llm_message":
					b.Files.Read = append(b.Files.Read, f)
				case "edit", "write":
					b.Files.Write = append(b.Files.Write, f)
				default:
					// bash/mcp/unknown 关联文件：无法确认写操作，保守归读
					b.Files.Read = append(b.Files.Read, f)
				}
			}
			if rec.Role == "assistant" && isLLMText(rec.Content, tool.Tool) {
				if claimsDone(rec.Content) {
					b.TextSignals.ClaimedDone++
				}
				if claimsTestPassed(rec.Content) {
					b.TextSignals.ClaimedTestPassed++
				}
			}
		}
	}
	b.Verification.HasCommit = len(ep.Commits) > 0
	// §3.6 自证检测：声称测试通过但段内未运行测试
	b.SelfClaimWithoutProof = b.TextSignals.ClaimedTestPassed > 0 && !b.Verification.RanTest

	if cwd != "" && len(allFiles) > 0 {
		out := 0
		for f := range allFiles {
			if filepath.IsAbs(f) && !isUnder(f, cwd) {
				out++
			}
		}
		b.OutOfScopeFiles = float64(out) / float64(len(allFiles))
	}
	return b
}

// classifyCommand bash 命令语义分类（前缀/关键词匹配，顺序敏感）。
func classifyCommand(cmd string) string {
	switch {
	case matchAny(cmd, "go vet"):
		return "vet"
	case matchAny(cmd, "go test", "pytest", "npm test", "swift test", "xcodebuild test", "make test"):
		return "test"
	case matchAny(cmd, "go build", "go install", "npm run build", "npm build", "swift build", "xcodebuild build", "make ", "cmake --build"):
		return "build"
	case matchAny(cmd, "git "):
		return "git"
	case matchAny(cmd, "sqlite3", "SELECT ", "psql", "mysql", "rg ", "grep ", "find ", "cat ", "head ", "wc ", "ls "):
		return "query"
	case matchAny(cmd, "docker", "kubectl", "scp", "rsync", "terraform", "aws ", "serverless"):
		return "deploy"
	default:
		return "other"
	}
}

func matchAny(s string, keys ...string) bool {
	for _, k := range keys {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// isLLMText 判定 assistant 记录是否为 LLM 回复文本（排除工具前缀行与占位）。
func isLLMText(content, tool string) bool {
	if tool == "llm_message" {
		return true // cursor/opencode 已归一
	}
	switch tool {
	case "bash", "edit", "read", "write", "mcp":
		return false
	}
	c := strings.TrimSpace(content)
	if c == "" {
		return false
	}
	if strings.HasPrefix(c, "(turn stopped)") {
		return false
	}
	// claude 工具行以 emoji 前缀标记（metadata 可能为空 → unknown）
	for _, p := range []string{"🔧", "🛠", "👁", "🔍", "📡"} {
		if strings.HasPrefix(c, p) {
			return false
		}
	}
	return true
}

// claimsDone 文本声称完成（排除工具行后统计）。
func claimsDone(s string) bool {
	return matchAny(s, "已完成", "完成了", "完成啦", "搞定", "做好了", "实现完成", "all done")
}

// claimsTestPassed 文本声称测试通过。
func claimsTestPassed(s string) bool {
	return matchAny(s, "测试通过", "测试全过", "全部通过", "tests pass", "test passed", "tests passed", "✅ 通过")
}

// isUnder 判定 path 是否在 root 之下（含 root 本身）。
func isUnder(path, root string) bool {
	return path == root || strings.HasPrefix(path, strings.TrimSuffix(root, "/")+"/")
}
