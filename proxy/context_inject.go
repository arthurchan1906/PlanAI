package proxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	pmdb "aipmc/db"

	"aipmc/session"
	"aipmc/store"
	"aipmc/u"
)

const (
	maxInjectChars   = 800            // hard cap to prevent context explosion
	guidelinesBudget = 600            // dedicated char budget for guidelines.md
	sessionTTL       = 48 * time.Hour // ignore sessions older than this
	actionItemCeil   = 10             // 2.1: safety ceiling for formatted action items
	perTypeCap       = 5              // 2.1: max individual items per event type
	warnActReserve   = 200            // E 线（8/27）：warnings+actionItems 保留预算（字节）
)

// E 线预算重排说明（8/27，数据依据见 METRICS_BASELINE 与注入日志）：
// 8/27 当日 1047/1047 次注入被 char_limit 裁剪（0 次完整注入），段裁剪条数
// file_cut=9404 / warn=7631 / act=4800 / goals=3113 / guide=1017——guidelines(600B)
// 与 fileAssoc 把 written 顶过 750 后，warnings/actionItems 的 guard 恒真、全被挤掉。
// 预算重排：buildContextBlock 计算 guidelines 可用预算时扣除 warnActReserve，
// 高优段（warnings+actionItems）稳定获得 ≈180B（≈2-3 条）到达空间，不被规范文本挤占。
// 对齐 v1.13 §4 验收：guidelines 满 600 时高优段不被挤掉。

// isEmergeEvent checks if an event type should be surfaced as an actionable item.
func isEmergeEvent(typ string) bool {
	return strings.HasSuffix(typ, "_orphan") || strings.HasSuffix(typ, "_stale_file") ||
		strings.Contains(typ, "untracked") || typ == "mcp_error"
}

type injectState struct {
	lastAt      time.Time
	contentHash string
}

var (
	injectTracker sync.Map // map[agent]injectState
	injectReqSeq  uint64   // W1（8/13）per-request 标识，区分同 session 同秒多次请求
	sessionCache  struct {
		mu          sync.RWMutex
		goals       []string
		warnings    []string
		actionItems []string // ⚠️ emerge events → actionable nudge
		contentHash string
		updatedAt   time.Time
		ttl         time.Duration
	}
	guidelinesCache struct {
		mu        sync.RWMutex
		content   string
		updatedAt time.Time
		ttl       time.Duration
	}
)

// segCounts 按 source_segment 统计被 cap 裁剪的条目数（W1 8/13，F2 数据源）。
// 实际写序：fileAssoc → guidelines → warnings → actionItems → goals（Vision tip 最后）。
// E 线（8/27）：guidelines 计算 avail 时扣除 warnActReserve，warn/act 稳定获得保留空间；
// 裁剪仍应集中在 goals/guidelines（低优先），伤及 warn/act/fileAssoc 才是问题。
type segCounts struct {
	fileAssoc     int
	warnings      int
	actionItems   int
	goals         int
	guidelines    int
	guidelinesDel int
}

func (s segCounts) total() int {
	return s.fileAssoc + s.warnings + s.actionItems + s.goals + s.guidelines
}

// extractSessionID 尽力从请求体取 session（codex 在 client_metadata.session_id；
// claude/gemini 可能无，留空——漏斗按 agent+时间窗兜底对齐）。
func extractSessionID(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	if md, ok := m["client_metadata"].(map[string]any); ok {
		if sid, ok := md["session_id"].(string); ok && sid != "" {
			return sid
		}
	}
	if sid, ok := m["session_id"].(string); ok && sid != "" {
		return sid
	}
	return ""
}

func init() {
	sessionCache.ttl = 5 * time.Minute
	guidelinesCache.ttl = 10 * time.Minute
}

// injectProjectPath 当前注入项目根（M1a 对账，8/26）：从写库目标 pmdb.FindPath
// 单点推导（pmdb.ProjectRoot），与 eval 过滤基准同源——修复全局日志混入其他项目
// 注入行导致 M1a 分母跨项目污染；env（PMAI_HOME 等）与正常项目结构统一。
var injectProjectOnce sync.Once
var injectProjectPath string

// absPathRe 匹配绝对路径段（方法 2 通用遍历用）。包级编译：projectRootFromBody
// 每个注入请求都会执行（proxy 热路径），避免函数体内重复 MustCompile。
var absPathRe = regexp.MustCompile(`(?:^|[\s"'(<])(/[^\s"'<>()]+)`)

func currentInjectProject() string {
	injectProjectOnce.Do(func() {
		if p, err := pmdb.FindPath(); err == nil {
			injectProjectPath = pmdb.ProjectRoot(p)
		}
	})
	return injectProjectPath
}

// injectProjectForRequest 按请求归因注入项目（C0，8/27）：优先从请求体绝对路径
// 推导项目根（修复 F3 根因——sync.Once 进程级缓存一旦在首次请求时推错，整进程
// 日志 project= 归属全部污染 :148/:151/:174/:182/:221/:232），找不到时回退到
// 进程级 currentInjectProject（env/cwd 推导，保持 M1a 对账与 eval 过滤基准同源）。
func injectProjectForRequest(body []byte) string {
	if p := projectRootFromBody(body); p != "" {
		return p
	}
	return currentInjectProject()
}

// projectRootFromBody 从请求文本找绝对路径，向上遍历找含 .pmai 的目录即项目根。
// 优先精确命中 /.pmai/ 段直接截取（一次 Index+LastIndex），否则回退通用绝对路径
// 正则逐层 Stat。env 模式（PMAI_HOME 指向 ~/.aipmc 等无 .pmai 层）自然不命中，
// 由调用方回退 currentInjectProject。
func projectRootFromBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	text := string(body)
	// 方法 1：精确命中 /.pmai/ 段直接截取项目根。注意: ① 必须过 os.Stat 验证
	// 目录真实存在（与方法 2 一致，防请求文本提及不存在的 /.pmai/ 路径误判）；
	// ② macOS 含空格路径会在空格处截断（start 回溯在分隔符停），含空格项目根
	// 归因不到——已知限制，回退进程级推导。
	if i := strings.Index(text, "/.pmai/"); i > 0 {
		start := i
		for start > 0 {
			c := text[start-1]
			if c == ' ' || c == '\t' || c == '"' || c == '\'' || c == '<' || c == '(' || c == '\n' || c == ',' {
				break
			}
			start--
		}
		abs := text[start:i]
		if strings.HasPrefix(abs, "/") && !strings.ContainsAny(abs, " \t") {
			if info, err := os.Stat(filepath.Join(abs, ".pmai")); err == nil && info.IsDir() {
				return abs
			}
		}
	}
	// 方法 2：通用绝对路径逐层 Stat 找 .pmai（含空格路径在此同样截断，见上注记）。
	for _, m := range absPathRe.FindAllStringSubmatch(text, -1) {
		if len(m) < 2 {
			continue
		}
		p := strings.TrimRight(m[1], `"'<>(),`)
		if p == "" || !strings.HasPrefix(p, "/") {
			continue
		}
		for dir := p; dir != "/" && dir != "."; {
			if info, err := os.Stat(filepath.Join(dir, ".pmai")); err == nil && info.IsDir() {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}

// InjectSessionContext prepends recent session goals into the system message
// of a proxy request. One injection per agent per unique content (content-hash
// based deduplication). Content is capped at maxInjectChars.
// If the request body contains file paths (from code context), file→task
// associations are added to help the agent understand which PM entities
// are related to the files it's working on.
func InjectSessionContext(body []byte, agent string) []byte {
	// A/B 开关（8/17 实验）：AIPMC_INJECT=0 关闭注入，默认开启（生产不变）。
	// 关闭用于隔离「注入 SP 抖动」对 deepseek prefix cache 的独立影响。
	if os.Getenv("AIPMC_INJECT") == "0" {
		return body
	}
	goals, warnings, actionItems, blockHash := getCachedContext()
	guidelines := loadGuidelines()

	// File awareness is computed per-request — must NOT be inside the
	// 5-minute session cache, or it returns nil on every cache hit.
	fileAssoc := resolveFileContext(body, agent)
	// C0（8/27）：按请求归因 project=，一次计算全请求日志点复用。
	proj := injectProjectForRequest(body)
	// W1（8/13）：session/req 标识进 inject/suppressed 日志，供可见性漏斗按
	// (agent, session, req, ts) 对齐注入与事件处理记录。
	sessionID := extractSessionID(body)
	// req 标识带 pid：跨进程/重启后不冲突（P3，8/13 审核）。
	reqID := fmt.Sprintf("r%d-%d", os.Getpid(), atomic.AddUint64(&injectReqSeq, 1))

	if len(goals) == 0 && len(fileAssoc) == 0 && len(guidelines) > 0 {
		u.LogShared("INJECT", "inject project=%s agent=%s session=%s req=%s source=guidelines_only", proj, agent, sessionID, reqID)
	}
	if len(goals) == 0 && len(fileAssoc) == 0 && len(guidelines) == 0 {
		u.LogShared("INJECT", "skip project=%s agent=%s session=%s req=%s reason=no_summary_data", proj, agent, sessionID, reqID)
		return body
	}

	// Include fileAssoc in the content hash so file-context changes
	// trigger re-injection even when session data is unchanged.
	fullHash := hashString(fmt.Sprintf("%s%v%s", blockHash, fileAssoc, guidelines))

	block, sc := buildContextBlock(goals, warnings, actionItems, fileAssoc, guidelines)

	// Content-hash based dedup（8/18 修正）：same_content 跳过时不能直接返回
	// 未注入的 body——每个请求对客户端都是全新 body，注入块不随会话保留；返回
	// 未注入 body 会让 SP 在「带块/不带块」间交替（cap_1 带注入块 vs cap_2 无
	// 注入块，字节实证 09:53:39/09:53:57）。正确语义：内容未变时重新注入同一
	// block（排序后内容逐字节稳定），SP 全程一致、缓存连续。shouldInject 仅
	// 保留 same_content 观测点，跳过 tracker 更新与 inject 明细日志。
	sameContent := !shouldInject(agent, sessionID, reqID, fullHash, proj)
	result := injectIntoPrompt(body, block, agent)
	if sameContent {
		return result
	}
	injectTracker.Store(agent, injectState{lastAt: time.Now(), contentHash: fullHash})
	u.LogShared("INJECT", "agent=%s project=%s session=%s req=%s hash=%s goals=%d warnings=%d actions=%d file_total=%d guidelines=%d guide_del=%d chars=%d",
		agent, proj, sessionID, reqID, fullHash[:8], len(goals), len(warnings), len(actionItems), len(fileAssoc), len(guidelines), sc.guidelinesDel, len(block))
	// suppressed 计数移到 shouldInject 之后：去重跳过（same_content/cooldown）的请求
	// 不产出抑制记录——收紧 F2 口径（旧实现把未注入请求的抑制也算进去，虚高）。
	// 8/18 修订（HARNESS §1.3）：char_limit 裁剪的请求已实际注入，仍写表，
	// suppressed 如实记录（对应 :153）。same_content/no_summary 已在上方 return，
	// 不写表——对照组从日志侧重建。
	if sc.total() > 0 {
		u.LogShared("INJECT", "suppressed=%d reason=char_limit cap=%d agent=%s project=%s session=%s req=%s segments=file_cut:%d warn:%d act:%d goals:%d guide:%d",
			sc.total(), maxInjectChars, agent, proj, sessionID, reqID, sc.fileAssoc, sc.warnings, sc.actionItems, sc.goals, sc.guidelines)
	}
	// inject_log（HARNESS §1.3，8/18 修订写策略）：实际注入即写（含裁剪）。
	source := ""
	if len(goals) == 0 && len(fileAssoc) == 0 && len(guidelines) > 0 {
		source = "guidelines_only"
	}
	writeInjectLog(proj, agent, sessionID, reqID, source, fullHash, len(block), sc.total() > 0, goals, warnings, actionItems, fileAssoc, guidelines)
	return result
}

// writeInjectLog records one actual injection into inject_log. Failure must
// not break the injection hot path — log and continue. suppressed=1 表示本次
// 注入有内容被 cap 裁剪（对应 :153），提取器按此分层（8/18 修订，HARNESS §1.3）。
func writeInjectLog(project, agent, sessionID, reqID, source string, fullHash string, chars int, suppressed bool, goals, warnings, actionItems, fileAssoc []string, guidelines string) {
	segments := map[string]any{
		"fileAssoc":   fileAssoc,
		"warnings":    warnings,
		"actionItems": actionItems,
		"goals":       goals,
		"guidelines":  len(guidelines) > 0,
	}
	supp := 0
	if suppressed {
		supp = 1
	}
	err := pmdb.InsertInjectLog(pmdb.InjectLogEntry{
		ID:           u.Slug("inj"),
		Agent:        agent,
		SessionID:    sessionID,
		ReqID:        reqID,
		TS:           u.NowISO(),
		Hash:         fullHash[:8],
		Source:       source,
		SegmentsJSON: u.JsonStr(segments),
		Chars:        chars,
		Suppressed:   supp,
		Project:      project,
	})
	if err != nil {
		u.LogShared("INJECT", "inject_log project=%s write_err=%v", project, err)
	}
}

func shouldInject(agent, sessionID, reqID, contentHash, project string) bool {
	v, ok := injectTracker.Load(agent)
	if !ok {
		return true
	}
	st := v.(injectState)
	if st.contentHash == contentHash {
		u.LogShared("INJECT", "skip project=%s agent=%s session=%s req=%s reason=same_content hash=%s", project, agent, sessionID, reqID, contentHash[:8])
		return false
	}
	return true
}

func getCachedContext() (goals, warnings, actionItems []string, hash string) {
	sessionCache.mu.RLock()
	if time.Since(sessionCache.updatedAt) < sessionCache.ttl && len(sessionCache.goals) > 0 {
		defer sessionCache.mu.RUnlock()
		return sessionCache.goals, sessionCache.warnings, sessionCache.actionItems, sessionCache.contentHash
	}
	sessionCache.mu.RUnlock()

	sessionCache.mu.Lock()
	defer sessionCache.mu.Unlock()

	cutoff := time.Now().Add(-sessionTTL).Format("2006-01-02T15:04:05")
	rows, err := store.ListSessionSummariesWithSummary(cutoff, 3)
	if err != nil || len(rows) == 0 {
		sessionCache.goals = nil
		sessionCache.contentHash = ""
		return nil, nil, nil, ""
	}
	goals = make([]string, 0, len(rows))
	for _, r := range rows {
		var l2 session.SessionL2Summary
		if json.Unmarshal([]byte(r.Summary), &l2) == nil && l2.Goal != "" {
			l2.Goal = session.UnnestGoal(l2.Goal)
			sid := u.Prefix(r.SessionID, 8)
			goals = append(goals, fmt.Sprintf("[%s] %s", sid, l2.Goal))
		}
		// Extract blind_edit_loop findings from review_json
		if r.ReviewJSON != "" {
			var review map[string]any
			if json.Unmarshal([]byte(r.ReviewJSON), &review) == nil {
				findings, _ := review["findings"].([]any)
				for _, fi := range findings {
					f, _ := fi.(map[string]any)
					tag, _ := f["tag"].(string)
					if tag == "blind_edit_loop" {
						ev, _ := f["evidence"].(string)
						if ev != "" {
							warnings = append(warnings, fmt.Sprintf("[%s] \u26a0\ufe0f %s", u.Prefix(r.SessionID, 8), ev))
						}
					}
				}
			}
		}
	}

	// Merge user negative feedback from recent discussion_log
	if fb := detectUserFrustration(); len(fb) > 0 {
		warnings = append(warnings, fb...)
	}

	// ── Pipeline emerge events → actionable nudge ──────────────────────
	actionItems = buildActionItems()

	sessionCache.goals = goals
	sessionCache.warnings = warnings
	sessionCache.actionItems = actionItems
	sessionCache.contentHash = hashString(fmt.Sprintf("%v%v%v", goals, warnings, actionItems))
	sessionCache.updatedAt = time.Now()
	return goals, warnings, actionItems, sessionCache.contentHash
}

// buildActionItems reads unconsumed pipeline emerge events and formats them
// as actionable items for INJECT. 2.1: events are priority-ordered (most
// actionable first) and aggregated by type — hotspot_untracked / mcp_error
// collapse into one line each, while commit_orphan / task_stale_file stay
// per-entity with a per-type cap. The old maxActions=3 hard cap is replaced
// by budget-driven formatting in buildContextBlock.
func buildActionItems() []string {
	events, err := store.GetUnconsumedEvents()
	if err != nil || len(events) == 0 {
		return nil
	}
	var list []emergeEvent
	for _, e := range events {
		typ, _ := e["type"].(string)
		if !isEmergeEvent(typ) {
			continue
		}
		summary, _ := e["summary"].(string)
		if summary == "" {
			continue
		}
		entityID, _ := e["entity_id"].(string)
		createdAt, _ := e["created_at"].(string)
		list = append(list, emergeEvent{typ, entityID, summary, createdAt, eventPriority(typ)})
	}
	if len(list) == 0 {
		return nil
	}
	items := formatActionItems(list)
	// Keep the emerge_events observability line (EVALUATION.md C3 复测方法依赖它):
	// total = raw unconsumed emerge events, items = after priority/aggregation/caps.
	u.LogShared("INJECT", "emerge_events total=%d types=%v items=%d perTypeCap=%d ceil=%d",
		len(events), eventTypeBreakdown(events), len(items), perTypeCap, actionItemCeil)
	return items
}

// formatActionItems is the pure formatting half of buildActionItems:
// priority sort, aggregation, per-type caps, ceiling.
func formatActionItems(list []emergeEvent) []string {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].prio != list[j].prio {
			return list[i].prio > list[j].prio
		}
		return list[i].createdAt < list[j].createdAt
	})

	var items []string
	// Aggregated types first: one compact line each.
	if h := aggregateHotspots(list); h != "" {
		items = append(items, h)
	}
	if m := aggregateMCPErrors(list); m != "" {
		items = append(items, m)
	}
	// Per-entity items in priority order, capped per type.
	perTypeCount := map[string]int{}
	for _, e := range list {
		if isAggregatedType(e.typ) {
			continue
		}
		if perTypeCount[e.typ] >= perTypeCap {
			continue
		}
		perTypeCount[e.typ]++
		line := fmt.Sprintf("\u26a0\ufe0f %s", e.summary)
		if hint := actionToolHint(e.typ, e.entityID); hint != "" {
			line += "\n  \u2192 " + hint
		}
		items = append(items, line)
	}
	if len(items) > actionItemCeil {
		items = items[:actionItemCeil]
	}
	return items
}

// eventPriority ranks emerge event types for injection order: events with a
// unique, concrete fix the agent can do now come first.
func eventPriority(typ string) int {
	switch {
	case strings.HasSuffix(typ, "_orphan"):
		return 4 // unique fix: bind commit to a task
	case strings.HasSuffix(typ, "_stale_file"):
		return 3 // unique fix: verify task completion
	case typ == "mcp_error":
		return 3 // unique fix: retry / check params
	case strings.Contains(typ, "untracked"):
		return 2 // aggregated: create tracking task
	}
	return 0
}

// isAggregatedType reports whether events of this type collapse into one
// injected line (Claude 审核细化: hotspot/mcp_error 聚合, orphan/link 不聚合).
func isAggregatedType(typ string) bool {
	return strings.Contains(typ, "untracked") || typ == "mcp_error"
}

type emergeEvent struct {
	typ, entityID, summary, createdAt string
	prio                              int
}

// aggregateHotspots collapses all hotspot_untracked events into one line.
func aggregateHotspots(list []emergeEvent) string {
	var files []string
	total := 0
	for _, e := range list {
		if !strings.Contains(e.typ, "untracked") {
			continue
		}
		total++
		if len(files) < 5 {
			files = append(files, filepath.Base(e.entityID))
		}
	}
	if total == 0 {
		return ""
	}
	shown := strings.Join(files, ", ")
	if total > len(files) {
		shown += fmt.Sprintf(" 等 %d 个文件", total)
	}
	return fmt.Sprintf("\u26a0\ufe0f %d 个文件被多 session 修改且无 task 跟踪：%s\n  \u2192 aipm_create_task(title=\"跟踪热点文件\", plan_id=\"...\") 为最活跃文件建 task", total, shown)
}

// aggregateMCPErrors collapses all mcp_error events into one line.
func aggregateMCPErrors(list []emergeEvent) string {
	var parts []string
	total := 0
	for _, e := range list {
		if e.typ != "mcp_error" {
			continue
		}
		total++
		if len(parts) < 3 {
			parts = append(parts, e.summary)
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("\u26a0\ufe0f %d 个 MCP 工具调用失败（最近：%s）\n  \u2192 检查参数后重试；持续失败用 aipm_search_context 查正确参数", total, strings.Join(parts, "；"))
}

// actionToolHint returns the MCP tool call suggestion for an event type.
func actionToolHint(eventType, entityID string) string {
	switch {
	case strings.HasSuffix(eventType, "_orphan"):
		return fmt.Sprintf("\u4fee\u590d: aipm_record_commit(task_id=\"?\", title=\"...\")  |  \u8be6\u60c5: aipm_get_commit(\"%s\")", entityID)
	case strings.HasSuffix(eventType, "_stale_file"):
		return fmt.Sprintf("\u68c0\u67e5: aipm_get_task(\"%s\") \u2014 \u786e\u8ba4 task \u662f\u5426\u771f\u6b63\u5b8c\u6210", entityID)
	case strings.Contains(eventType, "untracked"):
		return fmt.Sprintf("\u521b\u5efa: aipm_create_task(title=\"\u8ffd\u8e2a %s\", plan_id=\"...\")", u.TruncateStr(entityID, 40))
	case eventType == "mcp_error":
		return fmt.Sprintf("\u5de5\u5177 %s \u4e0a\u6b21\u8c03\u7528\u5931\u8d25\uff0c\u68c0\u67e5\u53c2\u6570\u540e\u91cd\u8bd5\u3002\u5982\u6301\u7eed\u5931\u8d25\uff0c\u7528 aipm_search_context \u67e5\u627e\u6b63\u786e\u7684\u53c2\u6570\u503c", entityID)
	}
	return ""
}

// eventTypeBreakdown returns a compact type→count string for logging.
func eventTypeBreakdown(events []map[string]any) string {
	counts := map[string]int{}
	for _, ev := range events {
		typ, _ := ev["type"].(string)
		counts[typ]++
	}
	parts := make([]string, 0, len(counts))
	for t, c := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", t, c))
	}
	return strings.Join(parts, " ")
}

func buildContextBlock(goals, warnings, actionItems, fileAssoc []string, guidelines string) (string, segCounts) {
	var buf bytes.Buffer
	var written int
	// write 统一计费：written = 实际 block 字节数（含段头/Vision tip）。
	// 8/28 修 chars≤800（v1.13 §4 验收）：旧实现 written 不含段头与 Vision tip，
	// guard(maxInjectChars-50) 只约束内容，block 实际可达 ~890 超出上限。
	// 现在所有 guard 用 maxInjectChars，block 严格 ≤800。
	write := func(s string) {
		buf.WriteString(s)
		written += len(s)
	}
	write("\n[AIPM Context]")
	var sc segCounts

	// ── File associations (highest priority, independent hard sub-budget) ──
	// 8/13 F2 修复：独立 200 字节子预算 + 前置写入。旧实现 guidelines 先写且
	// written 按源长度计数（len(guidelines) 而非截断后长度），1622 字规范把
	// written 顶到 1642，后续所有段 guard 恒真 → fileAssoc 100% 被裁。
	// 8/18 预算校准（数据实证）：200B 固定预算在 file_total p50=9/p90=45 时平均
	// 裁剪率 82%（1669 次注入仅 2.6% 完整注入）→ fileAssoc 功能失效。动态缩放
	// min(200+30×len, 500)：9 文件→470B≈5 行（56% 注入率）；cap 500B 防挤爆总
	// 预算（maxInjectChars=800）。参数依据记录于 task-20260818-134522-c6d5e9。
	fileAssocBudget := min(200+30*len(fileAssoc), 500)
	if len(fileAssoc) > 0 {
		write("\n[文件关联]")
		faWritten := 0
		for _, fa := range fileAssoc {
			line := "\n" + fa
			if faWritten+len(line) > fileAssocBudget {
				sc.fileAssoc++
				continue
			}
			write(line)
			faWritten += len(line)
		}
		write("\n")
	}

	// ── Guidelines (dedicated budget, truncated to remaining headroom) ──
	// E 线（8/27）：avail 扣除 warnActReserve——guidelines 满 600 时也必须给
	// warnings/actionItems 留保留空间（8/27 实测 warn 7631 条/日全被挤掉）；
	// 无高优段时 reserve=0，guidelines 不浪费让渡空间（保持旧行为）。
	if guidelines != "" {
		header := "\n[项目编码规范]\n"
		write(header)
		reserve := 0
		if len(warnings) > 0 || len(actionItems) > 0 {
			reserve = warnActReserve
		}
		// 8/28：avail 扣除段头+结尾换行开销，reserve 精确兑现为 warn/act 可用空间。
		avail := min(len(guidelines), guidelinesBudget, maxInjectChars-written-reserve-len(header)-1)
		if avail < 0 {
			avail = 0
		}
		if avail < len(guidelines) && avail >= 3 {
			// 截断追加 "…"(3B)：预扣，防止精确计费后 block 超 maxInjectChars（8/28）。
			avail -= 3
		}
		sc.guidelinesDel = avail
		if avail < len(guidelines) {
			sc.guidelines = 1
		}
		if avail > 0 {
			if avail < len(guidelines) {
				// avail is a byte budget — back off to a rune boundary so a
				// multi-byte CJK character is never split mid-sequence.
				cut := avail
				for cut > 0 && !utf8.RuneStart(guidelines[cut]) {
					cut--
				}
				write(guidelines[:cut] + "…")
			} else {
				write(guidelines[:avail])
			}
		}
		write("\n")
	}

	// Warnings next (high priority)
	for _, w := range warnings {
		line := w + "\n"
		if written+len(line) > maxInjectChars {
			sc.warnings++
			continue
		}
		write(line)
	}

	// ⚠️ 待处理: actionable items from pipeline emerge events
	if len(actionItems) > 0 {
		write("\n⚠️ 待处理:")
		for _, a := range actionItems {
			line := "\n" + a
			if written+len(line) > maxInjectChars {
				sc.actionItems++
				continue
			}
			write(line)
		}
	}

	if len(goals) > 0 {
		write("\n最近的 session:\n")
		for _, g := range goals {
			line := "- " + g + "\n"
			if written+len(line) > maxInjectChars {
				sc.goals++
				continue
			}
			write(line)
		}
	}

	// Vision tool tip: inject only when vision models are configured and room permits.
	if tip := visionToolTip(written, maxInjectChars); tip != "" {
		write(tip)
	}

	return buf.String(), sc
}

// visionToolTip returns a usage hint for aipmc_vision if room permits.
func visionToolTip(written, maxChars int) string {
	if !hasVisionModels() {
		return ""
	}
	tip := "\n[工具] aipmc_vision 可截图自查 UI：\nscreencapture/adb/xcrun 截图后用 aipmc_vision 传入代码片段+期望效果\n"
	if written+len(tip) > maxChars {
		return ""
	}
	return tip
}

// hasVisionModels checks models.json for any vision-tagged model.
func hasVisionModels() bool {
	reg := pmdb.LoadModelRegistry()
	for _, vm := range reg.Models {
		for _, t := range vm.Tags {
			if strings.EqualFold(t, "vision") {
				return true
			}
		}
	}
	return false
}

// ── File awareness: extract file paths from agent request body ──────────

// resolveFileContext extracts file paths from the LLM request body and
// returns PM entity associations for files the agent is working on.
// This gives the agent immediate context: "mcp/mcp.go → task-xxx (P0, in_progress)".
func resolveFileContext(body []byte, agent string) []string {
	paths := extractFilePaths(body, agent)
	if len(paths) == 0 {
		return nil
	}

	// Query file→task associations through graph_edges + commits
	edges, err := store.ListGraphEdges("", "", "file_touch")
	if err != nil {
		u.LogShared("INJECT", "file_assoc edges_err=%v", err)
		return nil
	}
	if len(edges) == 0 {
		u.LogShared("INJECT", "file_assoc paths=%d edges=0 reason=no_graph_data", len(paths))
		return nil
	}

	// Build file→task index from graph edges
	fileTasks := map[string]map[string]string{} // file → {taskID: taskTitle}
	for _, e := range edges {
		evidenceJSON, _ := e["evidence_json"].(string)
		if evidenceJSON == "" {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(evidenceJSON), &ev) != nil {
			continue
		}
		intersect, _ := ev["intersect"].([]any)
		for _, f := range intersect {
			fp := u.Str(f)
			if fileTasks[fp] == nil {
				fileTasks[fp] = map[string]string{}
			}
			// Resolve commit→task via graph edges
			commitID, _ := e["target_id"].(string)
			if commitID != "" {
				c, err := store.GetCommit(commitID)
				if err == nil {
					tid := u.Str(c["task_id"])
					ttl := u.Str(c["title"])
					if tid != "" && ttl != "" {
						// Get task status
						task, _ := store.GetTask(tid)
						status := ""
						priority := ""
						if task != nil {
							status, _ = task["status"].(string)
							priority, _ = task["priority"].(string)
						}
						tag := tid
						if status != "" {
							tag += fmt.Sprintf(" (%s", status)
							if priority != "" {
								tag += fmt.Sprintf(", %s", priority)
							}
							tag += ")"
						}
						fileTasks[fp][tid] = tag
					}
				}
			}
		}
	}

	// Match extracted paths against file→task index. buildFileAssoc sorts the
	// result so both fullHash and buildContextBlock's sub-budget truncation stay
	// deterministic across requests (Go map range order is randomized — 8/18
	// cache 命中率调查: 未排序时每请求重新注入，deepseek prefix cache 在
	// system prompt 末尾断裂，观测断点 4480/4608 token)。
	assoc := buildFileAssoc(paths, fileTasks)
	if len(assoc) > 0 {
		u.LogShared("INJECT", "file_assoc files=%d matches=%d", len(paths), len(assoc))
	}
	return assoc
}

// buildFileAssoc converts the file→task index into a sorted list of
// association lines. Sorting is REQUIRED (8/18): the slice feeds both fullHash
// (order-sensitive %v) and buildContextBlock's sub-budget truncation. An
// unsorted slice varies per request because Go map range order is randomized,
// which re-injects every request and breaks the deepseek prefix cache at the
// system-prompt end (observed 4480/4608 token breakpoints).
func buildFileAssoc(paths []string, fileTasks map[string]map[string]string) []string {
	var assoc []string
	seen := map[string]bool{}
	for _, p := range paths {
		tasks, ok := fileTasks[p]
		if !ok {
			continue
		}
		for tid, tag := range tasks {
			key := p + tid
			if seen[key] {
				continue
			}
			seen[key] = true
			assoc = append(assoc, fmt.Sprintf("%s → %s %s", p, tag, u.TruncateStr(tid, 20)))
		}
	}
	sort.Strings(assoc)
	return assoc
}

// File extensions considered code files for path extraction.
var codeExts = regexp.MustCompile(`\.(go|js|ts|jsx|tsx|py|rs|java|rb|c|cpp|h|hpp|swift|kt|scala|css|html|vue|svelte|sql|sh|yaml|yml|toml|json|md)$`)

// extractFilePaths extracts file paths from the LLM request body.
// Handles both Anthropic Messages format (messages[].content) and Codex format (instructions).
func extractFilePaths(body []byte, agent string) []string {
	var raw map[string]any
	if json.Unmarshal(body, &raw) == nil {
		var textParts []string

		// Anthropic format: messages array with content blocks
		if messages, ok := raw["messages"].([]any); ok {
			for _, m := range messages {
				msg, _ := m.(map[string]any)
				if msg == nil {
					continue
				}
				content := msg["content"]
				switch c := content.(type) {
				case string:
					textParts = append(textParts, c)
				case []any:
					for _, block := range c {
						if b, ok := block.(map[string]any); ok {
							if t, ok := b["text"].(string); ok {
								textParts = append(textParts, t)
							}
						}
					}
				}
			}
		}

		// Codex format: instructions field
		if instr, ok := raw["instructions"].(string); ok && instr != "" {
			textParts = append(textParts, instr)
		}

		// OpenAI Responses format: input array (codex /v1/responses).
		// Elements: {"type":"message","content":[{"type":"input_text","text":...}]}
		// or content as a plain string. Previously unparsed → codex silently
		// extracted 0 file paths (C2).
		if input, ok := raw["input"].([]any); ok {
			for _, item := range input {
				im, _ := item.(map[string]any)
				if im == nil {
					continue
				}
				switch c := im["content"].(type) {
				case string:
					if c != "" {
						textParts = append(textParts, c)
					}
				case []any:
					for _, block := range c {
						if b, ok := block.(map[string]any); ok {
							if t, ok := b["text"].(string); ok && t != "" {
								textParts = append(textParts, t)
							}
						}
					}
				}
			}
		}

		// Gemini format: systemInstruction.parts[].text
		if si, ok := raw["systemInstruction"].(map[string]any); ok {
			if parts, ok := si["parts"].([]any); ok {
				for _, p := range parts {
					if pm, ok := p.(map[string]any); ok {
						if t, ok := pm["text"].(string); ok {
							textParts = append(textParts, t)
						}
					}
				}
			}
		}

		return extractPaths(strings.Join(textParts, "\n"))
	}

	// Fallback: body is not a plain JSON object (SSE fragments, partial bodies,
	// or non-standard request formats). Extract paths directly from raw text
	// so codex/cursor-style payloads still get file awareness.
	// C2 口径：只有「看起来是 JSON（以 { 开头）但解析失败」才算真失败——
	// 空 body/纯文本（健康检查、未知路径请求被 detectAgent 误标 cursor）不污染指标。
	if len(body) > 0 && body[0] == '{' {
		u.LogShared("INJECT", "file_assoc body_parse=err agent=%s", agent)
	}
	return extractPaths(string(body))
}

// extractPaths finds file-like paths in text. Matches both absolute paths
// and relative paths that end with known code extensions.
func extractPaths(text string) []string {
	cwd, _ := os.Getwd()
	cwdPrefix := cwd + "/"

	var paths []string
	seen := map[string]bool{}

	// Find absolute paths in or near CWD (appear in Claude Code context)
	absRe := regexp.MustCompile(`(?:^|\s|["'<(])/(?:[\w.-]+/)+[\w.-]+\.\w+(?:$|\s|["'>)])`)
	for _, m := range absRe.FindAllString(text, -1) {
		p := strings.TrimSpace(m)
		p = strings.Trim(p, `"'<>()`)
		if strings.HasPrefix(p, cwdPrefix) {
			rel := strings.TrimPrefix(p, cwdPrefix)
			if codeExts.MatchString(rel) && !seen[rel] {
				seen[rel] = true
				paths = append(paths, rel)
			}
		} else if strings.HasPrefix(p, "/") {
			// Absolute path outside CWD (proxy may run from another project):
			// fall back to basename so file→task matching can still work.
			base := filepath.Base(p)
			if codeExts.MatchString(base) && !seen[base] {
				seen[base] = true
				paths = append(paths, base)
			}
		}
	}

	// Find relative paths with code extensions (appear in tool calls, discussions)
	relRe := regexp.MustCompile(`(?:^|\s)((?:[\w.-]+/)*[\w.-]+\.(?:go|js|ts|jsx|tsx|py|rs|java|rb|c|cpp|h|swift|kt|css|html|vue|svelte|sql|sh|yaml|yml|toml|json|md))(?:\s|$|["'>)])`)
	for _, m := range relRe.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 {
			p := strings.TrimSpace(m[1])
			if !seen[p] && codeExts.MatchString(p) {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}

	return paths
}

// detectUserFrustration checks recent discussion_log for user frustration signals.
// Returns warnings if explicit negative feedback is found.
func detectUserFrustration() []string {
	negativeKW := []string{
		"没有变化", "还是不行", "没有效果", "还是不对", "完全没用",
		"你的方式很垃圾", "你在干什么",
	}
	messages, err := store.RecentUserMessages(5)
	if err != nil || len(messages) == 0 {
		return nil
	}
	var warnings []string
	for _, m := range messages {
		content := strings.ToLower(u.Str(m["content"]))
		for _, kw := range negativeKW {
			if strings.Contains(content, kw) {
				preview := u.TruncateStr(u.Str(m["content"]), 80)
				warnings = append(warnings, fmt.Sprintf("\u26a0\ufe0f \u7528\u6237\u53cd\u9988: %s", preview))
				break
			}
		}
	}
	return warnings
}

// loadGuidelines reads .pmai/guidelines.md and returns its content.
// Cached for 10 minutes to avoid repeated filesystem reads.
func loadGuidelines() string {
	guidelinesCache.mu.RLock()
	if time.Since(guidelinesCache.updatedAt) < guidelinesCache.ttl {
		defer guidelinesCache.mu.RUnlock()
		return guidelinesCache.content
	}
	guidelinesCache.mu.RUnlock()

	guidelinesCache.mu.Lock()
	defer guidelinesCache.mu.Unlock()

	dir, err := pmdb.RuntimeDir()
	if err != nil {
		guidelinesCache.content = ""
		return ""
	}
	path := filepath.Join(dir, "guidelines.md")
	f, err := os.Open(path)
	if err != nil {
		guidelinesCache.content = ""
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		guidelinesCache.content = ""
		return ""
	}
	content := strings.TrimSpace(string(data))
	guidelinesCache.content = content
	guidelinesCache.updatedAt = time.Now()
	u.LogShared("GUIDELINES", "loaded %d chars from guidelines.md", len(content))
	return content
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ── Protocol-specific injectors (unchanged) ──────────────────────────

func injectIntoPrompt(body []byte, block string, agent string) []byte {
	switch agent {
	case "claude":
		return injectAnthropic(body, block)
	case "codex":
		return injectCodex(body, block)
	case "gemini":
		return injectGemini(body, block)
	default:
		return injectOpenAI(body, block)
	}
}
func injectAnthropic(body []byte, block string) []byte {
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return body
	}
	messages, _ := raw["messages"].([]any)
	if len(messages) == 0 {
		return body
	}
	for i, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if msg["role"] == "system" {
			content, _ := msg["content"].(string)
			messages[i] = map[string]any{
				"role":    "system",
				"content": content + block,
			}
			raw["messages"] = messages
			b, _ := json.Marshal(raw)
			return b
		}
	}
	messages = append([]any{map[string]any{"role": "system", "content": block}}, messages...)
	raw["messages"] = messages
	b, _ := json.Marshal(raw)
	return b
}

func injectCodex(body []byte, block string) []byte {
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return body
	}
	instructions, _ := raw["instructions"].(string)
	raw["instructions"] = instructions + block
	b, _ := json.Marshal(raw)
	return b
}

func injectGemini(body []byte, block string) []byte {
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return body
	}
	si, _ := raw["systemInstruction"].(map[string]any)
	if si == nil {
		raw["systemInstruction"] = map[string]any{
			"parts": []any{map[string]any{"text": block}},
		}
		b, _ := json.Marshal(raw)
		return b
	}
	parts, _ := si["parts"].([]any)
	if len(parts) == 0 {
		si["parts"] = []any{map[string]any{"text": block}}
	} else if p, ok := parts[0].(map[string]any); ok {
		text, _ := p["text"].(string)
		p["text"] = text + block
	}
	b, _ := json.Marshal(raw)
	return b
}

func injectOpenAI(body []byte, block string) []byte {
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return body
	}
	messages, _ := raw["messages"].([]any)
	if len(messages) == 0 {
		return body
	}
	for i, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if msg["role"] == "system" {
			content, _ := msg["content"].(string)
			messages[i] = map[string]any{
				"role":    "system",
				"content": content + block,
			}
			raw["messages"] = messages
			b, _ := json.Marshal(raw)
			return b
		}
	}
	messages = append([]any{map[string]any{"role": "system", "content": block}}, messages...)
	raw["messages"] = messages
	b, _ := json.Marshal(raw)
	return b
}

// injectSwitchState 返回 AIPMC_INJECT 开关状态，用于 BOOT 日志核验。
func injectSwitchState() string {
	if os.Getenv("AIPMC_INJECT") == "0" {
		return "off"
	}
	return "on"
}
