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
