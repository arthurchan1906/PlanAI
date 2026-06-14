// API 请求封装

export async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `Request failed: ${response.status}`);
  }
  return response.json();
}

// Chat API helpers
export async function chatSendStream(message, sessionId, handlers = {}) {
  console.log("[chatSendStream] calling /pmai/chat/send/stream");
  const response = await fetch("/pmai/chat/send/stream", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ message, session_id: sessionId }),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `Request failed: ${response.status}`);
  }
  if (!response.body) {
    throw new Error("Streaming not supported");
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const blocks = buffer.split("\n\n");
    buffer = blocks.pop() || "";
    for (const block of blocks) {
      if (!block.trim()) continue;
      let event = "message";
      let data = "";
      for (const line of block.split("\n")) {
        if (line.startsWith("event: ")) event = line.slice(7).trim();
        else if (line.startsWith("data: ")) data = line.slice(6);
      }
      if (!data) continue;
      let payload;
      try {
        payload = JSON.parse(data);
      } catch {
        continue;
      }
      switch (event) {
        case "token":
          handlers.onToken?.(payload.content || "");
          break;
        case "tool_start":
          handlers.onToolStart?.(payload);
          break;
        case "tool_result":
          handlers.onToolResult?.(payload);
          break;
        case "done":
          handlers.onDone?.(payload);
          break;
        case "error":
          handlers.onError?.(payload.message || "Agent error");
          break;
        default:
          break;
      }
    }
  }
}

export async function chatSend(message, sessionId) {
  return api("/pmai/chat/send", {
    method: "POST",
    body: JSON.stringify({ message, session_id: sessionId }),
  });
}

export async function chatGetSessions() {
  return api("/pmai/chat/sessions");
}

export async function chatGetSession(id) {
  return api(`/pmai/chat/session?id=${encodeURIComponent(id)}`);
}
