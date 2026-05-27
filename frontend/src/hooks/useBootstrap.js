import { useEffect, useState } from "react";

export default function useBootstrap(api, message) {
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);

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

  async function loadAll() {
    setLoading(true);
    try {
      const payload = await api("/pmai/web/bootstrap");
      setDashboard(payload.dashboard || null);
      setAiContext(payload.ai_context || null);
      setNextPacket(payload.next_packet || null);
      setHandoff(payload.handoff || null);
      setInbox(payload.inbox || null);
      setCanon(payload.canon || null);
      setVisions(payload.visions || []);
      setRoadmaps(payload.roadmaps || []);
      setPlans(payload.plans || []);
      setPrinciples(payload.principles || []);
      setCodeStatus(payload.code_status || null);
      setRecentGitCommits(payload.recent_git_commits || []);
      setTasks(payload.tasks || []);
      setTaskNotes(payload.task_notes || []);
      setCommits(payload.commits || []);
      setBugs(payload.bugs || []);
      setIdeas(payload.ideas || []);
      setDocs(payload.docs || []);
      setDocAudit(payload.doc_audit || null);
      setDecisions(payload.decisions || []);
      setDaily(payload.daily || null);
    } catch (error) {
      message.error(error.message || "Load failed");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { loadAll(); }, []);

  async function runAction(action, successMessage) {
    setBusy(true);
    try {
      await action();
      await loadAll();
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
    loadAll, runAction,
  };
}
