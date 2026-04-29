import { useEffect, useState } from "react";
import {
  App as AntdApp,
  Breadcrumb,
  Button,
  ConfigProvider,
  Layout,
  Menu,
  Typography,
} from "antd";
import {
  BookOutlined,
  BulbOutlined,
  DashboardOutlined,
  FileTextOutlined,
  FundProjectionScreenOutlined,
  ReloadOutlined,
  ScheduleOutlined,
  SettingOutlined,
  BranchesOutlined,
  CompassOutlined,
  SafetyCertificateOutlined,
  CodeOutlined,
  AppstoreOutlined,
  ProjectOutlined,
  TeamOutlined,
} from "@ant-design/icons";

// 导入工具和常量
import { api } from "./utils/api";
import { todayString, buildTaskPayload, buildCanonPayload, buildCommitPayload, buildDocPayload, buildDailyPayload } from "./utils/helpers";
import { NAV_GROUPS, NAV_ITEMS } from "./constants";

// 导入视图组件
import DashboardView from "./views/DashboardView";
import RoadmapView from "./views/RoadmapView";
import VisionsView from "./views/VisionsView";
import PrinciplesView from "./views/PrinciplesView";
import CodeView from "./views/CodeView";
import CanonView from "./views/CanonView";
import TasksView from "./views/TasksView";
import IdeasView from "./views/IdeasView";
import DocsView from "./views/DocsView";
import DecisionsView from "./views/DecisionsView";
import CommitsView from "./views/CommitsView";
import DailyViewHuman from "./views/DailyViewHuman";

const { Header, Sider, Content } = Layout;
const { Title, Text } = Typography;

function getViewFromHash() {
  const raw = window.location.hash.replace(/^#/, "");
  if (raw && NAV_ITEMS.some(i => i.key === raw)) return raw;
  return "dashboard";
}

function ConsoleApp() {
  const { message } = AntdApp.useApp();
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [view, setViewState] = useState(getViewFromHash);

  function setView(key) {
    setViewState(key);
    window.location.hash = key;
  }

  // 数据状态
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
  const [ideas, setIdeas] = useState([]);
  const [docs, setDocs] = useState([]);
  const [docAudit, setDocAudit] = useState(null);
  const [decisions, setDecisions] = useState([]);
  const [daily, setDaily] = useState(null);

  // 搜索/过滤状态
  const [taskSearch, setTaskSearch] = useState("");
  const [taskStatusFilter, setTaskStatusFilter] = useState("");
  const [commitSearch, setCommitSearch] = useState("");
  const [commitStatusFilter, setCommitStatusFilter] = useState("");
  const [commitAttentionFilter, setCommitAttentionFilter] = useState("");
  const [focusedTaskId, setFocusedTaskId] = useState("");
  const [ideaSearch, setIdeaSearch] = useState("");
  const [ideaStatusFilter, setIdeaStatusFilter] = useState("");
  const [focusedIdeaId, setFocusedIdeaId] = useState("");
  const [decisionSearch, setDecisionSearch] = useState("");
  const [decisionStatusFilter, setDecisionStatusFilter] = useState("");

  // 表单状态
  const [taskForm, setTaskForm] = useState({ title: "", acceptance: "", priority: "P1", phase: "general", roadmapId: "", planId: "" });
  const [commitForm, setCommitForm] = useState({ title: "", summary: "", evidenceSummary: "", reviewNotes: "", branch: "", taskId: "", decisionId: "", status: "draft", testStatus: "not_run", reviewStatus: "pending", files: "" });
  const [ideaForm, setIdeaForm] = useState({
    title: "",
    summary: "",
    impact: "",
    current_summary: "",
    main_question: "",
    recommended_next_action: "continue_discussion",
  });
  const [docForm, setDocForm] = useState({ path: "", type: "", status: "draft", layer: "exploration", sourceOfTruth: false });
  const [decisionForm, setDecisionForm] = useState({ title: "", background: "", decision: "" });
  const [dailyForm, setDailyForm] = useState({ noteDate: todayString(), completed: "", problems: "", risks: "", next: "" });
  const [canonForm, setCanonForm] = useState({ decisionId: "", productGoal: "", engineeringFocus: "", architecture: "", addScope: "", addAvoid: "" });
  const [visionForm, setVisionForm] = useState({ id: null, title: "", summary: "", status: "active", horizon: "long_term" });
  const [principleForm, setPrincipleForm] = useState({ id: null, title: "", summary: "", kind: "governance", status: "active" });

  // 数据加载
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

  useEffect(() => {
    function onHashChange() { setViewState(getViewFromHash()); }
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);

  // 通用操作执行
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

  function runDailyAction(method, successMessage) {
    const query = dailyForm.noteDate ? `?date=${encodeURIComponent(dailyForm.noteDate)}` : "";
    return runAction(
      () => api(`/pmai/daily${query}`, { method, body: JSON.stringify(buildDailyPayload(dailyForm)) }),
      successMessage,
    );
  }

  // 模块间交互回调
  function handleConvertIdeaToTask(idea) {
    return runAction(
      () => api(`/pmai/ideas/${idea.id}/convert`, { method: "POST", body: JSON.stringify({ target_type: "task" }) }),
      "Idea converted to task",
    );
  }

  function handleConvertIdeaToDecision(idea) {
    return runAction(
      () => api(`/pmai/ideas/${idea.id}/convert`, { method: "POST", body: JSON.stringify({ target_type: "decision" }) }),
      "Idea converted to decision",
    );
  }

  function handleCommitFiles(files) {
    setCommitForm({ ...commitForm, files: files.join(" | "), title: `提交: ${files[0]}${files.length > 1 ? ` 等 ${files.length} 个文件` : ''}` });
    message.info(`已选择 ${files.length} 个文件`);
    setView("commits");
  }

  function handleViewCommit(commit) {
    message.info(`查看提交详情: ${commit.short_hash || commit.commit_hash?.slice(0, 8)}`);
  }

  function handleCreateTaskFromDaily(item) {
    setTaskForm({ title: item, acceptance: "", priority: "P1", phase: "general" });
    message.info(`已从日报项创建任务表单: ${item}`);
    setView("tasks");
  }

  function handleOpenVisionDetail(vision) {
    message.info(`查看愿景 "${vision.title}" 的关联项`);
  }

  function handleOpenIdea(ideaId) {
    setFocusedIdeaId(ideaId || "");
    setIdeaSearch(ideaId || "");
    setIdeaStatusFilter("");
    setView("ideas");
  }

  function handleOpenCommitsForTask(task) {
    setFocusedTaskId(task?.id || "");
    setCommitForm((current) => ({
      ...current,
      taskId: task?.id || current.taskId,
      title: task?.title ? `Evidence for ${task.title}` : current.title,
    }));
    setCommitSearch("");
    setCommitStatusFilter("");
    setCommitAttentionFilter("");
    setView("commits");
  }

  function handleOpenCommitAttention(attention) {
    setFocusedTaskId("");
    setCommitSearch("");
    setCommitStatusFilter("");
    setCommitAttentionFilter(attention || "");
    setView("commits");
  }

  // 渲染视图
  return (
    <Layout className="console-layout">
      <Sider width={180} className="console-sider" breakpoint="lg" collapsedWidth="0">
        <div className="brand-block"><div className="brand-mark">PM</div></div>
        <Menu 
          theme="dark" 
          mode="inline" 
          selectedKeys={[view]} 
          items={NAV_GROUPS.map(group => ({
            key: group.key,
            type: 'group',
            label: group.label,
            children: group.children.map(item => ({
              key: item.key,
              label: item.label,
              icon: item.icon,
            }))
          }))}
          onClick={({ key }) => setView(key)} 
        />
      </Sider>
      <Layout>
        <Header className="console-header">
          <div>
            <Breadcrumb
              items={[
                { title: "工作台", onClick: () => setView("dashboard") },
                { title: NAV_ITEMS.find(i => i.key === view)?.label }
              ]}
              style={{ marginBottom: 8 }}
            />
            <Title level={3} className="header-title">{NAV_ITEMS.find(i => i.key === view)?.label}</Title>
          </div>
          <Button icon={<ReloadOutlined />} onClick={loadAll} loading={busy}>刷新</Button>
        </Header>
        <Content className="console-content">
          {view === "dashboard" && <DashboardView visions={visions} principles={principles} ideas={ideas} dashboard={dashboard} aiContext={aiContext} nextPacket={nextPacket} handoff={handoff} inbox={inbox} canon={canon} loading={loading} onOpenCanon={id => { setCanonForm({...canonForm, decisionId: id || ""}); setView("canon"); }} onOpenDecisions={() => setView("decisions")} onOpenTasks={() => setView("tasks")} onOpenCommits={() => setView("commits")} onOpenCommitAttention={handleOpenCommitAttention} onOpenIdeas={() => setView("ideas")} onOpenDocs={() => setView("docs")} onOpenDaily={() => setView("daily")} onOpenPrinciples={() => setView("principles")} />}
          {view === "planning" && (
            <RoadmapView
              roadmaps={roadmaps}
              plans={plans}
              visions={visions}
              tasks={tasks}
              taskNotes={taskNotes}
              commits={commits}
              docs={docs}
              ideas={ideas}
              busy={busy}
              onCreateRoadmap={(payload) => runAction(() => api("/pmai/roadmaps", { method: "POST", body: JSON.stringify(payload) }), "Roadmap created")}
              onGeneratePlan={(payload) => runAction(() => api("/pmai/plans/generate", { method: "POST", body: JSON.stringify(payload) }), payload.create_tasks ? "Plan and tasks generated" : "Plan generated")}
              onAdvancePlan={(planId) => runAction(() => api(`/pmai/plans/${planId}/advance`, { method: "POST", body: "{}" }), "Plan advanced")}
            />
          )}
          {view === "visions" && <VisionsView visions={visions} visionForm={visionForm} setVisionForm={setVisionForm} busy={busy} onCreateVision={p => runAction(() => api(p.id ? `/pmai/visions/${p.id}` : "/pmai/visions", { method: p.id ? "PATCH" : "POST", body: JSON.stringify(p) }), "Vision updated")} onUpdateVision={(id, p) => runAction(() => api(`/pmai/visions/${id}`, { method: "PATCH", body: JSON.stringify(p) }), "Vision updated")} onOpenVisionDetail={handleOpenVisionDetail} tasks={tasks} decisions={decisions} />}
          {view === "principles" && <PrinciplesView principles={principles} principleForm={principleForm} setPrincipleForm={setPrincipleForm} busy={busy} onCreatePrinciple={p => runAction(() => api(p.id ? `/pmai/principles/${p.id}` : "/pmai/principles", { method: p.id ? "PATCH" : "POST", body: JSON.stringify(p) }), "Principle updated")} onUpdatePrinciple={(id, p) => runAction(() => api(`/pmai/principles/${id}`, { method: "PATCH", body: JSON.stringify(p) }), "Principle updated")} tasks={tasks} decisions={decisions} />}
          {view === "code" && <CodeView codeStatus={codeStatus} recentCommits={recentGitCommits} loading={loading} onCommitFiles={handleCommitFiles} onViewCommit={handleViewCommit} />}
          {view === "tasks" && (
            <TasksView
              tasks={tasks}
              taskNotes={taskNotes}
              commits={commits}
              roadmaps={roadmaps}
              plans={plans}
              docs={docs}
              taskSearch={taskSearch}
              taskStatusFilter={taskStatusFilter}
              setTaskSearch={setTaskSearch}
              setTaskStatusFilter={setTaskStatusFilter}
              taskForm={taskForm}
              setTaskForm={setTaskForm}
              busy={busy}
              onOpenIdea={handleOpenIdea}
              onOpenCommitsForTask={handleOpenCommitsForTask}
              onCreateTask={() => runAction(() => api("/pmai/tasks", { method: "POST", body: JSON.stringify(buildTaskPayload(taskForm)) }), "Task created")}
              onUpdateTask={(id, s) => runAction(() => api(`/pmai/tasks/${id}`, { method: "PATCH", body: JSON.stringify({ status: s }) }), "Task updated")}
            />
          )}
          {view === "canon" && <CanonView canon={canon} decisions={decisions} canonForm={canonForm} setCanonForm={setCanonForm} busy={busy} onSubmitCanon={() => runAction(() => api("/pmai/canon/update", { method: "POST", body: JSON.stringify(buildCanonPayload(canonForm)) }), "Canon updated")} />}
          {view === "commits" && <CommitsView commits={commits} tasks={tasks} decisions={decisions} commitSearch={commitSearch} commitStatusFilter={commitStatusFilter} commitAttentionFilter={commitAttentionFilter} setCommitSearch={setCommitSearch} setCommitStatusFilter={setCommitStatusFilter} setCommitAttentionFilter={setCommitAttentionFilter} commitForm={commitForm} setCommitForm={setCommitForm} focusedTaskId={focusedTaskId} busy={busy} onCreateCommit={() => runAction(() => api("/pmai/commits", { method: "POST", body: JSON.stringify(buildCommitPayload(commitForm)) }), "Commit registered")} onUpdateCommit={(id, p) => runAction(() => api(`/pmai/commits/${id}`, { method: "PATCH", body: JSON.stringify(p) }), "Commit updated")} />}
          {view === "ideas" && (
            <IdeasView
              ideas={ideas}
              ideaSearch={ideaSearch}
              ideaStatusFilter={ideaStatusFilter}
              setIdeaSearch={setIdeaSearch}
              setIdeaStatusFilter={setIdeaStatusFilter}
              ideaForm={ideaForm}
              setIdeaForm={setIdeaForm}
              busy={busy}
              onCreateIdea={() => runAction(() => api("/pmai/ideas", { method: "POST", body: JSON.stringify(ideaForm) }), "Idea created")}
              onUpdateIdea={(id, payload) => runAction(() => api(`/pmai/ideas/${id}`, { method: "PATCH", body: JSON.stringify(payload) }), "Idea updated")}
              onCommentIdea={(id, payload) => runAction(() => api(`/pmai/ideas/${id}/comments`, { method: "POST", body: JSON.stringify(payload) }), "Comment added")}
              onConvertToTask={handleConvertIdeaToTask}
              onConvertToDecision={handleConvertIdeaToDecision}
              focusedIdeaId={focusedIdeaId}
            />
          )}
          {view === "docs" && (
            <DocsView
              docs={docs}
              docAudit={docAudit}
              docForm={docForm}
              setDocForm={setDocForm}
              busy={busy}
              onSubmitDoc={() => runAction(() => api("/pmai/docs", { method: "PATCH", body: JSON.stringify(buildDocPayload(docForm)) }), "Doc updated")}
              onSyncDocs={() => runAction(() => api("/pmai/docs/sync", { method: "POST" }), "Directory synced")}
              onRepairDocs={() => runAction(() => api("/pmai/docs/repair", { method: "POST" }), "Doc records repaired")}
              onPruneDocs={() => runAction(() => api("/pmai/docs/prune", { method: "POST" }), "Archive pruned")}
            />
          )}
          {view === "decisions" && <DecisionsView decisions={decisions} decisionSearch={decisionSearch} decisionStatusFilter={decisionStatusFilter} setDecisionSearch={setDecisionSearch} setDecisionStatusFilter={setDecisionStatusFilter} decisionForm={decisionForm} setDecisionForm={setDecisionForm} busy={busy} onOpenIdea={handleOpenIdea} onCreateDecision={() => runAction(() => api("/pmai/decisions", { method: "POST", body: JSON.stringify(decisionForm) }), "Decision created")} onUpdateDecision={(id, s) => runAction(() => api(`/pmai/decisions/${id}`, { method: "PATCH", body: JSON.stringify({ status: s }) }), "Decision updated")} onCopyIntoCanon={id => { setCanonForm({...canonForm, decisionId: id}); setView("canon"); }} />}
          {view === "daily" && <DailyViewHuman daily={daily} dailyForm={dailyForm} setDailyForm={setDailyForm} busy={busy} onAppendDaily={() => runDailyAction("POST", "Daily note updated")} onReplaceDaily={() => runDailyAction("PUT", "Daily note replaced")} tasks={tasks} commits={commits} onCreateTaskFromDaily={handleCreateTaskFromDaily} />}
        </Content>
      </Layout>
    </Layout>
  );
}

export default function App() {
  return (
    <ConfigProvider theme={{ token: { colorPrimary: "#2f6fec", borderRadius: 16 } }}>
      <AntdApp><ConsoleApp /></AntdApp>
    </ConfigProvider>
  );
}
