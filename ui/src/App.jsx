import { useEffect, useMemo, useState } from "react";
import {
  App as AntdApp,
  Badge,
  Breadcrumb,
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
  Divider,
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
  CompassOutlined,
  SafetyCertificateOutlined,
  CodeOutlined,
  LinkOutlined,
  DeleteOutlined,
  AppstoreOutlined,
  ProjectOutlined,
  TeamOutlined,
} from "@ant-design/icons";

const { Header, Sider, Content } = Layout;
const { Title, Paragraph, Text } = Typography;
const { TextArea } = Input;

// 导航菜单分组
const NAV_GROUPS = [
  {
    key: "core",
    label: "核心工作流",
    icon: <AppstoreOutlined />,
    children: [
      { key: "dashboard", label: "工作台", icon: <DashboardOutlined /> },
      { key: "tasks", label: "任务", icon: <ScheduleOutlined /> },
      { key: "decisions", label: "决策", icon: <FundProjectionScreenOutlined /> },
      { key: "commits", label: "提交", icon: <BranchesOutlined /> },
    ]
  },
  {
    key: "strategy",
    label: "战略规划",
    icon: <ProjectOutlined />,
    children: [
      { key: "visions", label: "愿景", icon: <CompassOutlined /> },
      { key: "principles", label: "原则", icon: <SafetyCertificateOutlined /> },
      { key: "canon", label: "规范", icon: <SettingOutlined /> },
    ]
  },
  {
    key: "knowledge",
    label: "知识管理",
    icon: <TeamOutlined />,
    children: [
      { key: "ideas", label: "想法", icon: <BulbOutlined /> },
      { key: "docs", label: "文档", icon: <BookOutlined /> },
      { key: "daily", label: "日报", icon: <FileTextOutlined /> },
      { key: "code", label: "代码", icon: <CodeOutlined /> },
    ]
  },
];

// 扁平化用于 Menu 组件
const NAV_ITEMS = NAV_GROUPS.flatMap(g => g.children);

// 状态常量
const TASK_STATUSES = ["todo", "in_progress", "blocked", "done", "dropped"];
const COMMIT_STATUSES = ["draft", "committed", "merged", "released", "dropped"];
const COMMIT_TEST_STATUSES = ["not_run", "passed", "failed"];
const COMMIT_REVIEW_STATUSES = ["pending", "approved", "changes_requested"];
const IDEA_STATUSES = ["inbox", "under_review", "accepted", "rejected", "obsolete"];
const DECISION_STATUSES = ["proposed", "accepted", "rejected", "superseded"];
const DOC_STATUSES = ["draft", "active", "archived", "obsolete"];
const DOC_LAYERS = ["baseline", "decision", "task", "exploration", "history", "topic"];
const VISION_STATUSES = ["active", "archived", "draft"];
const PRINCIPLE_STATUSES = ["active", "archived", "draft"];
const PRINCIPLE_KINDS = ["governance", "engineering", "product", "meta"];

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

function toTitleMap(items) {
  return new Map((items || []).map((item) => [item.id, item.title]));
}

function buildTaskPayload(form) {
  return {
    title: form.title,
    priority: form.priority,
    phase: form.phase,
    acceptance: splitValues(form.acceptance),
  };
}

function buildCanonPayload(form) {
  return {
    decision_id: form.decisionId,
    product_goal: form.productGoal,
    engineering_focus: form.engineeringFocus,
    architecture: form.architecture,
    add_scope: splitValues(form.addScope),
    add_avoid: splitValues(form.addAvoid),
  };
}

function buildCommitPayload(form) {
  return {
    title: form.title,
    summary: form.summary,
    branch: form.branch,
    task_id: form.taskId || null,
    decision_id: form.decisionId || null,
    status: form.status,
    test_status: form.testStatus,
    review_status: form.reviewStatus,
    files: splitValues(form.files),
  };
}

function buildDocPayload(form) {
  return {
    path: form.path,
    type: form.type,
    status: form.status,
    layer: form.layer,
    create: true,
    source_of_truth: form.sourceOfTruth,
    clear_source_of_truth: !form.sourceOfTruth,
  };
}

function buildDailyPayload(form) {
  return {
    completed: splitValues(form.completed),
    problems: splitValues(form.problems),
    risks: splitValues(form.risks),
    next: splitValues(form.next),
  };
}

function LinksDisplay({ links, onDeleteLink }) {
  if (!links) return null;
  const items = [...(links.outgoing || []), ...(links.incoming || [])];
  if (!items.length) return null;

  return (
    <div className="link-tag-list">
      <LinkOutlined style={{ marginRight: 8, color: "#8c8c8c" }} />
      {items.map((link) => (
        <Tag
          key={link.id}
          closable={!!onDeleteLink}
          onClose={() => onDeleteLink?.(link.id)}
          icon={<LinkOutlined />}
          className="link-tag"
        >
          {link.source_id} {link.relation} {link.target_id}
        </Tag>
      ))}
    </div>
  );
}

function DashboardView({
  dashboard,
  inbox,
  canon,
  visions,
  principles,
  loading,
  onOpenCanon,
  onOpenDecisions,
  onOpenTasks,
  onOpenCommits,
  onOpenIdeas,
  onOpenDocs,
  onOpenDaily,
  onOpenPrinciples,
}) {
  const activeVision = visions.find(v => v.status === 'active');
  const activePrinciples = principles.filter(p => p.status === 'active').slice(0, 5);
  const recommendedActions = inbox?.recommended_actions || [];
  const inboxCounts = inbox?.counts || {};
  const canonMeta = inbox?.canon || {};

  function handleRecommendedAction(action) {
    if (action.kind === "decision_review") {
      onOpenDecisions?.();
      return;
    }
    if (action.kind === "canon_followup") {
      onOpenCanon?.(action.target_id);
      return;
    }
    if (action.kind === "commit_review") {
      onOpenCommits?.();
      return;
    }
    if (action.kind === "verification_gap") {
      onOpenCommits?.();
      return;
    }
    if (action.kind === "task_closure_blocker") {
      onOpenTasks?.();
    }
  }

  if (loading && !dashboard) {
    return (
      <div className="page-loading">
        <Spin size="large" />
      </div>
    );
  }

  return (
    <div className="view-stack">
      {activeVision && (
        <Card 
          className="console-card vision-banner" 
          bordered={false}
          hoverable
          onClick={() => onOpenPrinciples?.()}
          style={{ cursor: 'pointer' }}
        >
          <Title level={4}><CompassOutlined /> 当前愿景: {activeVision.title}</Title>
          <Paragraph type="secondary">{activeVision.summary}</Paragraph>
          <div className="tag-wrap">
            <Tag color="blue">{activeVision.horizon}</Tag>
            <Text type="secondary">更新于 {activeVision.updated_at}</Text>
          </div>
        </Card>
      )}

      <Row gutter={[16, 16]}>
        <Col xs={24} md={12} xl={6}>
          <Card 
            className="console-card stat-card clickable-card" 
            bordered={false}
            hoverable
            onClick={(e) => {
              e.preventDefault();
              onOpenTasks?.();
            }}
          >
            <Statistic title="进行中任务" value={dashboard?.task_counts?.in_progress || 0} />
            <Text type="secondary">总数 {dashboard?.task_counts?.total || 0}</Text>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={6}>
          <Card 
            className="console-card stat-card clickable-card" 
            bordered={false}
            hoverable
            onClick={(e) => {
              e.preventDefault();
              onOpenDecisions?.();
            }}
          >
            <Statistic title="待审批事项" value={inboxCounts.total || 0} />
            <Text type="secondary">优先先看 inbox</Text>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={6}>
          <Card 
            className="console-card stat-card clickable-card" 
            bordered={false}
            hoverable
            onClick={(e) => {
              e.preventDefault();
              onOpenDecisions?.();
            }}
          >
            <Statistic title="待决策" value={inboxCounts.proposed_decisions || 0} />
            <Text type="secondary">显式 review 后再推进</Text>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={6}>
          <Card 
            className="console-card stat-card clickable-card" 
            bordered={false}
            hoverable
            onClick={(e) => {
              e.preventDefault();
              onOpenCanon?.();
            }}
          >
            <Statistic title="规范同步" value={inboxCounts.canon_followups || 0} />
            <Text type="secondary">已关联 {canonMeta.related_decisions_count || 0}</Text>
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={16}>
          <Card className="console-card" title="推荐动作" bordered={false}>
            {recommendedActions.length ? (
              <List
                dataSource={recommendedActions}
                renderItem={(action) => (
                  <List.Item
                    actions={[
                      <Button key="open" type="link" onClick={() => handleRecommendedAction(action)}>
                        打开处理
                      </Button>,
                    ]}
                  >
                    <Space direction="vertical" size={4} style={{ width: "100%" }}>
                      <Space wrap>
                        <Tag color={action.priority === "high" ? "red" : "gold"}>{action.priority}</Tag>
                        <Tag>{action.kind}</Tag>
                      </Space>
                      <Text strong>{action.title}</Text>
                      <Text type="secondary">{action.reason}</Text>
                      <Text code>{action.command}</Text>
                    </Space>
                  </List.Item>
                )}
              />
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无推荐动作" />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={8}>
          <Card 
            className="console-card" 
            title="活跃原则" 
            bordered={false}
            extra={
              <Button type="link" size="small" onClick={() => onOpenPrinciples?.()}>
                查看全部
              </Button>
            }
          >
            {activePrinciples.length ? (
              <List
                dataSource={activePrinciples}
                renderItem={(p) => (
                  <List.Item>
                    <Space direction="vertical" size={2}>
                      <Text strong><SafetyCertificateOutlined /> {p.title}</Text>
                      <Tag size="small">{p.kind}</Tag>
                    </Space>
                  </List.Item>
                )}
              />
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无活跃原则" />
            )}
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={12}>
          <Card className="console-card" title="基线快照" bordered={false}>
            {canon ? (
              <div className="canon-grid">
                <div>
                  <Text type="secondary">工程重点</Text>
                  <Paragraph ellipsis={{ rows: 2 }}>{canon.engineering_focus || '未定义'}</Paragraph>
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
          <Card className="console-card" title="今日关注" bordered={false}>
            <List
              dataSource={dashboard?.today_focus || []}
              renderItem={(item) => (
                <List.Item>
                  <Text strong>{item}</Text>
                </List.Item>
              )}
              locale={{ emptyText: "暂无关注项" }}
            />
          </Card>
        </Col>
      </Row>
    </div>
  );
}

function VisionsView({ visions, visionForm, setVisionForm, onCreateVision, onUpdateVision, onOpenVisionDetail, tasks, decisions, busy }) {
  const columns = [
    { title: "标题", dataIndex: "title", key: "title" },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      render: (s) => <Tag color={statusColor(s)}>{s}</Tag>
    },
    { title: "周期", dataIndex: "horizon", key: "horizon" },
    { title: "更新于", dataIndex: "updated_at", key: "updated_at" },
    {
      title: "关联项",
      key: "relations",
      render: (_, record) => {
        const visionTasks = (tasks || []).filter(t => t.vision_id === record.id);
        const visionDecisions = (decisions || []).filter(d => d.vision_id === record.id);
        return (
          <Space wrap>
            {visionTasks.length > 0 && <Tag color="blue">{visionTasks.length} 任务</Tag>}
            {visionDecisions.length > 0 && <Tag color="gold">{visionDecisions.length} 决策</Tag>}
            {visionTasks.length === 0 && visionDecisions.length === 0 && <Text type="secondary">无</Text>}
          </Space>
        );
      }
    },
    {
      title: "操作",
      key: "actions",
      render: (_, record) => (
        <Space>
          <Button size="small" onClick={() => onOpenVisionDetail?.(record)}>查看关联</Button>
          {record.status !== 'active' && (
            <Button size="small" onClick={() => onUpdateVision(record.id, { status: 'active' })}>设为 Active</Button>
          )}
          <Button size="small" onClick={() => setVisionForm({ ...record })}>编辑</Button>
        </Space>
      )
    }
  ];

  return (
    <div className="view-stack">
      <Card className="console-card" title="定义项目愿景" bordered={false}>
        <Form layout="vertical" onFinish={() => onCreateVision(visionForm)}>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item label="愿景标题" required>
                <Input value={visionForm.title} onChange={e => setVisionForm({ ...visionForm, title: e.target.value })} />
              </Form.Item>
            </Col>
            <Col span={6}>
              <Form.Item label="状态">
                <Select value={visionForm.status} onChange={v => setVisionForm({ ...visionForm, status: v })} options={VISION_STATUSES.map(s => ({ value: s, label: s }))} />
              </Form.Item>
            </Col>
            <Col span={6}>
              <Form.Item label="时间跨度">
                <Select value={visionForm.horizon} onChange={v => setVisionForm({ ...visionForm, horizon: v })} options={[{ value: 'short_term' }, { value: 'long_term' }, { value: 'north_star' }]} />
              </Form.Item>
            </Col>
            <Col span={24}>
              <Form.Item label="愿景详述">
                <TextArea rows={3} value={visionForm.summary} onChange={e => setVisionForm({ ...visionForm, summary: e.target.value })} />
              </Form.Item>
            </Col>
          </Row>
          <Button type="primary" htmlType="submit" loading={busy}>{visionForm.id ? '更新愿景' : '创建愿景'}</Button>
        </Form>
      </Card>
      <Card className="console-card" title="愿景历史" bordered={false}>
        <Table rowKey="id" columns={columns} dataSource={visions} pagination={false} />
      </Card>
    </div>
  );
}

function PrinciplesView({ principles, principleForm, setPrincipleForm, onCreatePrinciple, onUpdatePrinciple, tasks, decisions, busy }) {
  const getRelatedCount = (principle) => {
    const kindMap = {
      'governance': decisions,
      'engineering': tasks,
      'product': tasks,
      'meta': []
    };
    const related = kindMap[principle.kind] || [];
    return related.length;
  };

  return (
    <div className="view-stack">
      <Card className="console-card" title="维护项目原则" bordered={false}>
        <Form layout="vertical" onFinish={() => onCreatePrinciple(principleForm)}>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item label="原则标题" required>
                <Input value={principleForm.title} onChange={e => setPrincipleForm({ ...principleForm, title: e.target.value })} />
              </Form.Item>
            </Col>
            <Col span={6}>
              <Form.Item label="类别">
                <Select value={principleForm.kind} onChange={v => setPrincipleForm({ ...principleForm, kind: v })} options={PRINCIPLE_KINDS.map(k => ({ value: k, label: k }))} />
              </Form.Item>
            </Col>
            <Col span={6}>
              <Form.Item label="状态">
                <Select value={principleForm.status} onChange={v => setPrincipleForm({ ...principleForm, status: v })} options={PRINCIPLE_STATUSES.map(s => ({ value: s, label: s }))} />
              </Form.Item>
            </Col>
            <Col span={24}>
              <Form.Item label="原则描述">
                <TextArea rows={2} value={principleForm.summary} onChange={e => setPrincipleForm({ ...principleForm, summary: e.target.value })} />
              </Form.Item>
            </Col>
          </Row>
          <Button type="primary" htmlType="submit" loading={busy}>{principleForm.id ? '更新原则' : '添加原则'}</Button>
        </Form>
      </Card>
      <Row gutter={[16, 16]}>
        {principles.map(p => (
          <Col key={p.id} xs={24} md={12} xl={8}>
            <Card className="console-card principle-card" bordered={false} actions={[
              <Button type="link" onClick={() => setPrincipleForm({ ...p })}>编辑</Button>,
              <Button type="link" onClick={() => onUpdatePrinciple(p.id, { status: p.status === 'active' ? 'archived' : 'active' })}>
                {p.status === 'active' ? '存档' : '激活'}
              </Button>
            ]}>
              <Space direction="vertical">
                <Space wrap>
                  <Tag color={statusColor(p.status)}>{p.status}</Tag>
                  <Tag>{p.kind}</Tag>
                </Space>
                <Text strong size="large">{p.title}</Text>
                <Paragraph ellipsis={{ rows: 3 }}>{p.summary}</Paragraph>
                <Text type="secondary" size="small">关联 {getRelatedCount(p)} 项</Text>
              </Space>
            </Card>
          </Col>
        ))}
      </Row>
    </div>
  );
}

function CodeView({ codeStatus, recentCommits, loading, onCommitFiles, onViewCommit }) {
  if (loading && !codeStatus) return <Spin />;

  const handleCommitFiles = (files) => {
    if (onCommitFiles) {
      onCommitFiles(files);
    }
  };

  const handleViewCommit = (commit) => {
    if (onViewCommit) {
      onViewCommit(commit);
    }
  };

  return (
    <div className="view-stack">
      <Card className="console-card" title="Git 工作区状态" bordered={false} extra={
        <Space>
          <Tag color="blue">{codeStatus?.branch}</Tag>
          {(codeStatus?.staged?.length > 0 || codeStatus?.unstaged?.length > 0) && (
            <Button size="small" type="primary" onClick={() => {
              const allFiles = [...(codeStatus?.staged || []), ...(codeStatus?.unstaged || [])];
              handleCommitFiles(allFiles);
            }}>登记提交</Button>
          )}
        </Space>
      }>
        <Row gutter={16}>
          <Col span={8}>
            <Statistic title="已暂存 (Staged)" value={codeStatus?.staged?.length || 0} />
            <List size="small" dataSource={codeStatus?.staged || []} renderItem={f => (
              <List.Item>
                <Text code>{f}</Text>
              </List.Item>
            )} />
          </Col>
          <Col span={8}>
            <Statistic title="未暂存 (Unstaged)" value={codeStatus?.unstaged?.length || 0} />
            <List size="small" dataSource={codeStatus?.unstaged || []} renderItem={f => (
              <List.Item>
                <Text code type="warning">{f}</Text>
              </List.Item>
            )} />
          </Col>
          <Col span={8}>
            <Statistic title="未追踪 (Untracked)" value={codeStatus?.untracked?.length || 0} />
            <List size="small" dataSource={codeStatus?.untracked || []} renderItem={f => (
              <List.Item>
                <Text code type="secondary">{f}</Text>
              </List.Item>
            )} />
          </Col>
        </Row>
        {(codeStatus?.staged?.length > 0 || codeStatus?.unstaged?.length > 0) && (
          <Divider orientation="left" plain>快速操作</Divider>
        )}
        {codeStatus?.staged?.length > 0 && (
          <Space wrap style={{ marginBottom: 8 }}>
            <Text type="secondary">已暂存文件:</Text>
            {codeStatus.staged.slice(0, 5).map(f => (
              <Tag key={f} closable onClose={() => {}} onClick={() => handleCommitFiles([f])}>{f}</Tag>
            ))}
            {codeStatus.staged.length > 5 && <Text type="secondary">等 {codeStatus.staged.length} 个文件</Text>}
          </Space>
        )}
      </Card>
      <Card className="console-card" title="Git 近期提交历史" bordered={false}>
        <List
          dataSource={recentCommits}
          renderItem={c => (
            <List.Item
              actions={[
                <Button key="view" size="small" type="link" onClick={() => handleViewCommit(c)}>
                  查看详情
                </Button>
              ]}
            >
              <List.Item.Meta
                avatar={<BranchesOutlined />}
                title={<Text strong>{c.title}</Text>}
                description={
                  <Space direction="vertical" size={2}>
                    <Text type="secondary">{c.author} · {c.timestamp}</Text>
                    <Text code>{c.commit_hash}</Text>
                    <div className="tag-wrap">
                      {(c.files || []).slice(0, 5).map(f => <Tag key={f} size="small">{f}</Tag>)}
                      {(c.files || []).length > 5 && <Text type="secondary">等 {(c.files || []).length} 个文件</Text>}
                    </div>
                  </Space>
                }
              />
            </List.Item>
          )}
        />
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
          <Card className="console-card" title="同步规范基线" bordered={false}>
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
                同步规范
              </Button>
            </Form>
          </Card>
        </Col>
        <Col xs={24} xl={9}>
          <Card className="console-card" title="当前基线快照" bordered={false}>
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
  onDeleteLink,
  busy,
}) {
  const filteredTasks = useMemo(() => {
    return tasks.filter((task) => {
      const query = `${task.title} ${task.id} ${task.phase} ${(task.acceptance || []).join(" ")}`.toLowerCase();
      return (!taskStatusFilter || task.status === taskStatusFilter) && (!taskSearch || query.includes(taskSearch.toLowerCase()));
    });
  }, [tasks, taskSearch, taskStatusFilter]);

  const laneOrder = ["in_progress", "todo", "blocked", "done"];
  const groupedTasks = useMemo(() => {
    const byLane = Object.fromEntries(laneOrder.map((status) => [status, []]));
    filteredTasks.forEach((task) => {
      const lane = laneOrder.includes(task.status) ? task.status : "todo";
      byLane[lane].push(task);
    });
    return byLane;
  }, [filteredTasks]);

  return (
    <div className="view-stack">
      <Card className="console-card" title="登记执行任务" bordered={false}>
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
            登记任务
          </Button>
        </Form>
      </Card>

      <Card
        className="console-card"
        title="执行任务看板"
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
        <div className="task-lane-stack">
          {laneOrder.map((status) => {
            const items = groupedTasks[status] || [];
            return (
              <div key={status} className="task-lane">
                <div className="task-lane__head">
                  <Space>
                    <Tag color={statusColor(status)}>{status}</Tag>
                    <Badge count={items.length} color="#b55e32" />
                  </Space>
                </div>
                {items.length ? (
                  <List
                    dataSource={items}
                    className="task-list"
                    renderItem={(task) => (
                      <List.Item key={task.id} className="task-list__row">
                        <Space direction="vertical" size={8} style={{ width: "100%" }}>
                          <Space wrap>
                            <Tag>{task.priority}</Tag>
                            <Tag>{task.phase}</Tag>
                            <Text type="secondary">{task.id}</Text>
                          </Space>
                          <Text strong>{task.title}</Text>
                          {!!task.last_note && <Text type="secondary">{task.last_note}</Text>}
                          {!!(task.acceptance || []).length && (
                            <div className="tag-wrap">
                              {(task.acceptance || []).map((item) => (
                                <Tag key={item}>{item}</Tag>
                              ))}
                            </div>
                          )}
                          {!!(task.related_decision_titles || []).length && (
                            <div className="tag-wrap">
                              {(task.related_decision_titles || []).map((item) => (
                                <Tag key={item} color="gold">{item}</Tag>
                              ))}
                            </div>
                          )}
                          <Space wrap>
                            <Tag color={task.status_hint === "ready" ? "green" : "gold"}>{task.status_hint}</Tag>
                            {task.status !== "in_progress" && <Button size="small" onClick={() => onUpdateTask(task.id, "in_progress")}>Start</Button>}
                            {task.status !== "done" && <Button size="small" type="primary" ghost onClick={() => onUpdateTask(task.id, "done")}>Done</Button>}
                          </Space>
                        </Space>
                      </List.Item>
                    )}
                  />
                ) : (
                  <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="空" />
                )}
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
  onConvertToTask,
  onConvertToDecision,
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
      <Card className="console-card" title="记录新想法" bordered={false}>
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
            记录想法
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
                    {!!idea.impact && <Text type="secondary">影响: {idea.impact}</Text>}
                    {idea.converted_to && (
                      <Space wrap>
                        <Tag color="green">已转化</Tag>
                        {idea.converted_to_type === 'task' && (
                          <Tag color="blue">任务: {idea.converted_to_title}</Tag>
                        )}
                        {idea.converted_to_type === 'decision' && (
                          <Tag color="gold">决策: {idea.converted_to_title}</Tag>
                        )}
                      </Space>
                    )}
                    <Space wrap>
                      {idea.status === 'accepted' && !idea.converted_to && (
                        <>
                          <Button size="small" type="primary" onClick={() => onConvertToTask?.(idea)}>转化为任务</Button>
                          <Button size="small" onClick={() => onConvertToDecision?.(idea)}>转化为决策</Button>
                        </>
                      )}
                      {idea.status === 'inbox' && (
                        <Button size="small" onClick={() => onUpdateIdea(idea.id, "under_review")}>开始评审</Button>
                      )}
                      <Button size="small" onClick={() => onUpdateIdea(idea.id, "accepted")}>Accept</Button>
                      <Button size="small" danger onClick={() => onUpdateIdea(idea.id, "rejected")}>Reject</Button>
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

function DocsView({ docs, docAudit, docForm, setDocForm, onSubmitDoc, busy, decisions }) {
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
    {
      title: "关联决策",
      key: "related_decision",
      render: (_, record) => {
        if (!record.related_decision_id) return <Text type="secondary">无</Text>;
        const decision = (decisions || []).find(d => d.id === record.related_decision_id);
        return decision ? (
          <Tag color="gold">{decision.title}</Tag>
        ) : (
          <Text type="secondary">{record.related_decision_id}</Text>
        );
      }
    },
    {
      title: "Issues",
      dataIndex: "issues",
      key: "issues",
      render: (value) =>
        (value || []).length ? (
          <Space wrap>
            {(value || []).map((item) => (
              <Tag key={item} color="red">{item}</Tag>
            ))}
          </Space>
        ) : (
          <Tag color="green">clean</Tag>
        ),
    },
  ];

  return (
    <div className="view-stack">
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={10}>
          <Card className="console-card" title="更新文档状态" bordered={false}>
            <Form layout="vertical" onFinish={onSubmitDoc}>
              <Form.Item label="路径" required>
                <Input
                  value={docForm.path}
                  onChange={(event) => setDocForm((current) => ({ ...current, path: event.target.value }))}
                />
              </Form.Item>
              <Form.Item label="层级">
                <Select
                  value={docForm.layer}
                  onChange={(value) => setDocForm((current) => ({ ...current, layer: value }))}
                  options={DOC_LAYERS.map((layer) => ({ value: layer, label: layer }))}
                />
              </Form.Item>
              <Form.Item label="关联决策">
                <Select
                  allowClear
                  value={docForm.relatedDecisionId || undefined}
                  placeholder="选择关联决策（可选）"
                  onChange={(value) =>
                    setDocForm((current) => ({ ...current, relatedDecisionId: value }))
                  }
                  options={(decisions || []).map((d) => ({ value: d.id, label: d.title }))}
                />
              </Form.Item>
              <Form.Item label="真相来源">
                <Select
                  value={docForm.sourceOfTruth ? "yes" : "no"}
                  onChange={(value) =>
                    setDocForm((current) => ({ ...current, sourceOfTruth: value === "yes" }))
                  }
                  options={[{ value: "no", label: "否" }, { value: "yes", label: "是" }]}
                />
              </Form.Item>
              <Button type="primary" htmlType="submit" loading={busy}>保存状态</Button>
            </Form>
          </Card>
        </Col>
        <Col xs={24} xl={14}>
          <Card className="console-card" title="文档治理审计" bordered={false}>
            {docAudit ? (
              <Row gutter={[16, 16]}>
                <Col span={8}><Statistic title="活跃记录" value={docAudit.active_records} /></Col>
                <Col span={8}><Statistic title="真相来源" value={docAudit.source_of_truth_records} /></Col>
                <Col span={8}><Statistic title="待修复" value={docAudit.invalid_truth_records?.length || 0} /></Col>
              </Row>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />
            )}
          </Card>
        </Col>
      </Row>

      <Card className="console-card" title="文档治理清单" bordered={false}>
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
                relatedDecisionId: record.related_decision_id || undefined,
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
  onDeleteLink,
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
      <Card className="console-card" title="发起决策草案" bordered={false}>
        <Form layout="vertical" onFinish={onCreateDecision}>
          <Form.Item label="标题" required>
            <Input value={decisionForm.title} onChange={e => setDecisionForm({...decisionForm, title: e.target.value})} />
          </Form.Item>
          <Form.Item label="Background" required>
            <TextArea rows={3} value={decisionForm.background} onChange={e => setDecisionForm({...decisionForm, background: e.target.value})} />
          </Form.Item>
          <Form.Item label="决策内容" required>
            <TextArea rows={3} value={decisionForm.decision} onChange={e => setDecisionForm({...decisionForm, decision: e.target.value})} />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>创建草案</Button>
        </Form>
      </Card>

      <Card className="console-card" title="决策队列" bordered={false}>
        <List
          itemLayout="vertical"
          dataSource={filteredDecisions}
          renderItem={(decision) => (
            <List.Item>
              <Card className="inner-card" bordered={false}>
                <Space direction="vertical" size={8} style={{ width: "100%" }}>
                  <Space wrap>
                    <Tag color={statusColor(decision.status)}>{decision.status}</Tag>
                    <Text type="secondary">{decision.date} · {decision.id}</Text>
                  </Space>
                  <Text strong size="large">{decision.title}</Text>
                  {!!decision.background && <Paragraph type="secondary">{decision.background}</Paragraph>}
                  <Paragraph>{decision.decision}</Paragraph>
                  <Space wrap>
                    {decision.canon_synced && <Tag color="green">canon synced</Tag>}
                    {!!decision.linked_commit_count && <Tag color="blue">{decision.linked_commit_count} commits</Tag>}
                    {(decision.related_task_titles || []).map((item) => <Tag key={item}>{item}</Tag>)}
                  </Space>
                  <Space wrap>
                    <Button size="small" onClick={() => onUpdateDecision(decision.id, "accepted")}>采纳</Button>
                    {decision.status === "accepted" && <Button size="small" type="primary" ghost onClick={() => onCopyIntoCanon(decision.id)}>同步 Canon</Button>}
                  </Space>
                </Space>
              </Card>
            </List.Item>
          )}
        />
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
  onDeleteLink,
  busy,
}) {
  const taskTitleMap = useMemo(() => toTitleMap(tasks), [tasks]);
  const decisionTitleMap = useMemo(() => toTitleMap(decisions), [decisions]);
  const filteredCommits = useMemo(() => {
    return commits.filter((commit) => {
      const query = `${commit.title} ${commit.summary} ${commit.branch} ${commit.commit_hash}`.toLowerCase();
      return (!commitStatusFilter || commit.status === commitStatusFilter) && (!commitSearch || query.includes(commitSearch.toLowerCase()));
    });
  }, [commitSearch, commitStatusFilter, commits]);

  const columns = [
    {
      title: "提交内容",
      key: "info",
      render: (_, record) => (
        <Space direction="vertical" size={2}>
          <Text strong>{record.title}</Text>
          <Text type="secondary" size="small">{record.short_hash || record.commit_hash?.slice(0, 8)}</Text>
          {!!record.summary && <Text type="secondary">{record.summary}</Text>}
        </Space>
      ),
    },
    {
      title: "关联项",
      key: "links",
      render: (_, record) => (
        <Space direction="vertical" size={2}>
          {record.task_id && <Tag color="blue">Task: {record.task_title || taskTitleMap.get(record.task_id) || record.task_id}</Tag>}
          {record.decision_id && <Tag color="gold">Decision: {record.decision_title || decisionTitleMap.get(record.decision_id) || record.decision_id}</Tag>}
          {!!(record.files || []).length && <Text type="secondary">{record.file_count || record.files.length} files</Text>}
        </Space>
      ),
    },
    {
      title: "审查状态",
      key: "status",
      render: (_, record) => (
        <Space wrap>
          <Tag color={statusColor(record.status)}>{record.status}</Tag>
          <Tag color={record.review_status === "approved" ? "green" : "gold"}>{record.review_status}</Tag>
          <Tag color={record.status_hint === "ready" ? "green" : "gold"}>{record.status_hint}</Tag>
        </Space>
      ),
    },
    {
      title: "操作",
      key: "actions",
      render: (_, record) => (
        <Space>
          <Button size="small" onClick={() => onUpdateCommit(record.id, { review_status: 'approved' })}>批准</Button>
        </Space>
      ),
    },
  ];

  return (
    <div className="view-stack">
      <Card className="console-card" title="登记交付提交" bordered={false}>
        <Form layout="vertical" onFinish={onCreateCommit}>
          <Row gutter={16}>
            <Col span={12}><Form.Item label="标题" required><Input value={commitForm.title} onChange={e => setCommitForm({...commitForm, title: e.target.value})} /></Form.Item></Col>
            <Col span={12}><Form.Item label="分支"><Input value={commitForm.branch} onChange={e => setCommitForm({...commitForm, branch: e.target.value})} /></Form.Item></Col>
            <Col span={24}><Form.Item label="摘要"><TextArea rows={2} value={commitForm.summary} onChange={e => setCommitForm({...commitForm, summary: e.target.value})} /></Form.Item></Col>
            <Col span={12}>
              <Form.Item label="关联任务">
                <Select allowClear value={commitForm.taskId} onChange={v => setCommitForm({...commitForm, taskId: v})} options={tasks.map(t => ({ value: t.id, label: t.title }))} />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item label="关联决策">
                <Select allowClear value={commitForm.decisionId} onChange={v => setCommitForm({...commitForm, decisionId: v})} options={decisions.map(d => ({ value: d.id, label: d.title }))} />
              </Form.Item>
            </Col>
            <Col span={24}><Form.Item label="Files"><Input value={commitForm.files} placeholder="a.py | b.py" onChange={e => setCommitForm({...commitForm, files: e.target.value})} /></Form.Item></Col>
          </Row>
          <Button type="primary" htmlType="submit" loading={busy}>登记交付</Button>
        </Form>
      </Card>
      <Card className="console-card" title="交付清单" bordered={false}>
        <Table rowKey="id" columns={columns} dataSource={filteredCommits} pagination={{ pageSize: 8 }} />
      </Card>
    </div>
  );
}

function DailyView({ daily, dailyHistory, dailyForm, setDailyForm, onAppendDaily, onReplaceDaily, onLoadDate, busy }) {
  return (
    <div className="view-stack">
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={10}>
          <Card className="console-card" title="每日收口" bordered={false}>
            <Form layout="vertical" onFinish={onAppendDaily}>
              <Form.Item label="日期">
                <Input type="date" value={dailyForm.noteDate} onChange={e => setDailyForm({...dailyForm, noteDate: e.target.value})} />
              </Form.Item>
              <Form.Item label="已完成"><Input value={dailyForm.completed} placeholder="用 | 分隔" onChange={e => setDailyForm({...dailyForm, completed: e.target.value})} /></Form.Item>
              <Form.Item label="下一步"><Input value={dailyForm.next} placeholder="用 | 分隔" onChange={e => setDailyForm({...dailyForm, next: e.target.value})} /></Form.Item>
              <Space>
                <Button type="primary" htmlType="submit" loading={busy}>追加</Button>
                <Button onClick={onReplaceDaily} loading={busy}>覆盖</Button>
              </Space>
            </Form>
          </Card>
        </Col>
        <Col xs={24} xl={14}>
          <Card className="console-card" title={`日报内容 (${daily?.note_date || todayString()})`} bordered={false}>
            {daily ? (
              <Row gutter={[16, 16]}>
                <Col span={12}><Card size="small" title="已完成" bordered={false}><List size="small" dataSource={daily.completed || []} renderItem={i => <List.Item>{i}</List.Item>} /></Card></Col>
                <Col span={12}><Card size="small" title="计划" bordered={false}><List size="small" dataSource={daily.next || []} renderItem={i => <List.Item>{i}</List.Item>} /></Card></Col>
              </Row>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />
            )}
          </Card>
        </Col>
      </Row>
    </div>
  );
}

function DailyViewHuman({ daily, dailyForm, setDailyForm, onAppendDaily, onReplaceDaily, tasks, commits, onCreateTaskFromDaily, busy }) {
  const todayCommits = (commits || []).filter(c => c.created_at && c.created_at.startsWith(dailyForm.noteDate || todayString()));
  
  return (
    <div className="view-stack">
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={10}>
          <Card className="console-card" title="Daily Update" bordered={false}>
            <Form layout="vertical" onFinish={onAppendDaily}>
              <Form.Item label="Date">
                <Input type="date" value={dailyForm.noteDate} onChange={e => setDailyForm({...dailyForm, noteDate: e.target.value})} />
              </Form.Item>
              <Form.Item label="Completed">
                <Input value={dailyForm.completed} placeholder="item 1 | item 2" onChange={e => setDailyForm({...dailyForm, completed: e.target.value})} />
              </Form.Item>
              <Form.Item label="Problems">
                <Input value={dailyForm.problems} placeholder="item 1 | item 2" onChange={e => setDailyForm({...dailyForm, problems: e.target.value})} />
              </Form.Item>
              <Form.Item label="Risks">
                <Input value={dailyForm.risks} placeholder="item 1 | item 2" onChange={e => setDailyForm({...dailyForm, risks: e.target.value})} />
              </Form.Item>
              <Form.Item label="Next">
                <Input value={dailyForm.next} placeholder="item 1 | item 2" onChange={e => setDailyForm({...dailyForm, next: e.target.value})} />
              </Form.Item>
              <Space>
                <Button type="primary" htmlType="submit" loading={busy}>Append</Button>
                <Button onClick={onReplaceDaily} loading={busy}>Replace</Button>
              </Space>
            </Form>
          </Card>
          {todayCommits.length > 0 && (
            <Card className="console-card" title="今日提交" bordered={false} style={{ marginTop: 16 }}>
              <List
                size="small"
                dataSource={todayCommits}
                renderItem={c => (
                  <List.Item>
                    <Space direction="vertical" size={2}>
                      <Text strong>{c.title}</Text>
                      <Text type="secondary" size="small">{c.short_hash || c.commit_hash?.slice(0, 8)}</Text>
                      {c.task_id && <Tag color="blue">关联任务</Tag>}
                    </Space>
                  </List.Item>
                )}
              />
            </Card>
          )}
        </Col>
        <Col xs={24} xl={14}>
          <Card className="console-card" title={`Daily Note (${daily?.note_date || todayString()})`} bordered={false}>
            {daily ? (
              <Row gutter={[16, 16]}>
                <Col span={12}>
                  <Card size="small" title="Completed" bordered={false}>
                    <List size="small" dataSource={daily.completed || []} renderItem={i => <List.Item>{i}</List.Item>} />
                  </Card>
                </Col>
                <Col span={12}>
                  <Card size="small" title="Next" bordered={false}>
                    <List size="small" dataSource={daily.next || []} renderItem={i => (
                      <List.Item>
                        {i}
                        <Button size="small" type="link" onClick={() => onCreateTaskFromDaily?.(i)}>转任务</Button>
                      </List.Item>
                    )} />
                  </Card>
                </Col>
                <Col span={12}>
                  <Card size="small" title="Problems" bordered={false}>
                    <List size="small" dataSource={daily.problems || []} renderItem={i => (
                      <List.Item>
                        {i}
                        <Button size="small" type="link" onClick={() => onCreateTaskFromDaily?.(i)}>转任务</Button>
                      </List.Item>
                    )} />
                  </Card>
                </Col>
                <Col span={12}>
                  <Card size="small" title="Risks" bordered={false}>
                    <List size="small" dataSource={daily.risks || []} renderItem={i => <List.Item>{i}</List.Item>} />
                  </Card>
                </Col>
              </Row>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />
            )}
          </Card>
        </Col>
      </Row>
    </div>
  );
}

function ConsoleApp() {
  const { message } = AntdApp.useApp();
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [view, setView] = useState("dashboard");

  const [dashboard, setDashboard] = useState(null);
  const [inbox, setInbox] = useState(null);
  const [canon, setCanon] = useState(null);
  const [visions, setVisions] = useState([]);
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

  const [taskSearch, setTaskSearch] = useState("");
  const [taskStatusFilter, setTaskStatusFilter] = useState("");
  const [commitSearch, setCommitSearch] = useState("");
  const [commitStatusFilter, setCommitStatusFilter] = useState("");
  const [ideaSearch, setIdeaSearch] = useState("");
  const [ideaStatusFilter, setIdeaStatusFilter] = useState("");
  const [decisionSearch, setDecisionSearch] = useState("");
  const [decisionStatusFilter, setDecisionStatusFilter] = useState("");

  const [taskForm, setTaskForm] = useState({ title: "", acceptance: "", priority: "P1", phase: "general" });
  const [commitForm, setCommitForm] = useState({ title: "", summary: "", branch: "", taskId: "", decisionId: "", status: "draft", testStatus: "not_run", reviewStatus: "pending", files: "" });
  const [ideaForm, setIdeaForm] = useState({ title: "", summary: "", impact: "" });
  const [docForm, setDocForm] = useState({ path: "", type: "", status: "draft", layer: "exploration", sourceOfTruth: false, relatedDecisionId: undefined });
  const [decisionForm, setDecisionForm] = useState({ title: "", background: "", decision: "" });
  const [dailyForm, setDailyForm] = useState({ noteDate: todayString(), completed: "", problems: "", risks: "", next: "" });
  const [canonForm, setCanonForm] = useState({ decisionId: "", productGoal: "", engineeringFocus: "", architecture: "", addScope: "", addAvoid: "" });
  const [visionForm, setVisionForm] = useState({ id: null, title: "", summary: "", status: "active", horizon: "long_term" });
  const [principleForm, setPrincipleForm] = useState({ id: null, title: "", summary: "", kind: "governance", status: "active" });

  async function loadAll() {
    setLoading(true);
    try {
      const payload = await api("/pmai/web/bootstrap");

      setDashboard(payload.dashboard || null);
      setInbox(payload.inbox || null);
      setCanon(payload.canon || null);
      setVisions(payload.visions || []);
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

  // Ideas 转化功能
  function handleConvertIdeaToTask(idea) {
    setTaskForm({ 
      title: idea.title, 
      acceptance: "", 
      priority: "P1", 
      phase: "general" 
    });
    message.info(`已从想法 "${idea.title}" 创建任务表单`);
    setView("tasks");
  }

  function handleConvertIdeaToDecision(idea) {
    setDecisionForm({ 
      title: idea.title, 
      background: idea.summary, 
      decision: "" 
    });
    message.info(`已从想法 "${idea.title}" 创建决策表单`);
    setView("decisions");
  }

  // CodeView 交互功能
  function handleCommitFiles(files) {
    setCommitForm({ 
      ...commitForm, 
      files: files.join(" | "),
      title: `提交: ${files[0]}${files.length > 1 ? ` 等 ${files.length} 个文件` : ''}`
    });
    message.info(`已选择 ${files.length} 个文件`);
    setView("commits");
  }

  function handleViewCommit(commit) {
    message.info(`查看提交详情: ${commit.short_hash || commit.commit_hash?.slice(0, 8)}`);
    // 可以后续扩展为模态框展示
  }

  // Daily 创建任务功能
  function handleCreateTaskFromDaily(item) {
    setTaskForm({ 
      title: item, 
      acceptance: "", 
      priority: "P1", 
      phase: "general" 
    });
    message.info(`已从日报项创建任务表单: ${item}`);
    setView("tasks");
  }

  // Vision 关联查看功能
  function handleOpenVisionDetail(vision) {
    message.info(`查看愿景 "${vision.title}" 的关联项`);
    // 当前显示关联统计，后续可扩展为专门的关联视图
  }

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
          {view === "visions" && <VisionsView visions={visions} visionForm={visionForm} setVisionForm={setVisionForm} busy={busy} onCreateVision={p => runAction(() => api(p.id ? `/pmai/visions/${p.id}` : "/pmai/visions", { method: p.id ? "PATCH" : "POST", body: JSON.stringify(p) }), "Vision updated")} onUpdateVision={(id, p) => runAction(() => api(`/pmai/visions/${id}`, { method: "PATCH", body: JSON.stringify(p) }), "Vision updated")} onOpenVisionDetail={handleOpenVisionDetail} tasks={tasks} decisions={decisions} />}
          {view === "principles" && <PrinciplesView principles={principles} principleForm={principleForm} setPrincipleForm={setPrincipleForm} busy={busy} onCreatePrinciple={p => runAction(() => api(p.id ? `/pmai/principles/${p.id}` : "/pmai/principles", { method: p.id ? "PATCH" : "POST", body: JSON.stringify(p) }), "Principle updated")} onUpdatePrinciple={(id, p) => runAction(() => api(`/pmai/principles/${id}`, { method: "PATCH", body: JSON.stringify(p) }), "Principle updated")} tasks={tasks} decisions={decisions} />}
          {view === "code" && <CodeView codeStatus={codeStatus} recentCommits={recentGitCommits} loading={loading} onCommitFiles={handleCommitFiles} onViewCommit={handleViewCommit} />}
          {view === "tasks" && <TasksView tasks={tasks} taskSearch={taskSearch} taskStatusFilter={taskStatusFilter} setTaskSearch={setTaskSearch} setTaskStatusFilter={setTaskStatusFilter} taskForm={taskForm} setTaskForm={setTaskForm} busy={busy} onCreateTask={() => runAction(() => api("/pmai/tasks", { method: "POST", body: JSON.stringify(buildTaskPayload(taskForm)) }), "Task created")} onUpdateTask={(id, s) => runAction(() => api(`/pmai/tasks/${id}`, { method: "PATCH", body: JSON.stringify({ status: s }) }), "Task updated")} onDeleteLink={id => runAction(() => api(`/pmai/links/${id}`, { method: "DELETE" }), "Link deleted")} />}
          {view === "canon" && <CanonView canon={canon} decisions={decisions} canonForm={canonForm} setCanonForm={setCanonForm} busy={busy} onSubmitCanon={() => runAction(() => api("/pmai/canon/update", { method: "POST", body: JSON.stringify(buildCanonPayload(canonForm)) }), "Canon updated")} />}
          {view === "commits" && <CommitsView commits={commits} tasks={tasks} decisions={decisions} commitSearch={commitSearch} commitStatusFilter={commitStatusFilter} setCommitSearch={setCommitSearch} setCommitStatusFilter={setCommitStatusFilter} commitForm={commitForm} setCommitForm={setCommitForm} busy={busy} onCreateCommit={() => runAction(() => api("/pmai/commits", { method: "POST", body: JSON.stringify(buildCommitPayload(commitForm)) }), "Commit registered")} onUpdateCommit={(id, p) => runAction(() => api(`/pmai/commits/${id}`, { method: "PATCH", body: JSON.stringify(p) }), "Commit updated")} onDeleteLink={id => runAction(() => api(`/pmai/links/${id}`, { method: "DELETE" }), "Link deleted")} />}
          {view === "ideas" && <IdeasView ideas={ideas} ideaSearch={ideaSearch} ideaStatusFilter={ideaStatusFilter} setIdeaSearch={setIdeaSearch} setIdeaStatusFilter={setIdeaStatusFilter} ideaForm={ideaForm} setIdeaForm={setIdeaForm} busy={busy} onCreateIdea={() => runAction(() => api("/pmai/ideas", { method: "POST", body: JSON.stringify(ideaForm) }), "Idea created")} onUpdateIdea={(id, s) => runAction(() => api(`/pmai/ideas/${id}`, { method: "PATCH", body: JSON.stringify({ status: s }) }), "Idea updated")} onConvertToTask={handleConvertIdeaToTask} onConvertToDecision={handleConvertIdeaToDecision} />}
          {view === "docs" && <DocsView docs={docs} docAudit={docAudit} docForm={docForm} setDocForm={setDocForm} busy={busy} onSubmitDoc={() => runAction(() => api("/pmai/docs", { method: "PATCH", body: JSON.stringify(buildDocPayload(docForm)) }), "Doc updated")} decisions={decisions} />}
          {view === "decisions" && <DecisionsView decisions={decisions} decisionSearch={decisionSearch} decisionStatusFilter={decisionStatusFilter} setDecisionSearch={setDecisionSearch} setDecisionStatusFilter={setDecisionStatusFilter} decisionForm={decisionForm} setDecisionForm={setDecisionForm} busy={busy} onCreateDecision={() => runAction(() => api("/pmai/decisions", { method: "POST", body: JSON.stringify(decisionForm) }), "Decision created")} onUpdateDecision={(id, s) => runAction(() => api(`/pmai/decisions/${id}`, { method: "PATCH", body: JSON.stringify({ status: s }) }), "Decision updated")} onCopyIntoCanon={id => { setCanonForm({...canonForm, decisionId: id}); setView("canon"); }} onDeleteLink={id => runAction(() => api(`/pmai/links/${id}`, { method: "DELETE" }), "Link deleted")} />}
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
