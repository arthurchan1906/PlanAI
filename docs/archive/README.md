# Archived design docs

These documents describe **removed** features (v0 meeting rooms, v1 topic/collab CLI, assignment MCP tools). The current model:

- **Discussion log** — hooks capture agent sessions; `aipm_read_discussions` for cross-agent context
- **Briefing** — single shared `aipm_get_briefing` (no per-agent filter)
- **PM collaboration** — natural language in any agent window; no topic catchup/prompt CLI

See `skill.go` / installed `pmai` skill for the live workflow.
