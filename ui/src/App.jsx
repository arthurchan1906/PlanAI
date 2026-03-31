import { useEffect, useMemo, useState } from "react";
import {
  App as AntdApp,
  Badge,
  Button,
  Card,
  Col,
  ConfigProvider,
  Empty,
  Form,
  Input,
  Layout,
  List,
  Menu,
  Row,
  Select,
  Space,
  Spin,
  Statistic,
  Table,
  Tag,
  Typography,
} from "antd";
import {
  BookOutlined,
  BulbOutlined,
  CheckCircleOutlined,
  DashboardOutlined,
  FileTextOutlined,
  FundProjectionScreenOutlined,
  ReloadOutlined,
  ScheduleOutlined,
  SettingOutlined,
  BranchesOutlined,
} from "@ant-design/icons";

const { Header, Sider, Content } = Layout;
const { Title, Paragraph, Text } = Typography;
const { TextArea } = Input;

const VIEW_ITEMS = [
  { key: "dashboard", label: "总览", icon: <DashboardOutlined /> },
  { key: "canon", label: "规范", icon: <SettingOutlined /> },
  { key: "tasks", label: "任务", icon: <ScheduleOutlined /> },
  { key: "ideas", label: "想法", icon: <BulbOutlined /> },
  { key: "docs", label: "文档", icon: <BookOutlined /> },
  { key: "decisions", label: "决策", icon: <FundProjectionScreenOutlined /> },
  { key: "daily", label: "日报", icon: <FileTextOutlined /> },
];

const TASK_STATUSES = ["todo", "in_progress", "blocked", "done", "dropped"];
const COMMIT_STATUSES = ["draft", "committed", "merged", "released", "dropped"];
const COMMIT_TEST_STATUSES = ["not_run", "passed", "failed"];
const COMMIT_REVIEW_STATUSES = ["pending", "approved", "changes_requested"];
const IDEA_STATUSES = ["inbox", "under_review", "accepted", "rejected", "obsolete"];
const DECISION_STATUSES = ["proposed", "accepted", "rejected", "superseded"];
const DOC_STATUSES = ["draft", "active", "archived", "obsolete"];
const DOC_LAYERS = ["baseline", "decision", "task", "exploration", "history", "topic"];
const NAV_ITEMS = [
  ...VIEW_ITEMS.slice(0, 3),
  { key: "commits", label: "提交", icon: <BranchesOutlined /> },
  ...VIEW_ITEMS.slice(3),
];

async function api(path, options = {}) {
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

function splitValues(raw) {
  return String(raw || "")
    .split("|")
    .map((item) => item.trim())
    .filter(Boolean);
}

function statusColor(status) {
  if (["accepted", "active", "done", "in_progress"].includes(status)) return "green";
  if (["rejected", "obsolete", "dropped", "blocked"].includes(status)) return "red";
  if (["superseded", "archived"].includes(status)) return "default";
  return "gold";
}

function todayString() {
  return new Date().toISOString().slice(0, 10);
}

function DashboardView({ dashboard, canon, tasks, decisions, commits, loading, onOpenCanon }) {
  const inProgressTasks = (tasks || []).filter((task) => task.status === "in_progress").slice(0, 5);
  const acceptedDecisions = (decisions || []).filter((decision) => decision.status === "accepted").slice(0, 4);
  const recentCommits = (commits || []).slice(0, 5);

  if (loading && !dashboard) {
    return (
      <div className="page-loading">
        <Spin size="large" />
      </div>
    );
  }

  return (
    <div className="view-stack">
      <Row gutter={[16, 16]}>
        <Col xs={24} md={12} xl={6}>
          <Card className="console-card stat-card" bordered={false}>
            <Statistic title="进行中任务" value={dashboard?.task_counts?.in_progress || 0} />
            <Text type="secondary">总数 {dashboard?.task_counts?.total || 0}</Text>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={6}>
          <Card className="console-card stat-card" bordered={false}>
            <Statistic title="待处理想法" value={dashboard?.idea_counts?.inbox || 0} />
            <Text type="secondary">总数 {dashboard?.idea_counts?.total || 0}</Text>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={6}>
          <Card className="console-card stat-card" bordered={false}>
            <Statistic title="已采纳决策" value={dashboard?.decision_counts?.accepted || 0} />
            <Text type="secondary">总数 {dashboard?.decision_counts?.total || 0}</Text>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={6}>
          <Card className="console-card stat-card" bordered={false}>
            <Statistic title="真相文档" value={dashboard?.doc_counts?.source_of_truth || 0} />
            <Text type="secondary">总数 {dashboard?.doc_counts?.total || 0}</Text>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={6}>
          <Card className="console-card stat-card" bordered={false}>
            <Statistic title="代码提交" value={dashboard?.commit_counts?.committed || 0} />
            <Text type="secondary">待审查 {dashboard?.commit_counts?.needs_review || 0}</Text>
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={14}>
          <Card className="console-card" title="当前建议" bordered={false}>
            {dashboard?.current_recommendations?.length ? (
              <List
                dataSource={dashboard.current_recommendations}
                renderItem={(item) => <List.Item>{item}</List.Item>}
              />
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无建议" />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={10}>
          <Card className="console-card" title="当前风险" bordered={false}>
            {dashboard?.current_risks?.length ? (
              <List
                dataSource={dashboard.current_risks}
                renderItem={(item) => (
                  <List.Item>
                    <Tag color="red">风险</Tag>
                    <span>{item}</span>
                  </List.Item>
                )}
              />
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无风险" />
            )}
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={12}>
          <Card className="console-card" title="规范摘要" bordered={false}>
            {canon ? (
              <div className="canon-grid">
                <div>
                  <Text type="secondary">产品目标</Text>
                  <Paragraph>{canon.product_goal}</Paragraph>
                </div>
                <div>
                  <Text type="secondary">工程重点</Text>
                  <Paragraph>{canon.engineering_focus}</Paragraph>
                </div>
                <div>
                  <Text type="secondary">架构约束</Text>
                  <Paragraph>{canon.architecture}</Paragraph>
                </div>
                <div>
                  <Text type="secondary">版本范围</Text>
                  <div className="tag-wrap">
                    {(canon.version_scope || []).map((item) => (
                      <Tag key={item}>{item}</Tag>
                    ))}
                  </div>
                </div>
              </div>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无规范数据" />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={12}>
          <Card className="console-card" title="主线执行" bordered={false}>
            <List
              locale={{ emptyText: "暂无主线项目" }}
              dataSource={[...inProgressTasks, ...acceptedDecisions]}
              renderItem={(item) => (
                <List.Item>
                  <Space direction="vertical" size={2} style={{ width: "100%" }}>
                    <Space wrap>
                      {"status" in item ? <Tag color={statusColor(item.status)}>{item.status}</Tag> : null}
                      {"priority" in item ? <Tag>{item.priority}</Tag> : null}
                      {"date" in item ? <Text type="secondary">{item.date}</Text> : null}
                    </Space>
                    <Text strong>{item.title}</Text>
                    <Text type="secondary">{item.id}</Text>
                  </Space>
                </List.Item>
              )}
            />
          </Card>
        </Col>
      </Row>
      <Card className="console-card" title="最近提交" bordered={false}>
        {recentCommits.length ? (
          <List
            dataSource={recentCommits}
            renderItem={(commit) => (
              <List.Item>
                <Space direction="vertical" size={4} style={{ width: "100%" }}>
                  <Space wrap>
                    <Tag color={statusColor(commit.status)}>{commit.status}</Tag>
                    <Tag color={commit.test_status === "passed" ? "green" : commit.test_status === "failed" ? "red" : "default"}>
                      测试:{commit.test_status}
                    </Tag>
                    <Tag color={commit.review_status === "approved" ? "green" : "gold"}>
                      审查:{commit.review_status}
                    </Tag>
                  </Space>
                  <Text strong>{commit.title}</Text>
                  <Text type="secondary">
                    {commit.branch || "无分支"} {commit.commit_hash ? `· ${commit.commit_hash}` : ""}
                  </Text>
                </Space>
              </List.Item>
            )}
          />
        ) : (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无提交" />
        )}
      </Card>
    </div>
  );
}

function CanonView({ canon, decisions, canonForm, setCanonForm, onSubmitCanon, busy }) {
  const acceptedDecisions = decisions.filter((item) => item.status === "accepted");

  return (
    <div className="view-stack">
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={15}>
          <Card className="console-card" title="更新规范" bordered={false}>
            <Form layout="vertical" onFinish={onSubmitCanon}>
              <Form.Item label="已采纳决策" required>
                <Select
                  value={canonForm.decisionId || undefined}
                  placeholder="选择一个已采纳的决策"
                  onChange={(value) => setCanonForm((current) => ({ ...current, decisionId: value }))}
                  options={acceptedDecisions.map((item) => ({
                    value: item.id,
                    label: `${item.date} 路 ${item.title}`,
                  }))}
                />
              </Form.Item>
              <Form.Item label="产品目标">
                <TextArea
                  rows={3}
                  value={canonForm.productGoal}
                  onChange={(event) =>
                    setCanonForm((current) => ({ ...current, productGoal: event.target.value }))
                  }
                />
              </Form.Item>
              <Form.Item label="工程重点">
                <TextArea
                  rows={3}
                  value={canonForm.engineeringFocus}
                  onChange={(event) =>
                    setCanonForm((current) => ({ ...current, engineeringFocus: event.target.value }))
                  }
                />
              </Form.Item>
              <Form.Item label="架构约束">
                <TextArea
                  rows={3}
                  value={canonForm.architecture}
                  onChange={(event) =>
                    setCanonForm((current) => ({ ...current, architecture: event.target.value }))
                  }
                />
              </Form.Item>
              <Row gutter={16}>
                <Col xs={24} xl={12}>
                  <Form.Item label="追加范围">
                    <Input
                      value={canonForm.addScope}
                      placeholder="用 | 分隔"
                      onChange={(event) =>
                        setCanonForm((current) => ({ ...current, addScope: event.target.value }))
                      }
                    />
                  </Form.Item>
                </Col>
                <Col xs={24} xl={12}>
                  <Form.Item label="追加避免项">
                    <Input
                      value={canonForm.addAvoid}
                      placeholder="用 | 分隔"
                      onChange={(event) =>
                        setCanonForm((current) => ({ ...current, addAvoid: event.target.value }))
                      }
                    />
                  </Form.Item>
                </Col>
              </Row>
              <Button type="primary" htmlType="submit" loading={busy}>
                更新规范
              </Button>
            </Form>
          </Card>
        </Col>
        <Col xs={24} xl={9}>
          <Card className="console-card" title="当前规范" bordered={false}>
            {canon ? (
              <Space direction="vertical" size={16} style={{ width: "100%" }}>
                <div>
                  <Text type="secondary">更新时间</Text>
                  <Paragraph>{canon.updated_at}</Paragraph>
                </div>
                <div>
                  <Text type="secondary">关联决策</Text>
                  <div className="tag-wrap">
                    {(canon.related_decisions || []).map((item) => (
                      <Tag key={item}>{item}</Tag>
                    ))}
                  </div>
                </div>
                <div>
                  <Text type="secondary">Avoid Now</Text>
                  <div className="tag-wrap">
                    {(canon.avoid_now || []).map((item) => (
                      <Tag key={item} color="default">
                        {item}
                      </Tag>
                    ))}
                  </div>
                </div>
                <div>
                  <Text type="secondary">关键任务</Text>
                  <List
                    size="small"
                    locale={{ emptyText: "暂无" }}
                    dataSource={canon.top_tasks || []}
                    renderItem={(item) => <List.Item>{item}</List.Item>}
                  />
                </div>
              </Space>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无规范数据" />
            )}
          </Card>
        </Col>
      </Row>
    </div>
  );
}

function TasksView({
  tasks,
  taskSearch,
  taskStatusFilter,
  setTaskSearch,
  setTaskStatusFilter,
  taskForm,
  setTaskForm,
  onCreateTask,
  onUpdateTask,
  busy,
}) {
  const filteredTasks = useMemo(() => {
    return tasks.filter((task) => {
      const query = `${task.title} ${task.id} ${task.phase} ${(task.acceptance || []).join(" ")}`.toLowerCase();
      return (!taskStatusFilter || task.status === taskStatusFilter) && (!taskSearch || query.includes(taskSearch.toLowerCase()));
    });
  }, [tasks, taskSearch, taskStatusFilter]);

  const groups = ["todo", "in_progress", "blocked", "done"];

  return (
    <div className="view-stack">
      <Card className="console-card" title="新建任务" bordered={false}>
        <Form layout="vertical" onFinish={onCreateTask}>
          <Row gutter={16}>
            <Col xs={24} xl={12}>
              <Form.Item label="标题" required>
                <Input
                  value={taskForm.title}
                  onChange={(event) => setTaskForm((current) => ({ ...current, title: event.target.value }))}
                />
              </Form.Item>
            </Col>
            <Col xs={24} xl={12}>
              <Form.Item label="验收标准">
                <Input
                  value={taskForm.acceptance}
                  placeholder="用 | 分隔"
                  onChange={(event) =>
                    setTaskForm((current) => ({ ...current, acceptance: event.target.value }))
                  }
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item label="优先级">
                <Select
                  value={taskForm.priority}
                  onChange={(value) => setTaskForm((current) => ({ ...current, priority: value }))}
                  options={[{ value: "P0" }, { value: "P1" }, { value: "P2" }]}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item label="阶段">
                <Select
                  value={taskForm.phase}
                  onChange={(value) => setTaskForm((current) => ({ ...current, phase: value }))}
                  options={[
                    { value: "general" },
                    { value: "foundation" },
                    { value: "implementation" },
                    { value: "polish" },
                  ]}
                />
              </Form.Item>
            </Col>
          </Row>
          <Button type="primary" htmlType="submit" loading={busy}>
            创建任务
          </Button>
        </Form>
      </Card>

      <Card
        className="console-card"
        title="任务工作台"
        bordered={false}
        extra={
          <Space wrap>
            <Input
              value={taskSearch}
              onChange={(event) => setTaskSearch(event.target.value)}
              placeholder="搜索任务"
              style={{ width: 180 }}
            />
            <Select
              value={taskStatusFilter || undefined}
              allowClear
              placeholder="全部状态"
              style={{ width: 150 }}
              onChange={(value) => setTaskStatusFilter(value || "")}
              options={TASK_STATUSES.map((status) => ({ value: status, label: status }))}
            />
          </Space>
        }
      >
        <div className="board-grid">
          {groups.map((status) => {
            const items = filteredTasks.filter((task) => task.status === status);
            return (
              <div key={status} className="board-column">
                <div className="board-column__head">
                  <span>{status}</span>
                  <Badge count={items.length} color="#b55e32" />
                </div>
                <div className="board-column__body">
                  {items.length ? (
                    items.map((task) => (
                      <Card key={task.id} size="small" className="inner-card" bordered={false}>
                        <Space direction="vertical" size={8} style={{ width: "100%" }}>
                          <Space wrap>
                            <Tag>{task.priority}</Tag>
                            <Tag>{task.phase}</Tag>
                          </Space>
                          <Text strong>{task.title}</Text>
                          <Text type="secondary">{task.id}</Text>
                          {(task.acceptance || []).length ? (
                            <div className="tag-wrap">
                              {task.acceptance.map((item) => (
                                <Tag key={item} bordered={false}>
                                  {item}
                                </Tag>
                              ))}
                            </div>
                          ) : null}
                          <Space wrap>
                            {status !== "in_progress" ? (
                              <Button size="small" onClick={() => onUpdateTask(task.id, "in_progress")}>
                                In Progress
                              </Button>
                            ) : null}
                            {status !== "blocked" ? (
                              <Button size="small" onClick={() => onUpdateTask(task.id, "blocked")}>
                                Blocked
                              </Button>
                            ) : null}
                            {status !== "done" ? (
                              <Button
                                size="small"
                                type="primary"
                                ghost
                                onClick={() => onUpdateTask(task.id, "done")}
                              >
                                Done
                              </Button>
                            ) : null}
                          </Space>
                        </Space>
                      </Card>
                    ))
                  ) : (
                    <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="空" />
                  )}
                </div>
              </div>
            );
          })}
        </div>
      </Card>
    </div>
  );
}

function IdeasView({
  ideas,
  ideaSearch,
  ideaStatusFilter,
  setIdeaSearch,
  setIdeaStatusFilter,
  ideaForm,
  setIdeaForm,
  onCreateIdea,
  onUpdateIdea,
  busy,
}) {
  const filteredIdeas = useMemo(() => {
    return ideas.filter((idea) => {
      const query = `${idea.title} ${idea.summary} ${idea.impact || ""} ${idea.source || ""}`.toLowerCase();
      return (!ideaStatusFilter || idea.status === ideaStatusFilter) && (!ideaSearch || query.includes(ideaSearch.toLowerCase()));
    });
  }, [ideas, ideaSearch, ideaStatusFilter]);

  return (
    <div className="view-stack">
      <Card className="console-card" title="捕获想法" bordered={false}>
        <Form layout="vertical" onFinish={onCreateIdea}>
          <Form.Item label="标题" required>
            <Input
              value={ideaForm.title}
              onChange={(event) => setIdeaForm((current) => ({ ...current, title: event.target.value }))}
            />
          </Form.Item>
          <Form.Item label="Summary" required>
            <TextArea
              rows={4}
              value={ideaForm.summary}
              onChange={(event) => setIdeaForm((current) => ({ ...current, summary: event.target.value }))}
            />
          </Form.Item>
          <Form.Item label="Impact">
            <Input
              value={ideaForm.impact}
              onChange={(event) => setIdeaForm((current) => ({ ...current, impact: event.target.value }))}
            />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>
            创建想法
          </Button>
        </Form>
      </Card>

      <Card
        className="console-card"
        title="想法池"
        bordered={false}
        extra={
          <Space wrap>
            <Input
              value={ideaSearch}
              onChange={(event) => setIdeaSearch(event.target.value)}
              placeholder="搜索想法"
              style={{ width: 220 }}
            />
            <Select
              value={ideaStatusFilter || undefined}
              allowClear
              placeholder="全部状态"
              style={{ width: 180 }}
              onChange={(value) => setIdeaStatusFilter(value || "")}
              options={IDEA_STATUSES.map((status) => ({ value: status, label: status }))}
            />
          </Space>
        }
      >
        {filteredIdeas.length ? (
          <List
            itemLayout="vertical"
            dataSource={filteredIdeas}
            renderItem={(idea) => (
              <List.Item>
                <Card className="inner-card" bordered={false}>
                  <Space direction="vertical" size={8} style={{ width: "100%" }}>
                    <Space wrap>
                      <Tag color={statusColor(idea.status)}>{idea.status}</Tag>
                      <Text type="secondary">{idea.source}</Text>
                      <Text type="secondary">{idea.created_at}</Text>
                    </Space>
                    <Text strong>{idea.title}</Text>
                    <Paragraph>{idea.summary}</Paragraph>
                    {idea.impact ? <Text type="secondary">Impact: {idea.impact}</Text> : null}
                    <Space wrap>
                      <Button size="small" onClick={() => onUpdateIdea(idea.id, "accepted")}>
                        Accept
                      </Button>
                      <Button size="small" onClick={() => onUpdateIdea(idea.id, "rejected")}>
                        Reject
                      </Button>
                      <Button size="small" danger onClick={() => onUpdateIdea(idea.id, "obsolete")}>
                        Obsolete
                      </Button>
                    </Space>
                  </Space>
                </Card>
              </List.Item>
            )}
          />
        ) : (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无想法" />
        )}
      </Card>
    </div>
  );
}

function DocsView({ docs, docAudit, docForm, setDocForm, onSubmitDoc, busy }) {
  const columns = [
    { title: "路径", dataIndex: "path", key: "path" },
    { title: "类型", dataIndex: "type", key: "type" },
    { title: "层级", dataIndex: "layer", key: "layer" },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      render: (value) => <Tag color={statusColor(value)}>{value}</Tag>,
    },
    {
      title: "真相",
      dataIndex: "source_of_truth",
      key: "source_of_truth",
      render: (value) => (value ? <CheckCircleOutlined /> : "-"),
    },
    { title: "上次审阅", dataIndex: "last_reviewed", key: "last_reviewed" },
  ];

  return (
    <div className="view-stack">
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={10}>
      <Card className="console-card" title="文档记录" bordered={false}>
            <Form layout="vertical" onFinish={onSubmitDoc}>
              <Form.Item label="路径" required>
                <Input
                  value={docForm.path}
                  onChange={(event) => setDocForm((current) => ({ ...current, path: event.target.value }))}
                />
              </Form.Item>
              <Form.Item label="类型">
                <Input
                  value={docForm.type}
                  onChange={(event) => setDocForm((current) => ({ ...current, type: event.target.value }))}
                />
              </Form.Item>
              <Form.Item label="Status">
                <Select
                  value={docForm.status}
                  onChange={(value) => setDocForm((current) => ({ ...current, status: value }))}
                  options={DOC_STATUSES.map((status) => ({ value: status, label: status }))}
                />
              </Form.Item>
              <Form.Item label="层级">
                <Select
                  value={docForm.layer}
                  onChange={(value) => setDocForm((current) => ({ ...current, layer: value }))}
                  options={DOC_LAYERS.map((layer) => ({ value: layer, label: layer }))}
                />
              </Form.Item>
              <Form.Item label="真相来源">
                <Select
                  value={docForm.sourceOfTruth ? "yes" : "no"}
                  onChange={(value) =>
                    setDocForm((current) => ({ ...current, sourceOfTruth: value === "yes" }))
                  }
                  options={[
                    { value: "no", label: "否" },
                    { value: "yes", label: "是" },
                  ]}
                />
              </Form.Item>
              <Button type="primary" htmlType="submit" loading={busy}>
                保存文档记录
              </Button>
            </Form>
          </Card>
        </Col>
        <Col xs={24} xl={14}>
          <Card className="console-card" title="文档审计" bordered={false}>
            {docAudit ? (
              <Row gutter={[16, 16]}>
                <Col xs={12} md={6}>
                  <Statistic title="记录数" value={docAudit.total_records} />
                </Col>
                <Col xs={12} md={6}>
                  <Statistic title="活跃" value={docAudit.active_records} />
                </Col>
                <Col xs={12} md={6}>
                  <Statistic title="真相" value={docAudit.source_of_truth_records} />
                </Col>
                <Col xs={12} md={6}>
                  <Statistic
                    title="待替换"
                    value={docAudit.obsolete_without_replacement.length}
                  />
                </Col>
              </Row>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无审计数据" />
            )}
          </Card>
        </Col>
      </Row>

      <Card className="console-card" title="文档目录" bordered={false}>
        <Table
          rowKey="path"
          columns={columns}
          dataSource={docs}
          pagination={{ pageSize: 8 }}
          onRow={(record) => ({
            onClick: () =>
              setDocForm({
                path: record.path || "",
                type: record.type || "",
                status: record.status || "draft",
                layer: record.layer || "exploration",
                sourceOfTruth: !!record.source_of_truth,
              }),
          })}
        />
      </Card>
    </div>
  );
}

function DecisionsView({
  decisions,
  decisionSearch,
  decisionStatusFilter,
  setDecisionSearch,
  setDecisionStatusFilter,
  decisionForm,
  setDecisionForm,
  onCreateDecision,
  onUpdateDecision,
  onCopyIntoCanon,
  busy,
}) {
  const filteredDecisions = useMemo(() => {
    return decisions.filter((decision) => {
      const query = `${decision.title} ${decision.id} ${decision.background} ${decision.decision}`.toLowerCase();
      return (!decisionStatusFilter || decision.status === decisionStatusFilter) && (!decisionSearch || query.includes(decisionSearch.toLowerCase()));
    });
  }, [decisions, decisionSearch, decisionStatusFilter]);

  return (
    <div className="view-stack">
      <Card className="console-card" title="新建决策" bordered={false}>
        <Form layout="vertical" onFinish={onCreateDecision}>
          <Form.Item label="鏍囬" required>
            <Input
              value={decisionForm.title}
              onChange={(event) =>
                setDecisionForm((current) => ({ ...current, title: event.target.value }))
              }
            />
          </Form.Item>
          <Form.Item label="背景" required>
            <TextArea
              rows={3}
              value={decisionForm.background}
              onChange={(event) =>
                setDecisionForm((current) => ({ ...current, background: event.target.value }))
              }
            />
          </Form.Item>
          <Form.Item label="决策内容" required>
            <TextArea
              rows={3}
              value={decisionForm.decision}
              onChange={(event) =>
                setDecisionForm((current) => ({ ...current, decision: event.target.value }))
              }
            />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>
            创建决策
          </Button>
        </Form>
      </Card>

      <Card
        className="console-card"
        title="决策日志"
        bordered={false}
        extra={
          <Space wrap>
            <Input
              value={decisionSearch}
              onChange={(event) => setDecisionSearch(event.target.value)}
              placeholder="搜索决策"
              style={{ width: 220 }}
            />
            <Select
              value={decisionStatusFilter || undefined}
              allowClear
              placeholder="全部状态"
              style={{ width: 180 }}
              onChange={(value) => setDecisionStatusFilter(value || "")}
              options={DECISION_STATUSES.map((status) => ({ value: status, label: status }))}
            />
          </Space>
        }
      >
        {filteredDecisions.length ? (
          <List
            itemLayout="vertical"
            dataSource={filteredDecisions}
            renderItem={(decision) => (
              <List.Item>
                <Card className="inner-card" bordered={false}>
                  <Space direction="vertical" size={8} style={{ width: "100%" }}>
                    <Space wrap>
                      <Tag color={statusColor(decision.status)}>{decision.status}</Tag>
                      <Text type="secondary">{decision.date}</Text>
                      <Text type="secondary">{decision.id}</Text>
                    </Space>
                    <Text strong>{decision.title}</Text>
                    <Paragraph>{decision.decision}</Paragraph>
                    <Text type="secondary">背景: {decision.background}</Text>
                    <Space wrap>
                      <Button size="small" onClick={() => onUpdateDecision(decision.id, "accepted")}>
                        采纳
                      </Button>
                      <Button size="small" onClick={() => onUpdateDecision(decision.id, "rejected")}>
                        拒绝
                      </Button>
                      <Button size="small" onClick={() => onUpdateDecision(decision.id, "superseded")}>
                        替代
                      </Button>
                      {decision.status === "accepted" ? (
                        <Button size="small" type="primary" ghost onClick={() => onCopyIntoCanon(decision.id)}>
                          同步到 Canon
                        </Button>
                      ) : null}
                    </Space>
                  </Space>
                </Card>
              </List.Item>
            )}
          />
        ) : (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无决策" />
        )}
      </Card>
    </div>
  );
}

function CommitsView({
  commits,
  tasks,
  decisions,
  commitSearch,
  commitStatusFilter,
  setCommitSearch,
  setCommitStatusFilter,
  commitForm,
  setCommitForm,
  onCreateCommit,
  onUpdateCommit,
  busy,
}) {
  const filteredCommits = useMemo(() => {
    return commits.filter((commit) => {
      const query = `${commit.title} ${commit.summary} ${commit.branch} ${commit.commit_hash} ${(commit.files || []).join(" ")}`.toLowerCase();
      return (!commitStatusFilter || commit.status === commitStatusFilter) && (!commitSearch || query.includes(commitSearch.toLowerCase()));
    });
  }, [commitSearch, commitStatusFilter, commits]);

  const columns = [
    {
      title: "提交",
      dataIndex: "title",
      key: "title",
      render: (_, record) => (
        <Space direction="vertical" size={2}>
          <Text strong>{record.title}</Text>
          <Text type="secondary">{record.summary || "无摘要"}</Text>
          <Text type="secondary">{record.branch || "无分支"} {record.commit_hash ? `· ${record.commit_hash}` : ""}</Text>
        </Space>
      ),
    },
    {
      title: "关联",
      key: "links",
      render: (_, record) => (
        <Space direction="vertical" size={2}>
          <Text type="secondary">任务: {record.task_id || "-"}</Text>
          <Text type="secondary">决策: {record.decision_id || "-"}</Text>
        </Space>
      ),
    },
    {
      title: "状态",
      key: "status",
      render: (_, record) => (
        <Space direction="vertical" size={6}>
          <Tag color={statusColor(record.status)}>{record.status}</Tag>
          <Tag color={record.test_status === "passed" ? "green" : record.test_status === "failed" ? "red" : "default"}>
            测试:{record.test_status}
          </Tag>
          <Tag color={record.review_status === "approved" ? "green" : "gold"}>
            审查:{record.review_status}
          </Tag>
        </Space>
      ),
    },
    {
      title: "文件",
      key: "files",
      render: (_, record) => (
        <div className="tag-wrap">
          {(record.files || []).length ? record.files.map((file) => <Tag key={file}>{file}</Tag>) : <Text type="secondary">-</Text>}
        </div>
      ),
    },
    {
      title: "操作",
      key: "actions",
      render: (_, record) => (
        <Space wrap>
          {record.status !== "committed" ? (
            <Button size="small" onClick={() => onUpdateCommit(record.id, { status: "committed" })}>
              标记已提交
            </Button>
          ) : null}
          {record.status !== "merged" ? (
            <Button size="small" onClick={() => onUpdateCommit(record.id, { status: "merged" })}>
              标记已合并
            </Button>
          ) : null}
          <Button size="small" onClick={() => onUpdateCommit(record.id, { test_status: "passed" })}>
            测试通过
          </Button>
          <Button size="small" onClick={() => onUpdateCommit(record.id, { review_status: "approved" })}>
            通过审查
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <div className="view-stack">
      <Card className="console-card" title="登记提交" bordered={false}>
        <Form layout="vertical" onFinish={onCreateCommit}>
          <Row gutter={16}>
            <Col xs={24} xl={12}>
              <Form.Item label="标题" required>
                <Input
                  value={commitForm.title}
                  onChange={(event) => setCommitForm((current) => ({ ...current, title: event.target.value }))}
                />
              </Form.Item>
            </Col>
            <Col xs={24} xl={12}>
              <Form.Item label="分支">
                <Input
                  value={commitForm.branch}
                  onChange={(event) => setCommitForm((current) => ({ ...current, branch: event.target.value }))}
                />
              </Form.Item>
            </Col>
            <Col xs={24}>
              <Form.Item label="摘要">
                <TextArea
                  rows={3}
                  value={commitForm.summary}
                  onChange={(event) => setCommitForm((current) => ({ ...current, summary: event.target.value }))}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={12} xl={8}>
              <Form.Item label="提交哈希">
                <Input
                  value={commitForm.commitHash}
                  onChange={(event) => setCommitForm((current) => ({ ...current, commitHash: event.target.value }))}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={12} xl={8}>
              <Form.Item label="任务">
                <Select
                  allowClear
                  value={commitForm.taskId || undefined}
                  onChange={(value) => setCommitForm((current) => ({ ...current, taskId: value || "" }))}
                  options={tasks.map((task) => ({ value: task.id, label: `${task.title} 路 ${task.id}` }))}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={12} xl={8}>
              <Form.Item label="决策">
                <Select
                  allowClear
                  value={commitForm.decisionId || undefined}
                  onChange={(value) => setCommitForm((current) => ({ ...current, decisionId: value || "" }))}
                  options={decisions.map((decision) => ({ value: decision.id, label: `${decision.title} 路 ${decision.id}` }))}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={8}>
              <Form.Item label="状态">
                <Select
                  value={commitForm.status}
                  onChange={(value) => setCommitForm((current) => ({ ...current, status: value }))}
                  options={COMMIT_STATUSES.map((status) => ({ value: status, label: status }))}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={8}>
              <Form.Item label="测试">
                <Select
                  value={commitForm.testStatus}
                  onChange={(value) => setCommitForm((current) => ({ ...current, testStatus: value }))}
                  options={COMMIT_TEST_STATUSES.map((status) => ({ value: status, label: status }))}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={8}>
              <Form.Item label="审查">
                <Select
                  value={commitForm.reviewStatus}
                  onChange={(value) => setCommitForm((current) => ({ ...current, reviewStatus: value }))}
                  options={COMMIT_REVIEW_STATUSES.map((status) => ({ value: status, label: status }))}
                />
              </Form.Item>
            </Col>
            <Col xs={24}>
              <Form.Item label="文件">
                <Input
                  value={commitForm.files}
                  placeholder="src/app.py | store.py"
                  onChange={(event) => setCommitForm((current) => ({ ...current, files: event.target.value }))}
                />
              </Form.Item>
            </Col>
          </Row>
          <Button type="primary" htmlType="submit" loading={busy}>
            登记提交
          </Button>
        </Form>
      </Card>

      <Card
        className="console-card"
        title="提交面板"
        bordered={false}
        extra={
          <Space wrap>
            <Input
              value={commitSearch}
              onChange={(event) => setCommitSearch(event.target.value)}
              placeholder="搜索提交"
              style={{ width: 220 }}
            />
            <Select
              value={commitStatusFilter || undefined}
              allowClear
              placeholder="全部状态"
              style={{ width: 180 }}
              onChange={(value) => setCommitStatusFilter(value || "")}
              options={COMMIT_STATUSES.map((status) => ({ value: status, label: status }))}
            />
          </Space>
        }
      >
        <Table
          rowKey="id"
          columns={columns}
          dataSource={filteredCommits}
          pagination={{ pageSize: 8 }}
          locale={{ emptyText: "暂无提交" }}
        />
      </Card>
    </div>
  );
}

function DailyView({ daily, dailyHistory, dailyForm, setDailyForm, onAppendDaily, onReplaceDaily, onLoadDate, busy }) {
  return (
    <div className="view-stack">
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={10}>
      <Card className="console-card" title="日报编辑" bordered={false}>
            <Form layout="vertical" onFinish={onAppendDaily}>
              <Form.Item label="日期">
                <Input
                  type="date"
                  value={dailyForm.noteDate}
                  onChange={(event) =>
                    setDailyForm((current) => ({ ...current, noteDate: event.target.value }))
                  }
                />
              </Form.Item>
              <Form.Item label="已完成">
                <Input
                  value={dailyForm.completed}
                  placeholder="用 | 分隔"
                  onChange={(event) =>
                    setDailyForm((current) => ({ ...current, completed: event.target.value }))
                  }
                />
              </Form.Item>
              <Form.Item label="问题">
                <Input
                  value={dailyForm.problems}
                  placeholder="用 | 分隔"
                  onChange={(event) =>
                    setDailyForm((current) => ({ ...current, problems: event.target.value }))
                  }
                />
              </Form.Item>
              <Form.Item label="风险">
                <Input
                  value={dailyForm.risks}
                  placeholder="用 | 分隔"
                  onChange={(event) =>
                    setDailyForm((current) => ({ ...current, risks: event.target.value }))
                  }
                />
              </Form.Item>
              <Form.Item label="下一步">
                <Input
                  value={dailyForm.next}
                  placeholder="用 | 分隔"
                  onChange={(event) =>
                    setDailyForm((current) => ({ ...current, next: event.target.value }))
                  }
                />
              </Form.Item>
              <Space>
                <Button type="primary" htmlType="submit" loading={busy}>
                  追加
                </Button>
                <Button onClick={onReplaceDaily} loading={busy}>
                  覆盖
                </Button>
              </Space>
            </Form>
          </Card>
        </Col>
        <Col xs={24} xl={14}>
          <Card
            className="console-card"
            title={daily ? `日报内容 (${daily.note_date})` : "日报内容"}
            bordered={false}
          >
            {daily ? (
              <Row gutter={[16, 16]}>
                <Col xs={24} md={12}>
                  <Card size="small" className="inner-card" title="已完成" bordered={false}>
                    <List
                      dataSource={daily.completed || []}
                      locale={{ emptyText: "暂无" }}
                      renderItem={(item) => <List.Item>{item}</List.Item>}
                    />
                  </Card>
                </Col>
                <Col xs={24} md={12}>
                  <Card size="small" className="inner-card" title="问题" bordered={false}>
                    <List
                      dataSource={daily.problems || []}
                      locale={{ emptyText: "暂无" }}
                      renderItem={(item) => <List.Item>{item}</List.Item>}
                    />
                  </Card>
                </Col>
                <Col xs={24} md={12}>
                  <Card size="small" className="inner-card" title="风险" bordered={false}>
                    <List
                      dataSource={daily.risks || []}
                      locale={{ emptyText: "暂无" }}
                      renderItem={(item) => <List.Item>{item}</List.Item>}
                    />
                  </Card>
                </Col>
                <Col xs={24} md={12}>
                  <Card size="small" className="inner-card" title="下一步" bordered={false}>
                    <List
                      dataSource={daily.next || []}
                      locale={{ emptyText: "暂无" }}
                      renderItem={(item) => <List.Item>{item}</List.Item>}
                    />
                  </Card>
                </Col>
              </Row>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无日报" />
            )}
          </Card>
        </Col>
      </Row>

      <Card className="console-card" title="历史日报" bordered={false}>
        <div className="history-row">
          {dailyHistory.length ? (
            dailyHistory.map((item) => (
              <Button key={item.note_date} onClick={() => onLoadDate(item.note_date)}>
                {item.note_date}
              </Button>
            ))
          ) : (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无历史" />
          )}
        </div>
      </Card>
    </div>
  );
}

function ConsoleApp() {
  const { message } = AntdApp.useApp();
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [view, setView] = useState("dashboard");
  const [selectedDailyDate, setSelectedDailyDate] = useState("");

  const [dashboard, setDashboard] = useState(null);
  const [canon, setCanon] = useState(null);
  const [tasks, setTasks] = useState([]);
  const [commits, setCommits] = useState([]);
  const [ideas, setIdeas] = useState([]);
  const [docs, setDocs] = useState([]);
  const [docAudit, setDocAudit] = useState(null);
  const [decisions, setDecisions] = useState([]);
  const [daily, setDaily] = useState(null);
  const [dailyHistory, setDailyHistory] = useState([]);

  const [taskSearch, setTaskSearch] = useState("");
  const [taskStatusFilter, setTaskStatusFilter] = useState("");
  const [commitSearch, setCommitSearch] = useState("");
  const [commitStatusFilter, setCommitStatusFilter] = useState("");
  const [ideaSearch, setIdeaSearch] = useState("");
  const [ideaStatusFilter, setIdeaStatusFilter] = useState("");
  const [decisionSearch, setDecisionSearch] = useState("");
  const [decisionStatusFilter, setDecisionStatusFilter] = useState("");

  const [taskForm, setTaskForm] = useState({ title: "", acceptance: "", priority: "P1", phase: "general" });
  const [commitForm, setCommitForm] = useState({
    title: "",
    summary: "",
    branch: "",
    commitHash: "",
    taskId: "",
    decisionId: "",
    status: "draft",
    testStatus: "not_run",
    reviewStatus: "pending",
    files: "",
  });
  const [ideaForm, setIdeaForm] = useState({ title: "", summary: "", impact: "" });
  const [docForm, setDocForm] = useState({ path: "", type: "", status: "draft", layer: "exploration", sourceOfTruth: false });
  const [decisionForm, setDecisionForm] = useState({ title: "", background: "", decision: "" });
  const [dailyForm, setDailyForm] = useState({ noteDate: todayString(), completed: "", problems: "", risks: "", next: "" });
  const [canonForm, setCanonForm] = useState({
    decisionId: "",
    productGoal: "",
    engineeringFocus: "",
    architecture: "",
    addScope: "",
    addAvoid: "",
  });

  async function loadAll(date = selectedDailyDate) {
    setLoading(true);
    try {
      const dailyQuery = date ? `?date=${encodeURIComponent(date)}` : "";
      const [summaryData, canonData, taskData, commitData, ideaData, docData, auditData, decisionData, dailyData, historyData] = await Promise.all([
        api("/planai/dashboard"),
        api("/planai/canon"),
        api("/planai/tasks"),
        api("/planai/commits"),
        api("/planai/ideas"),
        api("/planai/docs"),
        api("/planai/docs/audit"),
        api("/planai/decisions"),
        api(`/planai/daily${dailyQuery}`),
        api("/planai/daily/history"),
      ]);

      setDashboard(summaryData);
      setCanon(canonData);
      setTasks(taskData.tasks || []);
      setCommits(commitData.commits || []);
      setIdeas(ideaData.ideas || []);
      setDocs(docData.records || []);
      setDocAudit(auditData);
      setDecisions(decisionData.decisions || []);
      setDaily(dailyData);
      setDailyHistory(historyData.items || []);
      setDailyForm({
        noteDate: dailyData.note_date || todayString(),
        completed: (dailyData.completed || []).join(" | "),
        problems: (dailyData.problems || []).join(" | "),
        risks: (dailyData.risks || []).join(" | "),
        next: (dailyData.next || []).join(" | "),
      });
    } catch (error) {
      message.error(error.message || "Load failed");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadAll("");
  }, []);

  useEffect(() => {
    if (!canon) return;
    setCanonForm((current) => ({
      ...current,
      productGoal: canon.product_goal || "",
      engineeringFocus: canon.engineering_focus || "",
      architecture: canon.architecture || "",
    }));
  }, [canon]);

  async function runAction(action, successMessage) {
    setBusy(true);
    try {
      await action();
      await loadAll(selectedDailyDate);
      message.success(successMessage);
    } catch (error) {
      message.error(error.message || "Action failed");
    } finally {
      setBusy(false);
    }
  }

  function openCanonWithDecision(decisionId = "") {
    setCanonForm((current) => ({ ...current, decisionId }));
    setView("canon");
  }

  return (
    <Layout className="console-layout">
      <Sider width={116} className="console-sider" breakpoint="lg" collapsedWidth="0">
        <div className="brand-block">
          <div className="brand-mark">PM</div>
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[view]}
          items={NAV_ITEMS.map((item) => ({
            key: item.key,
            icon: item.icon,
            label: item.label,
          }))}
          onClick={({ key }) => setView(key)}
        />
      </Sider>

      <Layout>
        <Header className="console-header">
          <div>
            <Text className="header-kicker">PlanAI 本地面板</Text>
            <Title level={3} className="header-title">
              {NAV_ITEMS.find((item) => item.key === view)?.label}
            </Title>
          </div>
          <Button icon={<ReloadOutlined />} onClick={() => loadAll(selectedDailyDate)} loading={busy}>
            刷新
          </Button>
        </Header>

        <Content className="console-content">
          {view === "dashboard" ? (
            <DashboardView
              dashboard={dashboard}
              canon={canon}
              tasks={tasks}
              decisions={decisions}
              commits={commits}
              loading={loading}
              onOpenCanon={() => openCanonWithDecision("")}
            />
          ) : null}

          {view === "canon" ? (
            <CanonView
              canon={canon}
              decisions={decisions}
              canonForm={canonForm}
              setCanonForm={setCanonForm}
              busy={busy}
              onSubmitCanon={() =>
                runAction(
                  () =>
                    api("/planai/canon/update", {
                      method: "POST",
                      body: JSON.stringify({
                        decision_id: canonForm.decisionId,
                        product_goal: canonForm.productGoal,
                        engineering_focus: canonForm.engineeringFocus,
                        architecture: canonForm.architecture,
                        add_scope: splitValues(canonForm.addScope),
                        add_avoid: splitValues(canonForm.addAvoid),
                      }),
                    }).then(() =>
                      setCanonForm((current) => ({
                        ...current,
                        addScope: "",
                        addAvoid: "",
                      })),
                    ),
                  "Canon updated",
                )
              }
            />
          ) : null}

          {view === "tasks" ? (
            <TasksView
              tasks={tasks}
              taskSearch={taskSearch}
              taskStatusFilter={taskStatusFilter}
              setTaskSearch={setTaskSearch}
              setTaskStatusFilter={setTaskStatusFilter}
              taskForm={taskForm}
              setTaskForm={setTaskForm}
              busy={busy}
              onCreateTask={() =>
                runAction(
                  () =>
                    api("/planai/tasks", {
                      method: "POST",
                      body: JSON.stringify({
                        title: taskForm.title,
                        acceptance: splitValues(taskForm.acceptance),
                        priority: taskForm.priority,
                        phase: taskForm.phase,
                      }),
                    }).then(() => setTaskForm({ title: "", acceptance: "", priority: "P1", phase: "general" })),
                  "Task created",
                )
              }
              onUpdateTask={(taskId, status) =>
                runAction(
                  () =>
                    api(`/planai/tasks/${taskId}`, {
                      method: "PATCH",
                      body: JSON.stringify({ status, note: "updated from antd ui" }),
                    }),
                  "Task updated",
                )
              }
            />
          ) : null}

          {view === "commits" ? (
            <CommitsView
              commits={commits}
              tasks={tasks}
              decisions={decisions}
              commitSearch={commitSearch}
              commitStatusFilter={commitStatusFilter}
              setCommitSearch={setCommitSearch}
              setCommitStatusFilter={setCommitStatusFilter}
              commitForm={commitForm}
              setCommitForm={setCommitForm}
              busy={busy}
              onCreateCommit={() =>
                runAction(
                  () =>
                    api("/planai/commits", {
                      method: "POST",
                      body: JSON.stringify({
                        title: commitForm.title,
                        summary: commitForm.summary,
                        branch: commitForm.branch,
                        commit_hash: commitForm.commitHash,
                        task_id: commitForm.taskId || null,
                        decision_id: commitForm.decisionId || null,
                        status: commitForm.status,
                        test_status: commitForm.testStatus,
                        review_status: commitForm.reviewStatus,
                        files: splitValues(commitForm.files),
                      }),
                    }).then(() =>
                      setCommitForm({
                        title: "",
                        summary: "",
                        branch: "",
                        commitHash: "",
                        taskId: "",
                        decisionId: "",
                        status: "draft",
                        testStatus: "not_run",
                        reviewStatus: "pending",
                        files: "",
                      }),
                    ),
                  "Commit registered",
                )
              }
              onUpdateCommit={(commitId, payload) =>
                runAction(
                  () =>
                    api(`/planai/commits/${commitId}`, {
                      method: "PATCH",
                      body: JSON.stringify(payload),
                    }),
                  "Commit updated",
                )
              }
            />
          ) : null}

          {view === "ideas" ? (
            <IdeasView
              ideas={ideas}
              ideaSearch={ideaSearch}
              ideaStatusFilter={ideaStatusFilter}
              setIdeaSearch={setIdeaSearch}
              setIdeaStatusFilter={setIdeaStatusFilter}
              ideaForm={ideaForm}
              setIdeaForm={setIdeaForm}
              busy={busy}
              onCreateIdea={() =>
                runAction(
                  () =>
                    api("/planai/ideas", {
                      method: "POST",
                      body: JSON.stringify({
                        title: ideaForm.title,
                        summary: ideaForm.summary,
                        impact: ideaForm.impact,
                        source: "planai-ui",
                      }),
                    }).then(() => setIdeaForm({ title: "", summary: "", impact: "" })),
                  "Idea created",
                )
              }
              onUpdateIdea={(ideaId, status) =>
                runAction(
                  () =>
                    api(`/planai/ideas/${ideaId}`, {
                      method: "PATCH",
                      body: JSON.stringify({ status, note: "updated from antd ui" }),
                    }),
                  "Idea updated",
                )
              }
            />
          ) : null}

          {view === "docs" ? (
            <DocsView
              docs={docs}
              docAudit={docAudit}
              docForm={docForm}
              setDocForm={setDocForm}
              busy={busy}
              onSubmitDoc={() =>
                runAction(
                  () =>
                    api("/planai/docs", {
                      method: "PATCH",
                      body: JSON.stringify({
                        path: docForm.path,
                        type: docForm.type,
                        status: docForm.status,
                        layer: docForm.layer,
                        source_of_truth: docForm.sourceOfTruth,
                        create: true,
                        last_reviewed: todayString(),
                      }),
                    }),
                  "Doc updated",
                )
              }
            />
          ) : null}

          {view === "decisions" ? (
            <DecisionsView
              decisions={decisions}
              decisionSearch={decisionSearch}
              decisionStatusFilter={decisionStatusFilter}
              setDecisionSearch={setDecisionSearch}
              setDecisionStatusFilter={setDecisionStatusFilter}
              decisionForm={decisionForm}
              setDecisionForm={setDecisionForm}
              busy={busy}
              onCreateDecision={() =>
                runAction(
                  () =>
                    api("/planai/decisions", {
                      method: "POST",
                      body: JSON.stringify({
                        title: decisionForm.title,
                        background: decisionForm.background,
                        decision: decisionForm.decision,
                        status: "proposed",
                      }),
                    }).then(() => setDecisionForm({ title: "", background: "", decision: "" })),
                  "Decision created",
                )
              }
              onUpdateDecision={(decisionId, status) =>
                runAction(
                  () =>
                    api(`/planai/decisions/${decisionId}`, {
                      method: "PATCH",
                      body: JSON.stringify({ status }),
                    }),
                  "Decision updated",
                )
              }
              onCopyIntoCanon={(decisionId) => openCanonWithDecision(decisionId)}
            />
          ) : null}

          {view === "daily" ? (
            <DailyView
              daily={daily}
              dailyHistory={dailyHistory}
              dailyForm={dailyForm}
              setDailyForm={setDailyForm}
              busy={busy}
              onAppendDaily={() =>
                runAction(() => {
                  const nextDate = dailyForm.noteDate || todayString();
                  const query = nextDate ? `?date=${encodeURIComponent(nextDate)}` : "";
                  setSelectedDailyDate(nextDate);
                  return api(`/planai/daily${query}`, {
                    method: "POST",
                    body: JSON.stringify({
                      completed: splitValues(dailyForm.completed),
                      problems: splitValues(dailyForm.problems),
                      risks: splitValues(dailyForm.risks),
                      next: splitValues(dailyForm.next),
                    }),
                  });
                }, "Daily note appended")
              }
              onReplaceDaily={() => {
                const nextDate = dailyForm.noteDate || todayString();
                const query = nextDate ? `?date=${encodeURIComponent(nextDate)}` : "";
                setSelectedDailyDate(nextDate);
                return runAction(
                  () =>
                    api(`/planai/daily${query}`, {
                      method: "PUT",
                      body: JSON.stringify({
                        completed: splitValues(dailyForm.completed),
                        problems: splitValues(dailyForm.problems),
                        risks: splitValues(dailyForm.risks),
                        next: splitValues(dailyForm.next),
                      }),
                    }),
                  "Daily note replaced",
                );
              }}
              onLoadDate={async (date) => {
                setSelectedDailyDate(date);
                await loadAll(date);
              }}
            />
          ) : null}
        </Content>
      </Layout>
    </Layout>
  );
}

export default function App() {
  return (
    <ConfigProvider
      theme={{
        token: {
          colorPrimary: "#2f6fec",
          colorInfo: "#2f6fec",
          colorSuccess: "#2f7d62",
          colorWarning: "#c48a2c",
          colorError: "#c54f4f",
          colorBgLayout: "#f3f7fc",
          colorBgContainer: "#ffffff",
          colorText: "#18253d",
          colorBorderSecondary: "rgba(66, 102, 154, 0.14)",
          borderRadius: 18,
          fontFamily: "\"Avenir Next\", \"Segoe UI Variable\", \"PingFang SC\", sans-serif",
        },
      }}
    >
      <AntdApp>
        <ConsoleApp />
      </AntdApp>
    </ConfigProvider>
  );
}

