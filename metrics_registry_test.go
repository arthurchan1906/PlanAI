package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// metricRegistry mirrors docs/METRICS_REGISTRY.md: every top-level printRow key
// emitted by `aipmc metrics`, plus its window behavior (full=全表 / since=随
// --since / log=日志文件). Keep in sync with the doc — the test below fails on
// any drift (new/renamed/duplicated metric keys), the caliber-divergence guard
// for the 8/26 "l2_coverage 40.1 vs 52.8" lesson.
var metricRegistry = map[string]string{
	"B1  summary_coverage":             "full",
	"B2  l2_nested_goal":               "full",
	"B2  l2_md_block":                  "full",
	"B6  event_dup_rate":               "full",
	"D2  event_processed_rate(可行动)": "since",
	"E6  workflow_score":               "full",
	"E7  task_completion_rate":         "full",
	"H2  metadata_health":              "full",
	"H2  rel_path_coverage(filetools)": "since",
	"H2  rel_path_coverage(bash)":      "full",
	"B8  hook_error(agent)":            "log",
	"B8  hook_error(post-commit)":      "log",
	"C1  inject_coverage":              "log",
	"C1  inject_rate":                  "log",
	"C2  file_parse_ok_rate":           "log",
	"C3  action_items(最新emerge)":     "log",
	"C3  suppressed(char_limit)":       "log",
	"E3  cache_hit_rate":               "log",
	"E5  mcp_calls":                    "log",
	"E5  mcp_err_reason":               "log",
	"E5  mcp_read/write":               "log",
	"E5  mcp_success_rate":             "log",
	"E5  update_status 显式率(窗口)":   "since",
	"E8  pipeline_health":              "log",
	"E8  review_error":                 "log",
	"E9  done_gate":                    "log",
}

// wantSince is the documented "随 --since" window set (metrics.go header +
// docs/METRICS_REGISTRY.md). Any drift here is a window-semantics change.
var wantSince = map[string]bool{
	"D2  event_processed_rate(可行动)": true,
	"E5  update_status 显式率(窗口)":   true,
	"H2  rel_path_coverage(filetools)": true,
}

func TestMetricsRegistryConsistent(t *testing.T) {
	src, err := os.ReadFile("metrics.go")
	if err != nil {
		t.Fatalf("read metrics.go: %v", err)
	}
	re := regexp.MustCompile(`printRow\("([^"]+)"`)
	emitted := map[string]bool{}
	var dup []string
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		key := m[1]
		if strings.HasPrefix(key, " ") {
			continue // P0 三件套子行（orphan_rate 等），注册表以 P0 父项登记
		}
		if emitted[key] {
			dup = append(dup, key)
		}
		emitted[key] = true
	}
	if len(dup) > 0 {
		t.Errorf("重复指标键（口径分叉风险）: %v", dup)
	}

	var missing, extra []string
	for k := range metricRegistry {
		if !emitted[k] {
			missing = append(missing, k)
		}
	}
	for k := range emitted {
		if _, ok := metricRegistry[k]; !ok {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("注册表有但 metrics.go 未输出: %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("metrics.go 输出但注册表未登记（需同步 docs/METRICS_REGISTRY.md）: %v", extra)
	}

	for k, w := range metricRegistry {
		if wantSince[k] != (w == "since") {
			t.Errorf("窗口标注不一致: %q 注册表=%s wantSince=%v", k, w, wantSince[k])
		}
	}
}
