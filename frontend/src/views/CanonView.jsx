import { Button, Card, Col, Empty, Form, Input, Row, Select, Space, Tag, Typography } from "antd";

const { Text, Paragraph } = Typography;
const { TextArea } = Input;

export default function CanonView({ canon, decisions, canonForm, setCanonForm, onSubmitCanon, busy }) {
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
