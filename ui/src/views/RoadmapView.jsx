import { useMemo, useState } from "react";
import { Button, Card, Col, Form, Input, List, Progress, Row, Select, Space, Switch, Tag, Typography } from "antd";
import PlanSummaryCard from "../components/PlanSummaryCard";
import { statusColor } from "../lib/planView";

const { Paragraph, Text } = Typography;

export default function RoadmapView({ roadmaps, plans, tasks, visions, busy, onCreateRoadmap, onGeneratePlan, onAdvancePlan }) {
  const [roadmapForm, setRoadmapForm] = useState({ title: "", target_date: "", vision_id: "", priority: "P1", status: "planned" });
  const [planOptions, setPlanOptions] = useState({});

  const roadmapTree = useMemo(() => {
    return (roadmaps || []).map((roadmap) => {
      const roadmapTasks = (tasks || []).filter((task) => task.roadmap_id === roadmap.id);
      const roadmapPlans = (plans || []).filter((plan) => plan.roadmap_id === roadmap.id);
      const doneTasks = roadmapTasks.filter((task) => task.status === "done").length;
      const progress = roadmapTasks.length ? Math.round((doneTasks / roadmapTasks.length) * 100) : 0;
      return { ...roadmap, tasks: roadmapTasks, plans: roadmapPlans, progress };
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
                <Col xs={24} xl={14}>
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
                <Col xs={24} xl={10}>
                  <Text strong>Active plans</Text>
                  <List
                    size="small"
                    dataSource={roadmap.plans}
                    locale={{ emptyText: "No plans yet" }}
                    renderItem={(plan) => <PlanSummaryCard plan={plan} busy={busy} onAdvancePlan={onAdvancePlan} />}
                  />
                </Col>
              </Row>

              <div style={{ marginTop: 16 }}>
                <Text strong>Tasks in roadmap</Text>
                <List
                  size="small"
                  dataSource={roadmap.tasks}
                  locale={{ emptyText: "No tasks linked to this roadmap" }}
                  renderItem={(task) => (
                    <List.Item>
                      <Space style={{ width: "100%", justifyContent: "space-between" }}>
                        <Space wrap>
                          <Text>{task.title}</Text>
                          <Tag>{task.phase}</Tag>
                        </Space>
                        <Tag color={statusColor(task.status)}>{task.status}</Tag>
                      </Space>
                    </List.Item>
                  )}
                />
              </div>
            </Card>
          );
        }}
      />
    </div>
  );
}
