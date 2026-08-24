package eval

// P0a1a T5 死循环候选（PROCESS_QUALITY_SPEC §2.1 重复停滞，口径冻结）。
// 组合信号：build 密集 + 自发检索 <2 + 中间无 edit/commit/根因定位信号。
// 排除规则（v1.3 补，11h 教训）：修复阶段 build 密集 + 修改是正常的 → 有 edit/commit 信号即排除。
// build 噪声防护（v1.2）：排除 DerivedData/.*/Build/、build\d 版本号、构建日志读取（Logs/Build）；
//   只计真正构建命令（xcodebuild build/swift build/go build 等动词形式）。

import (
	"regexp"
	"strings"
	"time"
)

// DeadloopParams 死循环候选参数（冻结口径初值，P0a1a 用 §4.1 实证校准）。
type DeadloopParams struct {
	WindowMin    int // 小时窗口（默认 60 分钟）
	BuildMin     int // build 密集阈值（默认 10，§4.1 11h 单信号误报实证）
	SpontRetrMax int // 窗口内自发检索上限（默认 2，SPEC「≈0」阈值 = <2）
}

// DefaultDeadloopParams 返回冻结口径默认参数。
func DefaultDeadloopParams() DeadloopParams {
	return DeadloopParams{WindowMin: 60, BuildMin: 10, SpontRetrMax: 2}
}

// DeadloopCandidate 死循环候选时段（Excluded=true 为 near-miss：满足 build+自发条件但被排除规则命中）。
// Fails/UserMsgs 为组合信号记录项（§2.1：build 密集 + 失败 + 用户高峰 + 自发检索<2），
// 阈值未冻结前如实记录供校准（§9.6 数据反馈）。
type DeadloopCandidate struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Builds    int       `json:"builds"`
	Fails     int       `json:"fails"`     // 失败命令数（ExitCode != 0）
	UserMsgs  int       `json:"user_msgs"` // 桶内 user 消息数（用户高峰信号）
	SpontRetr int       `json:"spont_retr"`
	Excluded  bool      `json:"excluded"`
	Reason    string    `json:"reason,omitempty"`
}

// rootCauseRe 根因定位信号（§2.1 排除规则，11h 教训）：agent 文本声明根因。
var rootCauseRe = regexp.MustCompile(`根因|已定位|原因已确认`)

var buildNoisePathRe = regexp.MustCompile(`(?i)DerivedData/.*/Build/|/Logs/Build|build[0-9]`)

// isRealBuild 真实构建命令（噪声防护后，SPEC §2.1 v1.2）。
func isRealBuild(cmd string) bool {
	if buildNoisePathRe.MatchString(cmd) {
		return false
	}
	lower := strings.ToLower(cmd)
	for _, k := range []string{"xcodebuild build", "swift build", "go build", "go install", "npm run build", "cmake --build", "make "} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

// isBuildDense 构建密集信号（§4.1 实证校准，8/24 数据反馈）：真实构建命令 +
// 构建产物检查（objdump/nm 反汇编、列举/拷贝构建产物）。三类噪声仍排除
// （DerivedData/.*/Build/、build\d 版本号、Logs/Build 读取）。
func isBuildDense(cmd string) bool {
	if buildNoisePathRe.MatchString(cmd) {
		return false
	}
	lower := strings.ToLower(cmd)
	if isRealBuild(cmd) {
		return true
	}
	if strings.Contains(lower, "objdump") || strings.Contains(lower, "llvm-nm") {
		return true
	}
	if strings.Contains(lower, "build-ios") || strings.Contains(lower, "/build/") {
		return true
	}
	return false
}

// FindDeadloops 按对齐整点小时桶扫描死循环候选（与 §4.1 小时表粒度一致）。
// spontaneous 为自发检索时间点（CountRetrieval 同源）；commits 为段内 commit 时间点（T2 输出）。
// 组合信号（§2.1 冻结）：build 密集 + 自发检索 <2 + 桶内无 edit/commit 信号。
func FindDeadloops(turns []Turn, spontaneous, commits []time.Time, p DeadloopParams) []DeadloopCandidate {
	if p.WindowMin <= 0 {
		p.WindowMin = 60
	}
	if p.BuildMin <= 0 {
		p.BuildMin = 10
	}
	if p.SpontRetrMax <= 0 {
		p.SpontRetrMax = 2
	}

	buckets := map[time.Time]*DeadloopCandidate{}
	add := func(ts time.Time, kind string) {
		h := ts.Truncate(time.Hour)
		b := buckets[h]
		if b == nil {
			b = &DeadloopCandidate{Start: h, End: h.Add(time.Duration(p.WindowMin) * time.Minute)}
			buckets[h] = b
		}
		b.End = h.Add(time.Duration(p.WindowMin) * time.Minute)
		switch kind {
		case "build":
			b.Builds++
		case "fail":
			b.Fails++
		case "user":
			b.UserMsgs++
		case "spont":
			b.SpontRetr++
		case "edit", "commit", "rootcause":
			if !b.Excluded {
				b.Excluded = true
				switch kind {
				case "edit":
					b.Reason = "桶内有 edit/write 信号（修复/进展）"
				case "commit":
					b.Reason = "桶内有 commit 信号（进展锚点）"
				case "rootcause":
					b.Reason = "桶内有根因定位文本信号"
				}
			}
		}
	}

	for i := range turns {
		if turns[i].UserMsg != "" {
			add(turns[i].Start, "user")
		}
		for j := range turns[i].Records {
			rec := turns[i].Records[j]
			switch {
			case rec.Tool.Tool == "bash" && isBuildDense(rec.Tool.Command):
				add(rec.CreatedAt, "build")
				if rec.Tool.ExitCode != nil && *rec.Tool.ExitCode != 0 {
					add(rec.CreatedAt, "fail")
				}
			case rec.Tool.Tool == "edit" || rec.Tool.Tool == "write":
				add(rec.CreatedAt, "edit")
			}
			if rec.Role == "assistant" && rootCauseRe.MatchString(rec.Content) {
				add(rec.CreatedAt, "rootcause")
			}
		}
	}
	for _, ts := range spontaneous {
		add(ts, "spont")
	}
	for _, ts := range commits {
		add(ts, "commit")
	}

	var cands []DeadloopCandidate
	for _, b := range buckets {
		if b.Builds >= p.BuildMin && b.SpontRetr < p.SpontRetrMax {
			cands = append(cands, *b)
		}
	}
	sortCands(cands)
	return cands
}

func sortCands(cands []DeadloopCandidate) {
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0 && cands[j].Start.Before(cands[j-1].Start); j-- {
			cands[j], cands[j-1] = cands[j-1], cands[j]
		}
	}
}
