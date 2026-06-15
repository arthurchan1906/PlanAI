package collab



import (

	"fmt"

	"strings"

	"time"



	"aipmc/discussion"

	"aipmc/store"

	"aipmc/u"

)



// AgentNotice is the standard behavior reminder appended to topic prompt output.
// Set via collab.SetAgentNotice() at init time (see skill.go).
var agentNotice = "讨论模式：勿改代码。互读用 aipm_read_discussions(full=true)。禁止 sqlite3。"

// SetAgentNotice updates the standard collaboration reminder text.
func SetAgentNotice(s string) { agentNotice = s }

func agentNoticeBlock() string {
	if agentNotice == "" {
		return ""
	}
	return "[Agent 须知 - 来自 Skill]\n" + agentNotice + "\n"
}

// Catchup prints a PM summary since last visit and updates pm_last_visit_at.

func Catchup(topicID string) error {

	topic, err := store.GetCollaborationTopic(topicID)

	if err != nil {

		return err

	}

	prevVisit, err := store.TouchPMLastVisit(topicID)

	if err != nil {

		return err

	}

	if prevVisit == "" {

		prevVisit = u.Str(topic["created_at"])

	}



	rows, err := store.ReadDiscussions(store.ReadDiscussionsOpts{

		Since:   prevVisit,

		TopicID: topicID,

		LastN:   200,

	})

	if err != nil {

		return err

	}



	leaveLabel := formatTimeLabel(prevVisit)

	fmt.Printf("自你 %s 离开后:\n", leaveLabel)



	groups := groupBySource(rows)

	if len(groups) == 0 {

		fmt.Println("  (无新讨论)")

		return nil

	}

	sources, _ := store.ListDiscussionSources()

	seen := map[string]bool{}

	for _, src := range sources {

		seen[src] = true

		if g, ok := groups[src]; ok {

			printSourceSummary(src, g)

		}

	}

	for src, g := range groups {

		if !seen[src] {

			printSourceSummary(src, g)

		}

	}

	return nil

}



func printSourceSummary(source string, msgs []map[string]any) {

	latest := msgs[len(msgs)-1]

	preview := discussionPreview(u.Str(latest["content"]), 40)

	fmt.Printf("  %-14s +%-2d 条  (最新: %q)\n", source, len(msgs), preview)

}



// Prompt builds the copy-paste block for routing to an agent and logs the route.

func Prompt(topicID, toSource, refsCSV, pmSay string) (string, error) {

	if topicID == "" || toSource == "" || pmSay == "" {

		return "", fmt.Errorf("topic, --to, and --say are required")

	}

	topic, err := store.GetCollaborationTopic(topicID)

	if err != nil {

		return "", err

	}

	since := u.Str(topic["pm_last_visit_at"])

	if since == "" {

		since = u.Str(topic["created_at"])

	}



	var b strings.Builder

	b.WriteString("[协作上下文 - 自动附加]\n")



	refParts := parseRefs(refsCSV)

	for _, ref := range refParts {

		section, err := resolveRef(ref, since, topicID, topic)

		if err != nil {

			return "", err

		}

		if section != "" {

			b.WriteString(section)

		}

	}



	b.WriteString("\n[PM 指令]\n")

	b.WriteString(pmSay)

	b.WriteString("\n\n")

	b.WriteString(agentNoticeBlock())

	b.WriteString("\n")



	out := b.String()

	if _, err := store.LogRoute(store.RouteLogEntry{

		TopicID:        topicID,

		ToSource:       toSource,

		Refs:           refsCSV,

		PMSay:          pmSay,

		PromptSnapshot: out,

	}); err != nil {

		return "", fmt.Errorf("log route: %w", err)

	}

	return out, nil

}



// CloseTopic closes a collaboration topic with optional warnings.

func CloseTopic(topicID string, force bool) (map[string]any, error) {

	topic, err := store.GetCollaborationTopic(topicID)

	if err != nil {

		return nil, err

	}

	if u.Str(topic["status"]) == "closed" {

		return topic, nil

	}

	if !force {

		return nil, fmt.Errorf("未记录 decision，确认关闭？加 --force 继续")

	}

	return store.CloseCollaborationTopic(topicID)

}



func parseRefs(refsCSV string) []string {

	if refsCSV == "" {

		return nil

	}

	var out []string

	for _, p := range strings.Split(refsCSV, ",") {

		p = strings.TrimSpace(p)

		if p != "" {

			out = append(out, p)

		}

	}

	return out

}



func resolveRef(ref, since, topicID string, topic map[string]any) (string, error) {

	switch {

	case ref == "since-last-route":

		routeSince, _ := store.LastRouteSince(topicID)

		if routeSince == "" {

			routeSince = u.Str(topic["created_at"])

		}

		rows, err := store.ReadDiscussions(store.ReadDiscussionsOpts{

			Since:   routeSince,

			TopicID: topicID,

			LastN:   50,

		})

		if err != nil {

			return "", err

		}

		if len(rows) == 0 {

			return fmt.Sprintf("--- 自上次路由 (%s) ---\n(无新消息)\n", formatTimeLabel(routeSince)), nil

		}

		var b strings.Builder

		b.WriteString(fmt.Sprintf("--- 自上次路由 %s (%d 条) ---\n", formatTimeLabel(routeSince), len(rows)))

		b.WriteString(formatGroupedBySource(rows, true))

		return b.String(), nil



	case strings.HasPrefix(ref, "latest:"):

		source := strings.TrimPrefix(ref, "latest:")

		rows, err := store.ReadDiscussions(store.ReadDiscussionsOpts{

			Source:  source,

			Since:   since,

			TopicID: topicID,

			LastN:   20,

		})

		if err != nil {

			return "", err

		}

		if len(rows) == 0 {

			return fmt.Sprintf("--- %s 自上次路由 (0 条) ---\n(无新消息)\n", source), nil

		}

		var b strings.Builder

		b.WriteString(fmt.Sprintf("--- %s 自上次路由 (%d 条) ---\n", source, len(rows)))

		b.WriteString(discussion.FormatResults(rows, true))

		return b.String(), nil



	case strings.HasPrefix(ref, "disc-") || strings.HasPrefix(ref, "disc"):

		rows, err := store.GetDiscussionsByIDs([]string{ref})

		if err != nil {

			return "", err

		}

		if len(rows) == 0 {

			return fmt.Sprintf("--- %s ---\n(未找到)\n", ref), nil

		}

		var b strings.Builder

		b.WriteString(fmt.Sprintf("--- %s ---\n", ref))

		b.WriteString(discussion.FormatResults(rows, true))

		return b.String(), nil

	}

	return "", fmt.Errorf("unknown ref format: %q (use latest:<source>, since-last-route, or disc-id)", ref)

}



func formatGroupedBySource(rows []map[string]any, full bool) string {

	groups := groupBySource(rows)

	var b strings.Builder

	for src, msgs := range groups {

		b.WriteString(fmt.Sprintf("\n### %s (%d 条)\n", src, len(msgs)))

		b.WriteString(discussion.FormatResults(msgs, full))

	}

	return b.String()

}



func groupBySource(rows []map[string]any) map[string][]map[string]any {

	out := map[string][]map[string]any{}

	for _, r := range rows {

		src := u.Str(r["source"])

		if src == "" {

			src = "unknown"

		}

		out[src] = append(out[src], r)

	}

	return out

}



func discussionPreview(s string, maxRunes int) string {

	s = strings.ReplaceAll(s, "\n", " ")

	runes := []rune(s)

	if len(runes) <= maxRunes {

		return s

	}

	return string(runes[:maxRunes]) + "…"

}



func formatTimeLabel(iso string) string {

	t, err := time.Parse("2006-01-02T15:04:05", iso)

	if err != nil {

		if len(iso) >= 16 {

			return iso[11:16]

		}

		return iso

	}

	return t.Format("15:04")

}


