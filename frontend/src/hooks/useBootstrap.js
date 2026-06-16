import { useCallback, useEffect, useRef, useState } from "react";

const VIEW_ENDPOINTS = {
  planning: "/pmai/web/planning",
  commits: "/pmai/web/commits",
  bugs: "/pmai/web/bugs",
  decisions: "/pmai/web/decisions",
  ideas: "/pmai/web/ideas",
  docs: "/pmai/web/docs",
  threads: "/pmai/web/threads",
  agents: "/pmai/web/agents",
  audit: "/pmai/web/audit",
  code: "/pmai/web/code",
  daily: "/pmai/web/daily",
};

function mergePayload(setters, payload) {
  if (payload.roadmaps != null) setters.setRoadmaps(payload.roadmaps);
  if (payload.plans != null) setters.setPlans(payload.plans);
  if (payload.visions != null) setters.setVisions(payload.visions);
  if (payload.tasks != null) setters.setTasks(payload.tasks);
  if (payload.task_notes != null) setters.setTaskNotes(payload.task_notes);
  if (payload.commits != null) setters.setCommits(payload.commits);
  if (payload.docs != null) setters.setDocs(payload.docs);
  if (payload.ideas != null) setters.setIdeas(payload.ideas);
  if (payload.bugs != null) setters.setBugs(payload.bugs);
  if (payload.decisions != null) setters.setDecisions(payload.decisions);
  if (payload.principles != null) setters.setPrinciples(payload.principles);
  if (payload.canon != null) setters.setCanon(payload.canon);
  if (payload.doc_audit != null) setters.setDocAudit(payload.doc_audit);
  if (payload.daily != null) setters.setDaily(payload.daily);
  if (payload.threads != null) setters.setThreads(payload.threads);
  if (payload.thread_suggestions != null) setters.setThreadSuggestions(payload.thread_suggestions);
  if (payload.thread_status != null) setters.setThreadStatus(payload.thread_status);
  if (payload.agents != null) setters.setAgents(payload.agents);
  if (payload.audit_logs != null) setters.setAuditLogs(payload.audit_logs);
  if (payload.code_status != null) setters.setCodeStatus(payload.code_status);
  if (payload.recent_git_commits != null) setters.setRecentGitCommits(payload.recent_git_commits);
}

export default function useBootstrap(api, message, view) {
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const loadedRef = useRef(new Set());

  const [dashboard, setDashboard] = useState(null);
  const [aiContext, setAiContext] = useState(null);
  const [nextPacket, setNextPacket] = useState(null);
  const [handoff, setHandoff] = useState(null);
  const [inbox, setInbox] = useState(null);
  const [canon, setCanon] = useState(null);
  const [visions, setVisions] = useState([]);
  const [roadmaps, setRoadmaps] = useState([]);
  const [plans, setPlans] = useState([]);
  const [principles, setPrinciples] = useState([]);
  const [codeStatus, setCodeStatus] = useState(null);
  const [recentGitCommits, setRecentGitCommits] = useState([]);
  const [tasks, setTasks] = useState([]);
  const [taskNotes, setTaskNotes] = useState([]);
  const [commits, setCommits] = useState([]);
  const [bugs, setBugs] = useState([]);
  const [ideas, setIdeas] = useState([]);
  const [docs, setDocs] = useState([]);
  const [docAudit, setDocAudit] = useState(null);
  const [decisions, setDecisions] = useState([]);
  const [daily, setDaily] = useState(null);
  const [threads, setThreads] = useState([]);
  const [threadSuggestions, setThreadSuggestions] = useState([]);
  const [threadStatus, setThreadStatus] = useState([]);
  const [agents, setAgents] = useState([]);
  const [auditLogs, setAuditLogs] = useState([]);

  const setters = {
    setRoadmaps, setPlans, setVisions, setTasks, setTaskNotes, setCommits,
    setDocs, setIdeas, setBugs, setDecisions, setPrinciples, setCanon,
    setDocAudit, setDaily, setThreads, setThreadSuggestions, setThreadStatus,
    setAgents, setAuditLogs, setCodeStatus, setRecentGitCommits,
  };

  const loadView = useCallback(async (viewKey, { force = false } = {}) => {
    const endpoint = VIEW_ENDPOINTS[viewKey];
    if (!endpoint) {
      setLoading(false);
      return;
    }
    if (loadedRef.current.has(viewKey) && !force) {
      return;
    }
    setLoading(true);
    try {
      const payload = await api(endpoint);
      mergePayload(setters, payload);
      loadedRef.current.add(viewKey);
    } catch (error) {
      message.error(error.message || "Load failed");
    } finally {
      setLoading(false);
    }
  }, [api, message]);

  useEffect(() => {
    loadView(view);
  }, [view, loadView]);

  async function loadAll(targetView) {
    const key = targetView || view;
    await loadView(key, { force: true });
  }

  async function runAction(action, successMessage, refreshView) {
    setBusy(true);
    try {
      await action();
      await loadView(refreshView || view, { force: true });
      message.success(successMessage);
    } catch (error) {
      message.error(error.message || "Action failed");
    } finally {
      setBusy(false);
    }
  }

  return {
    loading, busy,
    dashboard, aiContext, nextPacket, handoff, inbox,
    canon, visions, roadmaps, plans, principles,
    codeStatus, recentGitCommits,
    tasks, taskNotes, commits, bugs, ideas, docs, docAudit, decisions, daily,
    agents, auditLogs,
    threads, threadSuggestions, threadStatus,
    loadAll, runAction,
  };
}
