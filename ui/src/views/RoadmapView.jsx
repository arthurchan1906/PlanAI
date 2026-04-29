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

function RelatedContext({ context, onOpenDoc, taskNotes = [] }) {
  const allLogs = useMemo(() => {
    const combined = [
      ...(context?.commits || []).map(c => ({
        type: "commit",
        date: c.created_at || c.updated_at,
        content: c.title,
        id: c.id,
        hash: c.short_hash
      })),
      ...taskNotes.map(n => ({
        type: "note",
        date: n.created_at,
        content: n.content,
        id: n.id
      }))
    ];
    
    // Group by date
    const groups = {};
    combined.sort((a, b) => new Date(b.date) - new Date(a.date)).forEach(item => {
      const dateStr = item.date ? item.date.split("T")[0] : "unknown";
      if (!groups[dateStr]) groups[dateStr] = [];
      groups[dateStr].push(item);
    });
    
    return Object.entries(groups).sort((a, b) => b[0].localeCompare(a[0]));
  }, [context, taskNotes]);

  if (!context?.idea && !allLogs.length && !context?.docs?.length) {
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
      
      {allLogs.length > 0 && (
        <div className="tree-artifact-row" style={{ gridTemplateColumns: "1fr" }}>
          <Text type="secondary" style={{ marginBottom: 12, display: "block", fontWeight: 600 }}>演进时间轴</Text>
          <div className="task-timeline">
            {allLogs.map(([date, items]) => (
              <div key={date} className="task-log-group">
                <div className="task-log-date-divider">
                  <span className="task-log-date-text">{date}</span>
                </div>
                {items.map(item => (
                  <div key={item.id} className="timeline-entry">
                    <div className={`timeline-dot timeline-dot--${item.type}`} />
                    <div className="timeline-content">
                      <div className="timeline-header">
                        <Tag className={`task-log-item-type task-log-item-type--${item.type}`}>
                          {item.type}
                        </Tag>
                        <span className="timeline-time">
                          {item.date?.includes("T") ? item.date.split("T")[1].substring(0, 5) : ""}
                        </span>
                      </div>
                      <div className="timeline-body">
                        {item.hash && <Text type="secondary" style={{ marginRight: 4, fontFamily: "monospace" }}>[{item.hash}]</Text>}
                        {item.content}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function taskItems(tasks, artifactsByTask, onOpenDoc, taskNotesMap, planTitle, roadmapTitle) {
  return tasks.map((task) => {
    const isStale = useMemo(() => {
      if (task.status === "done") return false;
      const lastActive = new Date(task.updated_at);
      const diffDays = (new Date() - lastActive) / (1000 * 60 * 60 * 24);
      return diffDays > 2;
    }, [task.updated_at, task.status]);

    const durationDays = useMemo(() => {
      const start = new Date(task.created_at || task.updated_at);
      return Math.max(1, Math.ceil((new Date() - start) / (1000 * 60 * 60 * 24)));
    }, [task.created_at, task.updated_at]);

    return {
      key: task.id,
      label: (
        <div className={`tree-node-row tree-node-row--task ${task.status === "in_progress" ? "status-pulse" : ""}`}>
          <Space direction="vertical" size={2} style={{ flex: 1 }}>
            <div className="task-breadcrumb">
              <Text type="secondary" style={{ fontSize: 10 }}>{roadmapTitle} / {planTitle}</Text>
            </div>
            <Space wrap>
              <Tag className="task-label-tag">Task</Tag>
              <Text strong>{task.title}</Text>
              <Tag color={isStale ? "warning" : "default"} style={{ fontSize: 10 }}>
                {isStale ? "2天未更新" : `进行中 ${durationDays}天`}
              </Tag>
            </Space>
            <Text type="secondary" style={{ fontSize: 11 }}>
              {task.phase} | 优先级 {task.priority}
            </Text>
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
          {!!(task.acceptance || []).length && (
            <div className="tag-wrap">
              {task.acceptance.map((item) => (
                <Tag key={`${task.id}-${acceptanceText(item)}`} color={item?.done ? "green" : "default"}>
                  {acceptanceText(item)}
                </Tag>
              ))}
            </div>
          )}
          <RelatedContext 
            context={artifactsByTask?.get(task.id)} 
            onOpenDoc={onOpenDoc} 
            taskNotes={taskNotesMap?.get(task.id) || []}
          />
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
            <Text type="secondary" style={{ fontSize: 10 }}>ID: {task.id}</Text>
            <Text type="secondary" style={{ fontSize: 10 }}>创建于: {task.created_at || task.updated_at}</Text>
          </div>
        </Space>
      ),
    };
  });
}

function PlanDetails({ plan, tasks, artifactsByTask, busy, onAdvancePlan, onOpenDoc, taskNotesMap, roadmapTitle }) {
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
          defaultActiveKey={[]}
          items={taskItems(tasks, artifactsByTask, onOpenDoc, taskNotesMap, plan.title, roadmapTitle)}
        />
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No tasks decomposed from this plan yet" />
      )}
      <PlanRecommendations recommendations={plan.recommendations} busy={busy} onAdvancePlan={onAdvancePlan} planId={plan.id} />
    </Space>
  );
}

function RoadmapTree({ roadmap, artifactsByTask, busy, onAdvancePlan, onOpenDoc, taskNotesMap }) {
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
                  <Tag className="roadmap-label-tag">Roadmap</Tag>
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
                  defaultActiveKey={[]}
                  items={roadmap.plans.map((plan) => ({
                    key: plan.id,
                    label: (
                      <div className="tree-node-row tree-node-row--plan">
                        <Space direction="vertical" size={2}>
                          <Space wrap>
                            <Tag className="plan-label-tag">Plan</Tag>
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
                    children: <PlanDetails plan={plan} tasks={roadmap.tasksByPlan[plan.id] || []} artifactsByTask={artifactsByTask} busy={busy} onAdvancePlan={onAdvancePlan} onOpenDoc={onOpenDoc} taskNotesMap={taskNotesMap} />,
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
                      children: <Collapse className="task-tree-collapse" items={taskItems(roadmap.unplannedTasks, artifactsByTask, onOpenDoc, taskNotesMap)} />,
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

export default function RoadmapView({ roadmaps, plans, tasks, taskNotes = [], visions, commits = [], docs = [], ideas = [], busy, onCreateRoadmap, onGeneratePlan, onAdvancePlan }) {
  const [roadmapForm, setRoadmapForm] = useState({ title: "", target_date: "", vision_id: "", priority: "P1", status: "planned" });
  const [planOptions, setPlanOptions] = useState({});
  const [readingDoc, setReadingDoc] = useState(null);

  const taskNotesMap = useMemo(() => {
    const map = new Map();
    (taskNotes || []).forEach(note => {
      if (!map.has(note.task_id)) map.set(note.task_id, []);
      map.get(note.task_id).push(note);
    });
    return map;
  }, [taskNotes]);
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
                  <RoadmapTree roadmap={roadmap} artifactsByTask={artifactsByTask} busy={busy} onAdvancePlan={onAdvancePlan} onOpenDoc={setReadingDoc} taskNotesMap={taskNotesMap} />
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
