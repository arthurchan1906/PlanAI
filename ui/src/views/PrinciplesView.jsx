import { Button, Card, Col, Form, Input, Row, Select, Space, Tag, Typography } from "antd";
import { statusColor, PRINCIPLE_STATUSES, PRINCIPLE_KINDS } from "../utils/helpers";

const { Text, Paragraph } = Typography;

export default function PrinciplesView({ principles, principleForm, setPrincipleForm, onCreatePrinciple, onUpdatePrinciple, tasks, decisions, busy }) {
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
                <Input.TextArea rows={2} value={principleForm.summary} onChange={e => setPrincipleForm({ ...principleForm, summary: e.target.value })} />
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
