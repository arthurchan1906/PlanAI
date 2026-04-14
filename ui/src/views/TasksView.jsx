import { useMemo } from "react";
import { Badge, Button, Card, Col, Empty, Form, Input, List, Row, Select, Space, Tag, Typography } from "antd";
import { statusColor } from "../utils/helpers";

const { Text } = Typography;
const { TextArea } = Input;

const TASK_STATUSES = ["todo", "in_progress", "blocked", "done", "dropped"];
const laneOrder = ["in_progress", "todo", "blocked", "done"];

function getAcceptanceText(item) {
  if (typeof item === "string") {
    return item;
  }
  if (item && typeof item === "object") {
    return item.text || item.title || JSON.stringify(item);
  }
  return "";
}

function isAcceptanceDone(item) {
  return !!(item && typeof item === "object" && item.done);
}

export default function TasksView({
  tasks,
  roadmaps,
  plans,
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
  onDeleteLink,
  busy,
}) {
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
                          <Space wrap>
                            {!!task.linked_commit_count && <Tag color="blue">{task.linked_commit_count} evidence</Tag>}
                            {!!task.approved_commit_count && <Tag color="green">{task.approved_commit_count} reviewed</Tag>}
                            {!!task.verified_commit_count && <Tag color="cyan">{task.verified_commit_count} verified</Tag>}
                          </Space>
                          {!!task.latest_evidence_summary && <Text type="secondary">Evidence: {task.latest_evidence_summary}</Text>}
                          {!!(task.closure_reasons || []).length && (
                            <div className="tag-wrap">
                              {(task.closure_reasons || []).map((reason) => (
                                <Tag key={`${task.id}-${reason}`} color="red">{reason}</Tag>
                              ))}
                            </div>
                          )}
                          {!!(task.acceptance || []).length && (
                            <div className="tag-wrap">
                              {(task.acceptance || []).map((item) => (
                                <Tag key={`${task.id}-${getAcceptanceText(item)}`} color={isAcceptanceDone(item) ? "green" : "default"}>
                                  {getAcceptanceText(item)}
                                </Tag>
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
                          {!!task.source_idea && (
                            <div className="tag-wrap">
                              <Tag color="purple">idea: {task.source_idea.title || task.source_idea.id}</Tag>
                              <Button size="small" type="link" onClick={() => onOpenIdea?.(task.source_idea.id)}>
                                查看来源想法
                              </Button>
                            </div>
                          )}
                          <Space wrap>
                            <Tag color={task.status_hint === "ready" ? "green" : "gold"}>{task.status_hint}</Tag>
                            <Button size="small" type="link" onClick={() => onOpenCommitsForTask?.(task)}>查看证据</Button>
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
