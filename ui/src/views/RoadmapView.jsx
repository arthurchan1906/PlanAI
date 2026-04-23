import { useMemo, useState } from "react";
import { Button, Card, Col, Collapse, Empty, Form, Input, List, Progress, Row, Select, Space, Switch, Tag, Typography } from "antd";
import PlanRecommendations from "../components/PlanRecommendations";
import DocumentDrawer from "../components/DocumentDrawer";
import { statusColor } from "../lib/planView";

const { Paragraph, Text } = Typography;

function dateText(item) {
  return item.updated_at || item.created_at || item.target_date || "date unknown";
}

function shortGoal(plan) {
  return plan.goal || plan.manager_summary?.summary || "No plan goal recorded yet.";
}

function taskProgress(tasks) {
  const done = tasks.filter((task) => task.status === "done").length;
  return tasks.length ? Math.round((done / tasks.length) * 100) : 0;
}

function acceptanceText(item) {
  return typeof item === "string" ? item : item?.text || "";
}

function normalizePath(path) {
  return (path || "").replace(/\\/g, "/");
}

function docPathFromFile(filePath) {
  const path = normalizePath(filePath);
  if (!path) {
    return "";
  }
  if (path.startsWith("doc/") || path.startsWith("docs/")) {
    return path;
  }
  return /\.(md|mdx|rst|txt)$/i.test(path) ? path : "";
}

function RelatedContext({ context, onOpenDoc }) {
  if (!context?.idea && !context?.commits?.length && !context?.docs?.length) {
    return null;
  }

  return (
    <div className="tree-related-context">
      {!!context.idea && (
        <div className="tree-artifact-row">
          <Text type="secondary">Idea</Text>
          <Tag color="gold">{context.idea.title || context.idea.id}</Tag>
        </div>
      )}
      {!!context.docs?.length && (
        <div className="tree-artifact-row">
          <Text type="secondary">Docs</Text>
          <div className="tag-wrap">
            {context.docs.slice(0, 4).map((doc) => (
              <Tag
                key={doc.path || doc}
                className="tree-doc-tag"
                color="processing"
                onClick={() => onOpenDoc?.(doc.path || doc)}
              >
                {doc.path || doc}
              </Tag>
            ))}
            {context.docs.length > 4 && <Tag>+{context.docs.length - 4}</Tag>}
          </div>
        </div>
      )}
      {!!context.commits?.length && (
        <div className="tree-artifact-row">
          <Text type="secondary">Commits</Text>
          <Space direction="vertical" size={4}>
            {context.commits.slice(0, 3).map((commit) => (
              <Text key={commit.id} type="secondary">
                {commit.short_hash ? `${commit.short_hash} | ` : ""}{commit.title}
              </Text>
            ))}
            {context.commits.length > 3 && <Text type="secondary">+{context.commits.length - 3} more commits</Text>}
          </Space>
        </div>
      )}
    </div>
  );
}

function taskItems(tasks, artifactsByTask, onOpenDoc) {
  return tasks.map((task) => ({
    key: task.id,
    label: (
      <div className="tree-node-row tree-node-row--task">
        <Space direction="vertical" size={2}>
          <Space wrap>
            <Tag color={statusColor(task.status)}>Task</Tag>
            <Text strong>{task.title}</Text>
          </Space>
          <Text type="secondary">{dateText(task)} | {task.priority} | {task.phase}</Text>
        </Space>
        <Space wrap className="tree-node-meta">
          {!!artifactsByTask?.get(task.id)?.idea && <Tag color="gold">idea</Tag>}
          {!!artifactsByTask?.get(task.id)?.docs?.length && <Tag>{artifactsByTask.get(task.id).docs.length} docs</Tag>}
          {!!artifactsByTask?.get(task.id)?.commits?.length && <Tag color="cyan">{artifactsByTask.get(task.id).commits.length} commits</Tag>}
          <Tag color={statusColor(task.status)}>{task.status}</Tag>
        </Space>
      </div>
    ),
    children: (
      <Space direction="vertical" size={8} style={{ width: "100%" }}>
        {!!task.last_note && <Text type="secondary">{task.last_note}</Text>}
        {!!(task.acceptance || []).length && (
          <div className="tag-wrap">
            {task.acceptance.map((item) => (
              <Tag key={`${task.id}-${acceptanceText(item)}`} color={item?.done ? "green" : "default"}>
                {acceptanceText(item)}
              </Tag>
            ))}
          </div>
        )}
        <RelatedContext context={artifactsByTask?.get(task.id)} onOpenDoc={onOpenDoc} />
        <Text type="secondary">{task.id}</Text>
      </Space>
    ),
  }));
}

function PlanDetails({ plan, tasks, artifactsByTask, busy, onAdvancePlan, onOpenDoc }) {
  return (
    <Space direction="vertical" size={12} style={{ width: "100%" }}>
      <Paragraph type="secondary" style={{ marginBottom: 0 }}>{shortGoal(plan)}</Paragraph>
      {!!plan.manager_summary?.next_manager_checkpoint && (
        <div className="plan-breakdown__checkpoint">
          <Text type="secondary">Checkpoint: {plan.manager_summary.next_manager_checkpoint}</Text>
        </div>
      )}
      {tasks.length ? (
        <Collapse
          className="task-tree-collapse"
          defaultActiveKey={tasks.filter((task) => task.status === "in_progress").map((task) => task.id)}
          items={taskItems(tasks, artifactsByTask, onOpenDoc)}
        />
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No tasks decomposed from this plan yet" />
      )}
      <PlanRecommendations recommendations={plan.recommendations} busy={busy} onAdvancePlan={onAdvancePlan} planId={plan.id} />
    </Space>
  );
}

function RoadmapTree({ roadmap, artifactsByTask, busy, onAdvancePlan, onOpenDoc }) {
  const activePlanIds = roadmap.plans
    .filter((plan) => plan.status === "active" || (roadmap.tasksByPlan[plan.id] || []).some((task) => task.status === "in_progress"))
    .map((plan) => plan.id);
  return (
    <Collapse
      className="roadmap-tree-collapse"
      defaultActiveKey={[roadmap.id]}
      items={[
        {
          key: roadmap.id,
          label: (
            <div className="tree-node-row tree-node-row--roadmap">
              <Space direction="vertical" size={2}>
                <Space wrap>
                  <Tag color={statusColor(roadmap.status)}>Roadmap</Tag>
                  <Text strong>{roadmap.title}</Text>
                </Space>
                <Text type="secondary">
                  created {roadmap.created_at || "unknown"} | target {roadmap.target_date || "unset"} | {roadmap.plans.length} plans | {roadmap.tasks.length} tasks
                </Text>
              </Space>
              <Progress percent={roadmap.progress} size="small" style={{ width: 140 }} />
            </div>
          ),
          children: (
            <Space direction="vertical" size={12} style={{ width: "100%" }}>
              {roadmap.plans.length ? (
                <Collapse
                  className="plan-stack-collapse"
                  defaultActiveKey={activePlanIds}
                  items={roadmap.plans.map((plan) => ({
                    key: plan.id,
                    label: (
                      <div className="tree-node-row tree-node-row--plan">
                        <Space direction="vertical" size={2}>
                          <Space wrap>
                            <Tag color={statusColor(plan.status)}>Plan</Tag>
                            <Text strong>{plan.title}</Text>
                          </Space>
                          <Text type="secondary">{dateText(plan)} | {(roadmap.tasksByPlan[plan.id] || []).length} tasks</Text>
                        </Space>
                        <Space wrap>
                          <Progress percent={taskProgress(roadmap.tasksByPlan[plan.id] || [])} size="small" style={{ width: 100 }} />
                          <Tag color={statusColor(plan.health?.state)}>{plan.health?.state || "draft"}</Tag>
                        </Space>
                      </div>
                    ),
                    children: <PlanDetails plan={plan} tasks={roadmap.tasksByPlan[plan.id] || []} artifactsByTask={artifactsByTask} busy={busy} onAdvancePlan={onAdvancePlan} onOpenDoc={onOpenDoc} />,
                  }))}
                />
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Generate a plan to start decomposing this roadmap" />
              )}
              {!!roadmap.unplannedTasks.length && (
                <Collapse
                  className="task-tree-collapse"
                  items={[
                    {
                      key: "unplanned",
                      label: (
                        <div className="tree-node-row">
                          <Text strong>Direct roadmap tasks not yet attached to a plan</Text>
                          <Tag>{roadmap.unplannedTasks.length}</Tag>
                        </div>
                      ),
                      children: <Collapse className="task-tree-collapse" items={taskItems(roadmap.unplannedTasks, artifactsByTask, onOpenDoc)} />,
                    },
                  ]}
                />
              )}
            </Space>
          ),
        },
      ]}
    />
  );
}

export default function RoadmapView({ roadmaps, plans, tasks, visions, commits = [], docs = [], ideas = [], busy, onCreateRoadmap, onGeneratePlan, onAdvancePlan }) {
  const [roadmapForm, setRoadmapForm] = useState({ title: "", target_date: "", vision_id: "", priority: "P1", status: "planned" });
  const [planOptions, setPlanOptions] = useState({});
  const [readingDoc, setReadingDoc] = useState(null);
  const artifactsByTask = useMemo(() => {
    const docsByPath = new Map((docs || []).map((doc) => [normalizePath(doc.path), doc]));
    const ideasById = new Map((ideas || []).map((idea) => [idea.id, idea]));
    const commitsByTask = new Map();

    (commits || []).forEach((commit) => {
      if (!commit.task_id) {
        return;
      }
      const current = commitsByTask.get(commit.task_id) || [];
      commitsByTask.set(commit.task_id, [...current, commit]);
    });

    return new Map((tasks || []).map((task) => {
      const linkedCommits = commitsByTask.get(task.id) || [];
      const docPaths = new Set((task.related_docs || []).map(normalizePath).filter(Boolean));
      linkedCommits.forEach((commit) => {
        (commit.files || []).forEach((filePath) => {
          const docPath = docPathFromFile(filePath);
          if (docPath) {
            docPaths.add(docPath);
          }
        });
      });

      const taskDocs = [...docPaths]
        .map((path) => docsByPath.get(path))
        .filter(Boolean);
      const idea = task.source_idea?.id ? (ideasById.get(task.source_idea.id) || task.source_idea) : task.source_idea;
      return [task.id, { idea, commits: linkedCommits, docs: taskDocs }];
    }));
  }, [commits, docs, ideas, tasks]);

  const roadmapTree = useMemo(() => {
    return (roadmaps || []).map((roadmap) => {
      const roadmapPlans = (plans || []).filter((plan) => plan.roadmap_id === roadmap.id);
      const planIds = new Set(roadmapPlans.map((plan) => plan.id));
      const roadmapTasks = (tasks || []).filter((task) =>
        task.roadmap_id === roadmap.id || (task.plan_id && planIds.has(task.plan_id))
      );
      const tasksByPlan = Object.fromEntries(roadmapPlans.map((plan) => [plan.id, []]));
      const unplannedTasks = [];
      roadmapTasks.forEach((task) => {
        if (task.plan_id && tasksByPlan[task.plan_id]) {
          tasksByPlan[task.plan_id].push(task);
        } else {
          unplannedTasks.push(task);
        }
      });

      const doneTasks = roadmapTasks.filter((task) => task.status === "done").length;
      const progress = roadmapTasks.length ? Math.round((doneTasks / roadmapTasks.length) * 100) : 0;
      return { ...roadmap, tasks: roadmapTasks, plans: roadmapPlans, tasksByPlan, unplannedTasks, progress };
    });
  }, [roadmaps, plans, tasks]);

  function getPlanOption(roadmapId) {
    return planOptions[roadmapId] || { title: "", task_limit: 4, create_tasks: true };
  }

  function setRoadmapPlanOption(roadmapId, patch) {
    setPlanOptions((current) => ({
      ...current,
      [roadmapId]: {
        ...getPlanOption(roadmapId),
        ...patch,
      },
    }));
  }

  return (
    <div className="view-stack">
      <Card className="console-card" title="Roadmaps" bordered={false}>
        <Form layout="vertical" onFinish={() => onCreateRoadmap(roadmapForm)}>
          <Row gutter={16}>
            <Col xs={24} md={8}>
              <Form.Item label="Title" required>
                <Input value={roadmapForm.title} onChange={(event) => setRoadmapForm({ ...roadmapForm, title: event.target.value })} />
              </Form.Item>
            </Col>
            <Col xs={24} md={6}>
              <Form.Item label="Target Date">
                <Input placeholder="YYYY-MM-DD" value={roadmapForm.target_date} onChange={(event) => setRoadmapForm({ ...roadmapForm, target_date: event.target.value })} />
              </Form.Item>
            </Col>
            <Col xs={24} md={5}>
              <Form.Item label="Vision">
                <Select
                  allowClear
                  value={roadmapForm.vision_id || undefined}
                  onChange={(value) => setRoadmapForm({ ...roadmapForm, vision_id: value || "" })}
                  options={(visions || []).map((vision) => ({ label: vision.title, value: vision.id }))}
                />
              </Form.Item>
            </Col>
            <Col xs={12} md={2}>
              <Form.Item label="Priority">
                <Select value={roadmapForm.priority} onChange={(value) => setRoadmapForm({ ...roadmapForm, priority: value })} options={[{ value: "P0" }, { value: "P1" }, { value: "P2" }]} />
              </Form.Item>
            </Col>
            <Col xs={12} md={3}>
              <Form.Item label="Status">
                <Select value={roadmapForm.status} onChange={(value) => setRoadmapForm({ ...roadmapForm, status: value })} options={[{ value: "planned" }, { value: "active" }, { value: "done" }]} />
              </Form.Item>
            </Col>
          </Row>
          <Button type="primary" htmlType="submit" loading={busy}>Create roadmap</Button>
        </Form>
      </Card>

      <List
        grid={{ gutter: 16, column: 1 }}
        dataSource={roadmapTree}
        renderItem={(roadmap) => {
          const option = getPlanOption(roadmap.id);
          return (
            <Card
              className="console-card roadmap-card"
              key={roadmap.id}
              title={
                <Space wrap>
                  <Text strong>{roadmap.title}</Text>
                  {!!roadmap.target_date && <Tag color="blue">{roadmap.target_date}</Tag>}
                  <Tag color={statusColor(roadmap.status)}>{roadmap.status}</Tag>
                  <Tag>{roadmap.priority}</Tag>
                </Space>
              }
              extra={<Progress percent={roadmap.progress} size="small" style={{ width: 140 }} />}
            >
              <Row gutter={[16, 16]}>
                <Col xs={24}>
                  <Text strong>AI Planning</Text>
                  <Paragraph type="secondary">
                    Generate a manager-readable plan and optional child tasks for this roadmap.
                  </Paragraph>
                  <Space direction="vertical" size={12} style={{ width: "100%" }}>
                    <Input
                      value={option.title}
                      placeholder={`Plan focus for ${roadmap.title}`}
                      onChange={(event) => setRoadmapPlanOption(roadmap.id, { title: event.target.value })}
                    />
                    <Space wrap>
                      <Select
                        value={option.task_limit}
                        style={{ width: 140 }}
                        onChange={(value) => setRoadmapPlanOption(roadmap.id, { task_limit: value })}
                        options={[3, 4, 5, 6].map((value) => ({ value, label: `${value} tasks` }))}
                      />
                      <Space>
                        <Switch checked={option.create_tasks} onChange={(checked) => setRoadmapPlanOption(roadmap.id, { create_tasks: checked })} />
                        <Text>Create tasks</Text>
                      </Space>
                      <Button
                        type="primary"
                        loading={busy}
                        onClick={() =>
                          onGeneratePlan({
                            roadmap_id: roadmap.id,
                            title: option.title,
                            task_limit: option.task_limit,
                            create_tasks: option.create_tasks,
                          })
                        }
                      >
                        Generate plan
                      </Button>
                    </Space>
                  </Space>
                </Col>
              </Row>

              <div className="roadmap-tree-section">
                <Space direction="vertical" size={12} style={{ width: "100%" }}>
                  <Space direction="vertical" size={2}>
                    <Text strong>Roadmap to Plan to Task tree</Text>
                    <Text type="secondary">Details are collapsed by default. Dates show when each item entered or last moved in the flow.</Text>
                  </Space>
                  <RoadmapTree roadmap={roadmap} artifactsByTask={artifactsByTask} busy={busy} onAdvancePlan={onAdvancePlan} onOpenDoc={setReadingDoc} />
                </Space>
              </div>
            </Card>
          );
        }}
      />
      <DocumentDrawer docs={docs} path={readingDoc} open={!!readingDoc} onClose={() => setReadingDoc(null)} />
    </div>
  );
}
