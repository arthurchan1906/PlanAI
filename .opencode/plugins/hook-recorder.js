// OpenCode Hook Recorder — bridges opencode plugin events to aipmc
export const HookRecorder = async ({ $ }) => {
  const sendHook = async (payload) => {
    try {
      await $`echo ${JSON.stringify(payload)} | aipmc hook-opencode`
    } catch (_) {}
  }

  // Buffer pending text parts keyed by messageID, with age tracking
  const pending = {}
  const lastRole = {}  // sessionID → last known role

  const flushSession = (sid, fallbackRole) => {
    const texts = []
    for (const [key, part] of Object.entries(pending)) {
      if (part.sid === sid && part.text) {
        texts.push(part.text)
      }
      delete pending[key]
    }
    return { content: texts.join("\n"), role: fallbackRole || lastRole[sid] || "" }
  }

  // Cleanup stale buffers every 30s
  setInterval(() => {
    const now = Date.now()
    for (const [key, part] of Object.entries(pending)) {
      if (now - part.ts > 30000) {
        if (part.text) {
          sendHook({
            hook_event_name: "message.part.updated",
            session_id: part.sid,
            role: lastRole[part.sid] || "assistant",
            content: part.text,
          })
        }
        delete pending[key]
      }
    }
  }, 30000)

  return {
    event: async ({ event }) => {
      const evt = event || {}
      const type = evt?.type || ""
      const props = evt?.properties || {}

      if (type === "message.updated") {
        const info = props?.info || {}
        const role = info?.role || ""
        const sid = props?.sessionID || evt?.sessionID || ""
        if (!role || !sid) return

        lastRole[sid] = role

        const flushed = flushSession(sid, role)
        if (flushed.content) {
          await sendHook({
            hook_event_name: "message.updated",
            session_id: sid,
            role: role,
            content: flushed.content,
            _raw: evt,
          })
        }
      }

      if (type === "message.part.updated") {
        const part = props?.part || {}
        const sid = props?.sessionID || part?.sessionID || ""
        const msgId = part?.messageID || ""
        if (!part?.text || part?.type !== "text") return

        if (part?.role) {
          lastRole[sid] = part.role
          await sendHook({
            hook_event_name: "message.part.updated",
            session_id: sid,
            role: part.role,
            content: part.text,
            _raw: evt,
          })
        } else {
          const key = msgId + "-" + (part?.id || Math.random())
          pending[key] = { sid, text: part.text, ts: Date.now() }
        }
      }
    },

    "tool.execute.after": async (...args) => {
      const input = args[0] || {}
      const output = args[1] || {}
      await sendHook({
        hook_event_name: "tool.execute.after",
        session_id: input.sessionID || input.session?.id || input.session_id || "",
        tool_name: input.tool || input.tool_name || "",
        tool_input: input.args || input.tool_input || {},
        tool_response: output.result || output.tool_response || output,
        _raw_input: input,
        _raw_output: output,
      })
    },

    "session.idle": async (...args) => {
      const evt = args[0] || {}
      const sid = evt.sessionID || evt.session?.id || evt.id || evt.session_id || ""
      const flushed = flushSession(sid, lastRole[sid] || "assistant")
      if (flushed.content) {
        await sendHook({
          hook_event_name: "message.part.updated",
          session_id: sid,
          role: flushed.role,
          content: flushed.content,
        })
      }
      await sendHook({
        hook_event_name: "session.idle",
        session_id: sid,
        _raw: evt,
      })
    },
  }
}
