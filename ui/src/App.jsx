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

function ConsoleApp() {
  const { message } = AntdApp.useApp();
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [view, setView] = useState("dashboard");

  // 数据状态
  const [dashboard, setDashboard] = useState(null);
  const [inbox, setInbox] = useState(null);
  const [canon, setCanon] = useState(null);
  const [visions, setVisions] = useState([]);
  const [roadmaps, setRoadmaps] = useState([]);
  const [plans, setPlans] = useState([]);
  const [principles, setPrinciples] = useState([]);
  const [codeStatus, setCodeStatus] = useState(null);
  const [recentGitCommits, setRecentGitCommits] = useState([]);
  const [tasks, setTasks] = useState([]);
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
  const [ideaSearch, setIdeaSearch] = useState("");
  const [ideaStatusFilter, setIdeaStatusFilter] = useState("");
  const [decisionSearch, setDecisionSearch] = useState("");
  const [decisionStatusFilter, setDecisionStatusFilter] = useState("");

  // 表单状态
  const [taskForm, setTaskForm] = useState({ title: "", acceptance: "", priority: "P1", phase: "general", roadmapId: "", planId: "" });
  const [commitForm, setCommitForm] = useState({ title: "", summary: "", branch: "", taskId: "", decisionId: "", status: "draft", testStatus: "not_run", reviewStatus: "pending", files: "" });
  const [ideaForm, setIdeaForm] = useState({ title: "", summary: "", impact: "" });
  const [docForm, setDocForm] = useState({ path: "", type: "", status: "draft", layer: "exploration", sourceOfTruth: false, relatedDecisionId: undefined });
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
      setInbox(payload.inbox || null);
      setCanon(payload.canon || null);
      setVisions(payload.visions || []);
      setRoadmaps(payload.roadmaps || []);
      setPlans(payload.plans || []);
      setPrinciples(payload.principles || []);
      setCodeStatus(payload.code_status || null);
      setRecentGitCommits(payload.recent_git_commits || []);
      setTasks(payload.tasks || []);
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
    setTaskForm({ title: idea.title, acceptance: "", priority: "P1", phase: "general" });
    message.info(`已从想法 "${idea.title}" 创建任务表单`);
    setView("tasks");
  }

  function handleConvertIdeaToDecision(idea) {
    setDecisionForm({ title: idea.title, background: idea.summary, decision: "" });
    message.info(`已从想法 "${idea.title}" 创建决策表单`);
    setView("decisions");
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
          {view === "dashboard" && <DashboardView visions={visions} principles={principles} dashboard={dashboard} inbox={inbox} canon={canon} loading={loading} onOpenCanon={id => { setCanonForm({...canonForm, decisionId: id || ""}); setView("canon"); }} onOpenDecisions={() => setView("decisions")} onOpenTasks={() => setView("tasks")} onOpenCommits={() => setView("commits")} onOpenIdeas={() => setView("ideas")} onOpenDocs={() => setView("docs")} onOpenDaily={() => setView("daily")} onOpenPrinciples={() => setView("principles")} />}
          {view === "planning" && (
            <RoadmapView
              roadmaps={roadmaps}
              plans={plans}
              visions={visions}
              tasks={tasks}
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
              roadmaps={roadmaps}
              plans={plans}
              taskSearch={taskSearch}
              taskStatusFilter={taskStatusFilter}
              setTaskSearch={setTaskSearch}
              setTaskStatusFilter={setTaskStatusFilter}
              taskForm={taskForm}
              setTaskForm={setTaskForm}
              busy={busy}
              onCreateTask={() => runAction(() => api("/pmai/tasks", { method: "POST", body: JSON.stringify(buildTaskPayload(taskForm)) }), "Task created")}
              onUpdateTask={(id, s) => runAction(() => api(`/pmai/tasks/${id}`, { method: "PATCH", body: JSON.stringify({ status: s }) }), "Task updated")}
            />
          )}
          {view === "canon" && <CanonView canon={canon} decisions={decisions} canonForm={canonForm} setCanonForm={setCanonForm} busy={busy} onSubmitCanon={() => runAction(() => api("/pmai/canon/update", { method: "POST", body: JSON.stringify(buildCanonPayload(canonForm)) }), "Canon updated")} />}
          {view === "commits" && <CommitsView commits={commits} tasks={tasks} decisions={decisions} commitSearch={commitSearch} commitStatusFilter={commitStatusFilter} setCommitSearch={setCommitSearch} setCommitStatusFilter={setCommitStatusFilter} commitForm={commitForm} setCommitForm={setCommitForm} busy={busy} onCreateCommit={() => runAction(() => api("/pmai/commits", { method: "POST", body: JSON.stringify(buildCommitPayload(commitForm)) }), "Commit registered")} onUpdateCommit={(id, p) => runAction(() => api(`/pmai/commits/${id}`, { method: "PATCH", body: JSON.stringify(p) }), "Commit updated")} />}
          {view === "ideas" && <IdeasView ideas={ideas} ideaSearch={ideaSearch} ideaStatusFilter={ideaStatusFilter} setIdeaSearch={setIdeaSearch} setIdeaStatusFilter={setIdeaStatusFilter} ideaForm={ideaForm} setIdeaForm={setIdeaForm} busy={busy} onCreateIdea={() => runAction(() => api("/pmai/ideas", { method: "POST", body: JSON.stringify(ideaForm) }), "Idea created")} onUpdateIdea={(id, s) => runAction(() => api(`/pmai/ideas/${id}`, { method: "PATCH", body: JSON.stringify({ status: s }) }), "Idea updated")} onConvertToTask={handleConvertIdeaToTask} onConvertToDecision={handleConvertIdeaToDecision} />}
          {view === "docs" && <DocsView docs={docs} docAudit={docAudit} docForm={docForm} setDocForm={setDocForm} busy={busy} onSubmitDoc={() => runAction(() => api("/pmai/docs", { method: "PATCH", body: JSON.stringify(buildDocPayload(docForm)) }), "Doc updated")} decisions={decisions} />}
          {view === "decisions" && <DecisionsView decisions={decisions} decisionSearch={decisionSearch} decisionStatusFilter={decisionStatusFilter} setDecisionSearch={setDecisionSearch} setDecisionStatusFilter={setDecisionStatusFilter} decisionForm={decisionForm} setDecisionForm={setDecisionForm} busy={busy} onCreateDecision={() => runAction(() => api("/pmai/decisions", { method: "POST", body: JSON.stringify(decisionForm) }), "Decision created")} onUpdateDecision={(id, s) => runAction(() => api(`/pmai/decisions/${id}`, { method: "PATCH", body: JSON.stringify({ status: s }) }), "Decision updated")} onCopyIntoCanon={id => { setCanonForm({...canonForm, decisionId: id}); setView("canon"); }} />}
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
