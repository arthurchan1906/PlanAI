export function statusColor(status) {
  if (["accepted", "active", "done", "in_progress"].includes(status)) return "green";
  if (["verification"].includes(status)) return "blue";
  if (["rejected", "obsolete", "dropped", "blocked"].includes(status)) return "red";
  if (["superseded", "archived"].includes(status)) return "default";
  return "gold";
}

export function planAttentionColor(item) {
  if (item.auto_action_available) return "green";
  if (item.state === "blocked") return "red";
  if (item.state === "verification") return "blue";
  return "gold";
}
