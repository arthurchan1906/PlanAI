package eval

// P0a2 静态可核对（PROCESS_QUALITY_SPEC §2.1 静态可核对项 + §0「真机轮次是预算」）。
// 定义：上真机前的静态检查清单——字符串 selector 对 SDK 头文件核对（open: vs openURL: 教训）、
// API 参数类型查头文件签名（UIScene options 教训）、废弃 API 检查；命中清单项的错误不烧真机轮次。
// 01a013f3 实证：10:52 改造后直接「等你真机验证」→ 10:56 用户贴崩溃栈（BUG IN CLIENT OF UIKIT）
// → 11:15 才 grep iPhoneOS SDK 头文件（openURL:options:completionHandler:）→ 11:16 修正 selector。
// L1 检测：真机轮次标记（设备部署/构建命令、用户崩溃/安装错误消息）前窗口内无 SDK 头文件/API
// 签名静态核对命令 → 候选「静态可核对未先查」。方向性报告，高召回低精度，「查了但查错 API」
// 类不足归 L2 语义确认（P1）。

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// StaticCheckParams 静态可核对参数。
type StaticCheckParams struct {
	WindowMin int // 真机轮次前静态核对窗口（默认 30 分钟）
}

// DefaultStaticCheckParams 默认参数。
func DefaultStaticCheckParams() StaticCheckParams {
	return StaticCheckParams{WindowMin: 30}
}

// StaticCheckCandidate 静态可核对候选：真机轮次前窗口内无 SDK 头文件核对。
type StaticCheckCandidate struct {
	RoundAt         time.Time `json:"round_at"`
	RoundKind       string    `json:"round_kind"` // device_cmd / device_error_msg / device_test_request
	RoundSnippet    string    `json:"round_snippet,omitempty"`
	LastStaticCheck time.Time `json:"last_static_check,omitempty"` // 窗口内最后一次静态核对（零值 = 无）
	WindowMin       int       `json:"window_min"`
	Note            string    `json:"note,omitempty"`
}

// isDeviceRoundCmd 真机/部署轮次命令：真机构建（xcodebuild -sdk iphoneos / build-ios-device）与
// 安装/启动（simctl install/launch、ios-deploy、ideviceinstaller）。
func isDeviceRoundCmd(cmd string) bool {
	lower := strings.ToLower(strings.TrimSpace(cmd))
	if strings.Contains(lower, "xcodebuild") && strings.Contains(lower, "-sdk iphoneos") {
		return true
	}
	if strings.Contains(lower, "build-ios-device") {
		return true
	}
	for _, k := range []string{"xcrun simctl install", "xcrun simctl launch", "ios-deploy", "ideviceinstaller"} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

// isStaticCheckCmd SDK 头文件/API 签名静态核对（静态可核对清单项）：
// grep/rg 命令参数含 SDK/Headers/.framework 路径（selector 对头文件核对、API 签名查头文件）。
func isStaticCheckCmd(cmd string) bool {
	lower := strings.ToLower(strings.TrimSpace(cmd))
	if !strings.Contains(lower, "grep") && !strings.Contains(lower, "rg ") && !strings.HasPrefix(lower, "rg") {
		return false
	}
	for _, k := range []string{".sdk", "/sdk", "headers", ".framework/"} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

// deviceErrorRe 用户崩溃/设备安装错误消息（系统错误信号源，真机轮次已烧的实证）。
var deviceErrorRe = regexp.MustCompile(`(?i)Bug in client|CoreDeviceError|EXSinkLoadOperator|libsystem_kernel|unable to install|failed to install|crash`)

// roundMarker 真机轮次标记（时间有序扫描用）。
// 轮次标记只取命令级（设备部署/构建命令）与信号级（用户崩溃/安装错误消息）——assistant
// 文本里的「真机/测试」字样噪声大（状态汇报也常含），不作为轮次标记（10:52「等你真机验证」
// 由其前 10:50 的设备构建命令覆盖）。
type roundMarker struct {
	at      time.Time
	kind    string
	snippet string
}

// DetectStaticCheckMisses 静态可核对检测：
// 扫描时间有序记录；每次真机轮次标记（设备命令/崩溃消息/真机测试请求）时，检查窗口
// [round - WindowMin, round] 内是否有静态核对命令；无 → 候选。
// 轮次标记 10 分钟内相邻去重（同一轮次的多形态标记只报一次）。
func DetectStaticCheckMisses(turns []Turn, p StaticCheckParams) []StaticCheckCandidate {
	if p.WindowMin <= 0 {
		p.WindowMin = 30
	}
	var all []Record
	for i := range turns {
		for j := range turns[i].Records {
			all = append(all, turns[i].Records[j])
		}
	}
	// 时间有序事件流：user 消息（崩溃/错误）+ assistant 记录（设备命令/测试请求/静态核对）
	type ev struct {
		ts   time.Time
		user string // 非空 = user 消息；空 = assistant 记录
		rec  *Record
	}
	var evs []ev
	for i := range turns {
		t := &turns[i]
		if strings.TrimSpace(t.UserMsg) != "" {
			evs = append(evs, ev{ts: t.Start, user: t.UserMsg})
		}
		for j := range t.Records {
			evs = append(evs, ev{ts: t.Records[j].CreatedAt, rec: &t.Records[j]})
		}
	}
	var staticTimes []time.Time
	var out []StaticCheckCandidate
	var lastRoundAt time.Time
	for i := range evs {
		e := &evs[i]
		var marker *roundMarker
		switch {
		case e.user != "":
			if deviceErrorRe.MatchString(e.user) {
				marker = &roundMarker{at: e.ts, kind: "device_error_msg", snippet: snippetOf(e.user)}
			}
		case e.rec.Tool.Tool == "bash" && isDeviceRoundCmd(e.rec.Tool.Command):
			marker = &roundMarker{at: e.ts, kind: "device_cmd", snippet: snippetOf(e.rec.Tool.Command)}
		}
		if e.rec != nil && isStaticCheckCmd(e.rec.Tool.Command) {
			staticTimes = append(staticTimes, e.ts)
		}
		if marker == nil {
			continue
		}
		// 相邻轮次（10 分钟内）去重：同一轮次只报一次
		if !lastRoundAt.IsZero() && marker.at.Sub(lastRoundAt) <= 10*time.Minute {
			continue
		}
		lastRoundAt = marker.at
		last := lastStaticInWindow(staticTimes, marker.at, p.WindowMin)
		if !last.IsZero() {
			continue // 窗口内有静态核对 → 非候选（「查了但不足」归 L2）
		}
		out = append(out, StaticCheckCandidate{
			RoundAt: marker.at, RoundKind: marker.kind, RoundSnippet: marker.snippet,
			WindowMin: p.WindowMin,
			Note:      "静态可核对未先查：真机轮次前窗口内无 SDK 头文件/API 签名核对（open: vs openURL: 教训）",
		})
	}
	return out
}

// lastStaticInWindow 窗口内最后一次静态核对时间（无则零值）。
func lastStaticInWindow(staticTimes []time.Time, round time.Time, windowMin int) time.Time {
	from := round.Add(-time.Duration(windowMin) * time.Minute)
	var last time.Time
	for i := range staticTimes {
		t := staticTimes[i]
		if !t.Before(from) && !t.After(round) {
			if t.After(last) {
				last = t
			}
		}
	}
	return last
}

// FormatStaticCheckHuman 人类可读输出。
func FormatStaticCheckHuman(cands []StaticCheckCandidate) string {
	if len(cands) == 0 {
		return "  静态可核对：无候选\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "  静态可核对候选 %d 个（真机轮次前无 SDK 头文件核对）\n", len(cands))
	for _, c := range cands {
		fmt.Fprintf(&sb, "    %s [%s] %s\n", tsFmt(c.RoundAt), c.RoundKind, snippetOf(c.RoundSnippet))
	}
	return sb.String()
}
