import { useMemo } from "react";
import { Button, Card, Empty, Form, Input, List, Select, Space, Tag, Typography } from "antd";
import { statusColor } from "../utils/helpers";

const { Text, Paragraph } = Typography;
const { TextArea } = Input;

export default function DecisionsView({
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
