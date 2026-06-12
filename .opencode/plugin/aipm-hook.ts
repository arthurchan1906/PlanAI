// AIPM Hook Plugin for OpenCode
// Captures conversation and tool use to AIPM discussion_log.
// Requires: npm install @opencode-ai/plugin (in project or global)
import type { Plugin } from "@opencode-ai/plugin"
import { spawnSync } from "node:child_process"

export const AipmHook: Plugin = async () => {
  return {
    event: async ({ event }) => {
      const { type, data } = event as { type: string; data: Record<string, unknown> }

      // Build hook payload compatible with aipmc hook-opencode
      const payload: Record<string, unknown> = {
        hook_event_name: type,
        session_id: data.sessionID || data.session_id || "",
      }

      switch (type) {
        case "message.updated": {
          const msg = data.message as { role: string; content: string } | undefined
          if (msg?.role === "user") {
            payload.prompt = msg.content || ""
          } else {
            return // ignore assistant message parts
          }
          break
        }
        case "session.idle": {
          const lastMsg = data.lastAssistantMessage || data.response || ""
          if (!lastMsg) return
          payload.last_assistant_message = lastMsg
          break
        }
        case "tool.execute.after": {
          payload.tool_name = data.tool || data.tool_name || ""
          payload.tool_input = data.input || data.args || {}
          payload.tool_response = data.output || data.result || {}
          break
        }
        default:
          return // ignore other events
      }

      try {
        spawnSync("aipmc", ["hook-opencode"], {
          input: JSON.stringify(payload),
          timeout: 10000,
          stdio: ["pipe", "ignore", "pipe"],
        })
      } catch {
        // Never crash OpenCode
      }
    },
  }
}
