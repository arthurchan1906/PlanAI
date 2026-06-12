// OpenCode Hook Recorder — bridges opencode plugin events to aipmc
export const HookRecorder = async ({ $ }) => {
  const sendHook = async (payload) => {
    try {
      await $`echo ${JSON.stringify(payload)} | aipmc hook-opencode`
    } catch (_) {}
  }

  // Buffer: accumulate pending text parts keyed by messageID
  const pendingParts = {}

  return {
    event: async ({ event }) => {
      const evt = event || {}
      const type = evt?.type || ""
      const props = evt?.properties || {}

      // message.updated: fires with role metadata
      if (type === "message.updated") {
        const info = props?.info || {}
        const role = info?.role || ""
        const msgId = evt?.id || ""
        const sid = props?.sessionID || evt?.sessionID || ""

        if (role) {
          // Flush any pending text parts for this session
          const texts = []
          for (const [key, part] of Object.entries(pendingParts)) {
            if (part.sid === sid) {
              texts.push(part.text)
              delete pendingParts[key]
            }
          }
          const content = texts.join("\n") || ""

          await sendHook({
            hook_event_name: "message.updated",
            session_id: sid,
            role: role,
            content: content,
            message_id: msgId,
            _raw: evt,
          })
        }
      }

      // message.part.updated: fires with actual text content
      if (type === "message.part.updated") {
        const part = props?.part || {}
        const sid = props?.sessionID || part?.sessionID || ""
        const msgId = part?.messageID || ""

        if (part?.type === "text" && part?.text) {
          // Buffer this text part until message.updated gives us the role
          const key = msgId + "-" + (part?.id || Date.now())
          pendingParts[key] = { sid, text: part.text, role: part?.role }

          // If part has its own role, send immediately
          if (part?.role) {
            delete pendingParts[key]
            await sendHook({
              hook_event_name: "message.part.updated",
              session_id: sid,
              role: part.role,
              content: part.text,
              message_id: msgId,
              _raw: evt,
            })
          }
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
      // Flush any remaining pending parts
      for (const [key, part] of Object.entries(pendingParts)) {
        await sendHook({
          hook_event_name: "message.part.updated",
          session_id: part.sid,
          role: part.role || "assistant",
          content: part.text,
          _raw: {},
        })
        delete pendingParts[key]
      }
      await sendHook({
        hook_event_name: "session.idle",
        session_id: evt.sessionID || evt.session?.id || evt.id || evt.session_id || "",
        session: evt.session || {},
        status: evt.status || "",
        _raw: evt,
      })
    },
  }
}
