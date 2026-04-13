import { useState, useMemo } from "react";
import { Badge, Button, Card, Col, Form, Input, List, Progress, Row, Select, Space, Tag, Typography } from "antd";

const { Text } = Typography;

function statusColor(status) {
  if (["accepted", "active", "done", "in_progress"].includes(status)) return "green";
  if (["rejected", "obsolete", "dropped", "blocked"].includes(status)) return "red";
  if (["superseded", "archived"].includes(status)) return "default";
  return "gold";
}

export default function RoadmapView({ roadmaps, tasks, visions, busy, onCreateRoadmap, onUpdateRoadmap }) {
  const [roadmapForm, setRoadmapForm] = useState({ title: "", target_date: "", vision_id: "", priority: "P1" });

  const activeVision = visions.find(v => v.status === 'active');

  const roadmapTree = useMemo(() => {
    return (roadmaps || []).map(rdm => {
      const rdmTasks = (tasks || []).filter(t => t.roadmap_id === rdm.id);
      const doneTasks = rdmTasks.filter(t => t.status === 'done').length;
      const progress = rdmTasks.length ? Math.round((doneTasks / rdmTasks.length) * 100) : 0;
      return { ...rdm, tasks: rdmTasks, progress };
    });
  }, [roadmaps, tasks]);

  return (
    <div className="view-stack">
      <Card className="console-card" title="制定里程碑/路线图" bordered={false}>
        <Form layout="vertical" onFinish={() => onCreateRoadmap(roadmapForm)}>
          <Row gutter={16}>
            <Col span={8}>
              <Form.Item label="标题" required>
                <Input value={roadmapForm.title} onChange={e => setRoadmapForm({...roadmapForm, title: e.target.value})} />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item label="目标日期">
                <Input placeholder="YYYY-MM-DD" value={roadmapForm.target_date} onChange={e => setRoadmapForm({...roadmapForm, target_date: e.target.value})} />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item label="关联愿景">
                <Select
                  allowClear
                  value={roadmapForm.vision_id || undefined}
                  onChange={v => setRoadmapForm({...roadmapForm, vision_id: v})}
                  options={visions.map(v => ({ label: v.title, value: v.id }))}
                />
              </Form.Item>
            </Col>
          </Row>
          <Button type="primary" htmlType="submit" loading={busy}>添加里程碑</Button>
        </Form>
      </Card>

      <div className="roadmap-container" style={{ padding: '20px 0' }}>
        <List
          grid={{ gutter: 16, column: 1 }}
          dataSource={roadmapTree}
          renderItem={rdm => (
            <Card
              className="console-card roadmap-card"
              key={rdm.id}
              title={
                <Space>
                  <Text strong>{rdm.title}</Text>
                  <Tag color="blue">{rdm.target_date}</Tag>
                  <Tag color={statusColor(rdm.status)}>{rdm.status}</Tag>
                </Space>
              }
              extra={<Progress percent={rdm.progress} size="small" style={{ width: 120 }} />}
            >
              <List
                size="small"
                dataSource={rdm.tasks}
                renderItem={task => (
                  <List.Item>
                    <Space style={{ width: '100%', justifyContent: 'space-between' }}>
                      <Space>
                        <Badge status={task.status === 'done' ? 'success' : 'processing'} />
                        <Text>{task.title}</Text>
                      </Space>
                      <Tag>{task.status}</Tag>
                    </Space>
                  </List.Item>
                )}
                locale={{ emptyText: "暂无关联任务" }}
              />
            </Card>
          )}
        />
      </div>
    </div>
  );
}
