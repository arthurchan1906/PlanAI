import { Button, Card, Col, Empty, Form, Input, Row, Select, Space, Statistic, Table, Tag, Typography } from "antd";
import { CheckCircleOutlined } from "@ant-design/icons";
import { statusColor, DOC_LAYERS } from "../utils/helpers";

const { Text } = Typography;

export default function DocsView({ docs, docAudit, docForm, setDocForm, onSubmitDoc, busy, decisions }) {
  const columns = [
    { title: "路径", dataIndex: "path", key: "path" },
    { title: "类型", dataIndex: "type", key: "type" },
    { title: "层级", dataIndex: "layer", key: "layer" },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      render: (value) => <Tag color={statusColor(value)}>{value}</Tag>,
    },
    {
      title: "真相",
      dataIndex: "source_of_truth",
      key: "source_of_truth",
      render: (value) => (value ? <CheckCircleOutlined /> : "-"),
    },
    {
      title: "关联决策",
      key: "related_decision",
      render: (_, record) => {
        if (!record.related_decision_id) return <Text type="secondary">无</Text>;
        const decision = (decisions || []).find(d => d.id === record.related_decision_id);
        return decision ? (
          <Tag color="gold">{decision.title}</Tag>
        ) : (
          <Text type="secondary">{record.related_decision_id}</Text>
        );
      }
    },
    {
      title: "Issues",
      dataIndex: "issues",
      key: "issues",
      render: (value) =>
        (value || []).length ? (
          <Space wrap>
            {(value || []).map((item) => (
              <Tag key={item} color="red">{item}</Tag>
            ))}
          </Space>
        ) : (
          <Tag color="green">clean</Tag>
        ),
    },
  ];

  return (
    <div className="view-stack">
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={10}>
          <Card className="console-card" title="更新文档状态" bordered={false}>
            <Form layout="vertical" onFinish={onSubmitDoc}>
              <Form.Item label="路径" required>
                <Input
                  value={docForm.path}
                  onChange={(event) => setDocForm((current) => ({ ...current, path: event.target.value }))}
                />
              </Form.Item>
              <Form.Item label="层级">
                <Select
                  value={docForm.layer}
                  onChange={(value) => setDocForm((current) => ({ ...current, layer: value }))}
                  options={DOC_LAYERS.map((layer) => ({ value: layer, label: layer }))}
                />
              </Form.Item>
              <Form.Item label="关联决策">
                <Select
                  allowClear
                  value={docForm.relatedDecisionId || undefined}
                  placeholder="选择关联决策（可选）"
                  onChange={(value) =>
                    setDocForm((current) => ({ ...current, relatedDecisionId: value }))
                  }
                  options={(decisions || []).map((d) => ({ value: d.id, label: d.title }))}
                />
              </Form.Item>
              <Form.Item label="真相来源">
                <Select
                  value={docForm.sourceOfTruth ? "yes" : "no"}
                  onChange={(value) =>
                    setDocForm((current) => ({ ...current, sourceOfTruth: value === "yes" }))
                  }
                  options={[{ value: "no", label: "否" }, { value: "yes", label: "是" }]}
                />
              </Form.Item>
              <Button type="primary" htmlType="submit" loading={busy}>保存状态</Button>
            </Form>
          </Card>
        </Col>
        <Col xs={24} xl={14}>
          <Card className="console-card" title="文档治理审计" bordered={false}>
            {docAudit ? (
              <Row gutter={[16, 16]}>
                <Col span={8}><Statistic title="活跃记录" value={docAudit.active_records} /></Col>
                <Col span={8}><Statistic title="真相来源" value={docAudit.source_of_truth_records} /></Col>
                <Col span={8}><Statistic title="待修复" value={docAudit.invalid_truth_records?.length || 0} /></Col>
              </Row>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />
            )}
          </Card>
        </Col>
      </Row>

      <Card className="console-card" title="文档治理清单" bordered={false}>
        <Table
          rowKey="path"
          columns={columns}
          dataSource={docs}
          pagination={{ pageSize: 8 }}
          onRow={(record) => ({
            onClick: () =>
              setDocForm({
                path: record.path || "",
                type: record.type || "",
                status: record.status || "draft",
                layer: record.layer || "exploration",
                sourceOfTruth: !!record.source_of_truth,
                relatedDecisionId: record.related_decision_id || undefined,
              }),
          })}
        />
      </Card>
    </div>
  );
}
