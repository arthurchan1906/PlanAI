import { Button, Card, Col, Form, Input, Row, Select, Space, Table, Tag, Typography } from "antd";
import { statusColor, VISION_STATUSES } from "../utils/helpers";

const { Text, Paragraph } = Typography;

export default function VisionsView({
  visions, visionForm, setVisionForm, onCreateVision, onUpdateVision, onOpenVisionDetail, tasks, decisions, busy }) {
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
                <Input.TextArea rows={3} value={visionForm.summary} onChange={e => setVisionForm({ ...visionForm, summary: e.target.value })} />
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
