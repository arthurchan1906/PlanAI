package eval

// P0a1b T7 构建产物完整性（PROCESS_QUALITY_SPEC §2.1 构建产物完整性，验收③）。
// 规则：构建类命令后校验 `*.app`/`*.appex` 含主可执行文件 + 关键 Info.plist 键进产物。
// 单样本方向性（验收③）：01a013f3 8/20 10:24 xcodebuild build（BUILD SUCCEEDED）→ 10:25:21
// agent 声称「编译通过」→ 10:27:26 `ls .../EncryptDrive.app/` 输出 total 16（空壳，无主可执行
// EncryptDrive 二进制）→ 规则命中空壳。用户真机安装报 Code 3000。
//
// 复用 extract.go CmdSemantics build 分类（isRealBuild）。产物检查命令识别：ls/plutil/PlistBuddy/file
// 指向 `*.app`/`*.appex` 路径（排除构建日志读取 Logs/Build——buildNoisePathRe 同源）。

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ArtifactParams T7 构建产物完整性参数。
type ArtifactParams struct {
	CheckWindowMin int // 构建后产物检查窗口（默认 30 分钟）
	LsMinTotal     int // ls 输出 total 低于该值视为空壳（默认 200 块 ≈ 无主二进制）
}

// DefaultArtifactParams 默认参数。
func DefaultArtifactParams() ArtifactParams {
	return ArtifactParams{CheckWindowMin: 30, LsMinTotal: 200}
}

// HollowBuild T7 空壳构建候选（BUILD SUCCEEDED / agent 自报成功，但产物无主可执行文件）。
type HollowBuild struct {
	BuildCmd    string    `json:"build_cmd"`
	BuildAt     time.Time `json:"build_at"`
	AppPath     string    `json:"app_path,omitempty"`
	CheckCmd    string    `json:"check_cmd,omitempty"`
	CheckAt     time.Time `json:"check_at,omitempty"`
	Hollow      bool      `json:"hollow"`      // 空壳信号：产物检查显示无主可执行
	Placeholder bool      `json:"placeholder"` // CFBundleExecutable 占位符残留（$(EXECUTABLE_NAME)）
	ClaimOK     bool      `json:"claim_ok"`    // agent 文本声称编译通过（自报 vs 产物矛盾佐证）
	Evidence    []string  `json:"evidence"`
}

var appPathRe = regexp.MustCompile(`(?i)(?:^|[\s'"/])([A-Za-z0-9_.\-]+\.(?:app|appex))(?:[\s'"/]|$)`)

var lsTotalRe = regexp.MustCompile(`(?i)total\s+(\d+)`)

// DetectHollowBuilds 扫描构建命令后的产物检查，标记空壳构建候选（T7）。
func DetectHollowBuilds(turns []Turn, p ArtifactParams) []HollowBuild {
	if p.CheckWindowMin <= 0 {
		p.CheckWindowMin = 30
	}
	if p.LsMinTotal <= 0 {
		p.LsMinTotal = 200
	}
	var out []HollowBuild
	// 扁平时间索引：构建与产物检查可能跨 turn（01a013f3 10:24 构建 → 10:26:21 user 贴安装失败日志
	// 切新 turn → 10:27:26 产物检查），按全量记录时间窗扫描。
	var all []Record
	for i := range turns {
		for j := range turns[i].Records {
			all = append(all, turns[i].Records[j])
		}
	}
	for i := range all {
		rec := &all[i]
		if rec.Tool.Tool != "bash" || !isRealBuild(rec.Tool.Command) {
			continue
		}
		hb := HollowBuild{BuildCmd: rec.Tool.Command, BuildAt: rec.CreatedAt}
		for k := i + 1; k < len(all); k++ {
			r2 := &all[k]
			if r2.CreatedAt.Sub(rec.CreatedAt) > time.Duration(p.CheckWindowMin)*time.Minute {
				break
			}
			if !isArtifactCheck(r2.Tool.Command) {
				continue
			}
			hb.CheckCmd = r2.Tool.Command
			hb.CheckAt = r2.CreatedAt
			if app := appPathRe.FindStringSubmatch(r2.Tool.Command); len(app) > 1 {
				hb.AppPath = app[1]
			}
			if hb.AppPath == "" && len(hb.Evidence) == 0 {
				continue // 无法定位 app 路径的检查不计（保守）
			}
			// 空壳判定 1：ls 产物目录无主可执行文件
			if isLsCommand(r2.Tool.Command) && isHollowLsOutput(r2.Tool.Command, r2.Content, hb.AppPath, p.LsMinTotal) {
				hb.Hollow = true
				hb.Evidence = append(hb.Evidence,
					fmt.Sprintf("产物检查 %s: %s 目录无主可执行文件（空壳）", r2.CreatedAt.Format("15:04:05"), hb.AppPath))
			}
			// 空壳判定 2：产物 Info.plist CFBundleExecutable 占位符残留
			if isPlistCheck(r2.Tool.Command) && strings.Contains(r2.Content, "$(EXECUTABLE_NAME)") {
				hb.Placeholder = true
				hb.Evidence = append(hb.Evidence,
					fmt.Sprintf("产物检查 %s: CFBundleExecutable 占位符未替换", r2.CreatedAt.Format("15:04:05")))
			}
		}
		if hb.Hollow || hb.Placeholder {
			out = append(out, hb)
		}
	}
	return out
}

// isArtifactCheck 产物检查命令：ls/plutil/PlistBuddy/file/find 指向 *.app/*.appex。
// 注意：不排除 DerivedData/Build/ 路径——产物恰在 DerivedData/Build/Products 下（01a013f3
// 10:27:26 `find .../DerivedData/.../Build/Products/.../EncryptDrive.app` 即产物检查）；
// 只排除构建日志读取（Logs/Build、build 版本号目录）。
func isArtifactCheck(cmd string) bool {
	lower := strings.ToLower(cmd)
	if strings.Contains(lower, "/logs/build") || buildVersionRe.MatchString(cmd) {
		return false // 构建日志读取（T5 噪声防护保留项）
	}
	if !strings.Contains(lower, ".app") && !strings.Contains(lower, ".appex") {
		return false
	}
	for _, k := range []string{"ls ", "plutil", "plistbuddy", "file ", "find "} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

// buildVersionRe 构建目录版本号（build1/build2 等，T5 噪声防护同源）。
var buildVersionRe = regexp.MustCompile(`(?i)build[0-9]`)

func isLsCommand(cmd string) bool {
	return strings.Contains(strings.ToLower(cmd), "ls ")
}

func isPlistCheck(cmd string) bool {
	lower := strings.ToLower(cmd)
	return strings.Contains(lower, "plistbuddy") || strings.Contains(lower, "plutil")
}

// isHollowLsOutput 判定 ls 产物目录输出是否为空壳：
// 1) total 块数 < LsMinTotal（空壳 app 极小，正常 app 含主二进制 ≥ 数百 KB）
// 2) 输出不含主可执行文件名（app bundle 名去掉 .app/.appex）
func isHollowLsOutput(cmd, out, appPath string, minTotal int) bool {
	binary := strings.TrimSuffix(strings.TrimSuffix(appPath, ".app"), ".appex")
	hasBinary := false
	lines := strings.Split(out, "\n")
	for _, ln := range lines {
		if strings.Contains(ln, " "+binary) || strings.HasSuffix(strings.TrimSpace(ln), binary) {
			hasBinary = true
			break
		}
	}
	if m := lsTotalRe.FindStringSubmatch(out); len(m) > 1 {
		var total int
		fmt.Sscanf(m[1], "%d", &total)
		return total < minTotal && !hasBinary
	}
	// 无 total 行（输出被截断）：保守判非空壳（缺证据不误报）
	return false
}

// FormatArtifactHuman T7 人类可读输出。
func FormatArtifactHuman(hbs []HollowBuild) string {
	var b strings.Builder
	fmt.Fprintf(&b, "T7 构建产物完整性（空壳构建候选 %d 个）\n", len(hbs))
	for _, hb := range hbs {
		fmt.Fprintf(&b, "  ⚠ %s %s\n", hb.BuildAt.Format("01-02 15:04:05"), snippetOf(hb.BuildCmd))
		for _, e := range hb.Evidence {
			fmt.Fprintf(&b, "    - %s\n", e)
		}
	}
	if len(hbs) == 0 {
		b.WriteString("  无空壳构建候选（构建后产物检查正常或无检查）\n")
	}
	return b.String()
}
