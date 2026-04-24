import { useMemo, useState } from "react";
import { Badge, Button, Card, Col, Empty, Form, Input, List, Row, Select, Space, Tag, Typography } from "antd";
import { statusColor } from "../utils/helpers";
import DocumentDrawer from "../components/DocumentDrawer";

const { Text, Paragraph } = Typography;

const TASK_STATUSES = ["todo", "in_progress", "blocked", "done", "dropped"];
const laneOrder = ["in_progress", "todo", "blocked", "done"];

function getAcceptanceText(item) {
  if (typeof item === "string") {
    return item;
  }
  if (item && typeof item === "object") {
    const text = item.text || item.title || "";
    if (typeof text === "string") return text;
    return JSON.stringify(text);
  }
  return String(item || "");
}

function isAcceptanceDone(item) {
  return !!(item && typeof item === "object" && item.done);
}

function TaskTimeline({ taskId, taskNotes = [], commits = [] }) {
  const allLogs = useMemo(() => {
    const combined = [
      ...(commits || []).filter(c => c.task_id === taskId).map(c => ({
        type: "commit",
        date: c.created_at || c.updated_at,
        content: c.title,
        id: c.id,
        hash: c.short_hash || (c.commit_hash ? c.commit_hash.slice(0, 7) : "")
      })),
      ...(taskNotes || []).filter(n => n.task_id === taskId).map(n => ({
        type: "note",
        date: n.created_at,
        content: n.content,
        id: n.id,
        mode: n.mode
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
  }, [taskId, taskNotes, commits]);

  if (!allLogs.length) return null;

  return (
    <div className="task-timeline" style={{ marginTop: 16, borderTop: "1px dashed #eee", paddingTop: 16 }}>
      <Text type="secondary" style={{ marginBottom: 12, display: "block", fontSize: 12, fontWeight: 600 }}>演进时间轴</Text>
      {allLogs.map(([date, items]) => (
        <div key={date} className="task-log-group">
          <div className="task-log-date-divider" style={{ marginBottom: 12 }}>
            <span className="task-log-date-text" style={{ fontSize: 10 }}>{date}</span>
          </div>
          {items.map(item => (
            <div key={item.id} className="timeline-entry" style={{ marginBottom: 12 }}>
              <div className={`timeline-dot timeline-dot--${item.mode === 'system' ? 'system' : item.type}`} />
              <div className="timeline-content" style={{ padding: "8px 12px", background: "#fcfcfc" }}>
                <div className="timeline-header">
                  <Tag className={`task-log-item-type task-log-item-type--${item.mode === 'system' ? 'system' : item.type}`} style={{ fontSize: 9 }}>
                    {item.mode === 'system' ? 'EVENT' : item.type.toUpperCase()}
                  </Tag>
                  <span className="timeline-time" style={{ fontSize: 10 }}>
                    {item.date?.includes("T") ? item.date.split("T")[1].substring(0, 5) : ""}
                  </span>
                </div>
                <div className="timeline-body" style={{ fontSize: 12 }}>
                  {item.hash && <Text type="secondary" style={{ marginRight: 4, fontFamily: "monospace" }}>[{item.hash}]</Text>}
                  {item.content}
                </div>
              </div>
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}

function RelatedArtifacts({ task, onOpenDoc, onOpenIdea }) {
  const hasArtifacts = task.source_idea || (task.related_decision_titles || []).length || (task.related_docs || []).length;
  if (!hasArtifacts) return null;

  return (
    <div style={{ marginTop: 12, background: "rgba(0,0,0,0.01)", padding: "8px 12px", borderRadius: 8 }}>
      <Text type="secondary" style={{ fontSize: 11, display: "block", marginBottom: 8, fontWeight: 600 }}>关联资产</Text>
      <Space wrap>
        {!!task.source_idea && (
          <Tag color="purple" style={{ cursor: "pointer" }} onClick={() => onOpenIdea?.(task.source_idea.id)}>
            Idea: {task.source_idea.title || task.source_idea.id}
          </Tag>
        )}
        {(task.related_decision_titles || []).map((item) => (
          <Tag key={item} color="gold">Decision: {item}</Tag>
        ))}
        {(task.related_docs || []).map((docPath) => (
          <Tag key={docPath} color="processing" style={{ cursor: "pointer" }} onClick={() => onOpenDoc?.(docPath)}>
            Doc: {docPath}
          </Tag>
        ))}
      </Space>
    </div>
  );
}

export default function TasksView({
  tasks,
  taskNotes,
  commits,
  roadmaps,
  plans,
  docs,
  taskSearch,
  taskStatusFilter,
  setTaskSearch,
  setTaskStatusFilter,
  taskForm,
  setTaskForm,
  onCreateTask,
  onUpdateTask,
  onOpenIdea,
  onOpenCommitsForTask,
  busy,
}) {
  const [readingDoc, setReadingDoc] = useState(null);
  const [expandedTasks, setExpandedTasks] = useState(new Set());

  const toggleExpand = (taskId) => {
    const next = new Set(expandedTasks);
    if (next.has(taskId)) next.delete(taskId);
    else next.add(taskId);
    setExpandedTasks(next);
  };

  const filteredTasks = useMemo(() => {
    return tasks.filter((task) => {
      const acceptanceText = (task.acceptance || []).map(getAcceptanceText).join(" ");
      const query = `${task.title} ${task.id} ${task.phase} ${acceptanceText}`.toLowerCase();
      return (!taskStatusFilter || task.status === taskStatusFilter) && (!taskSearch || query.includes(taskSearch.toLowerCase()));
    });
  }, [tasks, taskSearch, taskStatusFilter]);

  const groupedTasks = useMemo(() => {
    const byLane = Object.fromEntries(laneOrder.map((status) => [status, []]));
    filteredTasks.forEach((task) => {
      const lane = laneOrder.includes(task.status) ? task.status : "todo";
      byLane[lane].push(task);
    });
    return byLane;
  }, [filteredTasks]);

  const roadmapTitleById = useMemo(
    () => new Map((roadmaps || []).map((roadmap) => [roadmap.id, roadmap.title])),
    [roadmaps],
  );
  const planTitleById = useMemo(
    () => new Map((plans || []).map((plan) => [plan.id, plan.title])),
    [plans],
  );

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
            <Col xs={24} md={6}>
              <Form.Item label="优先级">
                <Select
                  value={taskForm.priority}
                  onChange={(value) => setTaskForm((current) => ({ ...current, priority: value }))}
                  options={[{ value: "P0" }, { value: "P1" }, { value: "P2" }]}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={6}>
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
            <Col xs={24} md={6}>
              <Form.Item label="Roadmap ID (可选)">
                <Select
                  value={taskForm.roadmapId || undefined}
                  allowClear
                  placeholder="关联路线图"
                  onChange={(value) => setTaskForm((current) => ({ ...current, roadmapId: value || "" }))}
                  options={(roadmaps || []).map(r => ({ value: r.id, label: r.title }))}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={6}>
              <Form.Item label="Plan ID (可选)">
                <Select
                  value={taskForm.planId || undefined}
                  allowClear
                  placeholder="关联计划"
                  onChange={(value) => setTaskForm((current) => ({ ...current, planId: value || "" }))}
                  options={(plans || []).map(p => ({ value: p.id, label: p.title }))}
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
                    <Tag color={statusColor(status)}>{status.toUpperCase()}</Tag>
                    <Badge count={items.length} color="#b55e32" />
                  </Space>
                </div>
                {items.length ? (
                  <List
                    dataSource={items}
                    className="task-list"
                    renderItem={(task) => {
                      const isExpanded = expandedTasks.has(task.id);
                      return (
                        <List.Item 
                          key={task.id} 
                          className={`task-list__row ${task.status === "in_progress" ? "status-pulse" : ""}`}
                          style={{ padding: "16px 20px", marginBottom: 12, cursor: "pointer", userSelect: "none" }}
                          onDoubleClick={() => toggleExpand(task.id)}
                        >
                          <Space direction="vertical" size={8} style={{ width: "100%" }}>
                            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start" }}>
                              <Space direction="vertical" size={4} style={{ flex: 1 }}>
                                <Space wrap>
                                  <Tag color={task.priority === "P0" ? "red" : "default"}>{task.priority}</Tag>
                                  <Tag>{task.phase}</Tag>
                                  {!!task.roadmap_id && <Text type="secondary" style={{ fontSize: 11 }}>[{roadmapTitleById.get(task.roadmap_id) || task.roadmap_id}]</Text>}
                                  <Text type="secondary" style={{ fontSize: 10 }}>#{task.id.split("-").pop()}</Text>
                                </Space>
                                <Text strong style={{ fontSize: 15 }}>{task.title}</Text>
                              </Space>
                              <Button 
                                type="link" 
                                size="small" 
                                onClick={() => toggleExpand(task.id)}
                                style={{ fontSize: 12 }}
                              >
                                {isExpanded ? "收起详情" : "查看详情"}
                              </Button>
                            </div>

                            <Space wrap style={{ marginTop: 4 }}>
                              {!!task.linked_commit_count && <Tag color="blue">{task.linked_commit_count} 提交</Tag>}
                              {!!task.verified_commit_count && <Tag color="cyan">已验证</Tag>}
                              {!!(task.acceptance || []).length && (
                                <Text type="secondary" style={{ fontSize: 12 }}>
                                  进度: {task.acceptance.filter(isAcceptanceDone).length}/{task.acceptance.length}
                                </Text>
                              )}
                            </Space>

                            {isExpanded && (
                              <div style={{ marginTop: 8, animation: "fadeIn 0.3s" }}>
                                {!!(task.acceptance || []).length && (
                                  <div className="tag-wrap" style={{ marginBottom: 12 }}>
                                    {(task.acceptance || []).map((item) => (
                                      <Tag key={`${task.id}-${getAcceptanceText(item)}`} color={isAcceptanceDone(item) ? "green" : "default"}>
                                        {getAcceptanceText(item)}
                                      </Tag>
                                    ))}
                                  </div>
                                )}
                                
                                <RelatedArtifacts task={task} onOpenDoc={setReadingDoc} onOpenIdea={onOpenIdea} />
                                
                                {!!(task.closure_reasons || []).length && (
                                  <div style={{ marginTop: 8 }}>
                                    {(task.closure_reasons || []).map((reason) => (
                                      <Tag key={`${task.id}-${reason}`} color="red">{reason}</Tag>
                                    ))}
                                  </div>
                                )}

                                <TaskTimeline taskId={task.id} taskNotes={taskNotes} commits={commits} />
                              </div>
                            )}

                            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginTop: 8, borderTop: "1px solid #f0f0f0", paddingTop: 10 }}>
                              <Space wrap>
                                <Button size="small" type="link" onClick={() => onOpenCommitsForTask?.(task)}>查看证据</Button>
                                {task.status !== "in_progress" && <Button size="small" onClick={() => onUpdateTask(task.id, "in_progress")}>Start</Button>}
                                {task.status !== "done" && <Button size="small" type="primary" ghost onClick={() => onUpdateTask(task.id, "done")}>Done</Button>}
                              </Space>
                              <Text type="secondary" style={{ fontSize: 10 }}>更新于: {task.updated_at}</Text>
                            </div>
                          </Space>
                        </List.Item>
                      );
                    }}
                  />
                ) : (
                  <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="空" />
                )}
              </div>
            );
          })}
        </div>
      </Card>
      <DocumentDrawer docs={docs} path={readingDoc} open={!!readingDoc} onClose={() => setReadingDoc(null)} />
    </div>
  );
}
