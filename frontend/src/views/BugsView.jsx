import { useMemo } from "react";
import { Button, Card, Col, Form, Input, Row, Select, Space, Table, Tag, Typography } from "antd";
import { statusColor, toTitleMap } from "../utils/helpers";

const { Text } = Typography;
const { TextArea } = Input;

const BUG_STATUSES = ["open", "in_progress", "resolved", "closed", "wont_fix"];
const BUG_SEVERITIES = ["critical", "major", "minor", "trivial"];

const SEVERITY_COLORS = { critical: "red", major: "orange", minor: "blue", trivial: "default" };

export default function BugsView({
  bugs,
  commits,
  bugSearch,
  bugStatusFilter,
  bugSeverityFilter,
  setBugSearch,
  setBugStatusFilter,
  setBugSeverityFilter,
  bugForm,
  setBugForm,
  onCreateBug,
  onUpdateBug,
  onOpenCommit,
  busy,
}) {
  const commitTitleMap = useMemo(() => toTitleMap(commits), [commits]);
  const filteredBugs = useMemo(() => {
    return bugs.filter((bug) => {
      const query = `${bug.title} ${bug.description}`.toLowerCase();
      return (!bugStatusFilter || bug.status === bugStatusFilter)
        && (!bugSeverityFilter || bug.severity === bugSeverityFilter)
        && (!bugSearch || query.includes(bugSearch.toLowerCase()));
    });
  }, [bugSearch, bugStatusFilter, bugSeverityFilter, bugs]);

  const columns = [
    {
      title: "Bug 信息",
      key: "info",
      render: (_, record) => (
        <Space direction="vertical" size={2}>
          <Text strong>{record.title}</Text>
          {!!record.description && <Text type="secondary" style={{ maxWidth: 300 }} ellipsis>{record.description}</Text>}
        </Space>
      ),
    },
    {
      title: "严重程度",
      key: "severity",
      width: 100,
      render: (_, record) => (
        <Tag color={SEVERITY_COLORS[record.severity] || "default"}>{record.severity}</Tag>
      ),
    },
    {
      title: "状态",
      key: "status",
      width: 110,
      render: (_, record) => (
        <Tag color={statusColor(record.status)}>{record.status}</Tag>
      ),
    },
    {
      title: "关联提交",
      key: "commit",
      render: (_, record) => (
        record.commit_id
          ? <Tag color="geekblue" style={{ cursor: "pointer" }} onClick={() => onOpenCommit?.(record)}>{record.commit_title || record.commit_id}</Tag>
          : <Text type="secondary" style={{ fontSize: 11 }}>未关联提交</Text>
      ),
    },
    {
      title: "时间",
      key: "time",
      width: 140,
      render: (_, record) => {
        const dt = record.created_at || record.updated_at;
        return (
          <Space direction="vertical" size={0}>
            <Text style={{ fontSize: 13 }}>{dt ? dt.split("T")[0] : "-"}</Text>
            <Text type="secondary" style={{ fontSize: 11 }}>{dt ? dt.split("T")[1]?.substring(0, 5) : ""}</Text>
          </Space>
        );
      },
    },
    {
      title: "操作",
      key: "actions",
      width: 160,
      render: (_, record) => (
        <Space>
          {record.status === "open" && (
            <Button size="small" onClick={() => onUpdateBug(record.id, { status: "in_progress" })}>开始</Button>
          )}
          {(record.status === "open" || record.status === "in_progress") && (
            <Button size="small" onClick={() => onUpdateBug(record.id, { status: "resolved" })}>Resolve</Button>
          )}
          {record.status !== "closed" && record.status !== "wont_fix" && (
            <Button size="small" onClick={() => onUpdateBug(record.id, { status: "closed" })}>Close</Button>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div className="view-stack">
      <Card className="console-card" title="登记 Bug" bordered={false}>
        <Form layout="vertical" onFinish={onCreateBug}>
          <Row gutter={16}>
            <Col xs={24} md={12}>
              <Form.Item label="标题" required>
                <Input value={bugForm.title} onChange={e => setBugForm({...bugForm, title: e.target.value})} />
              </Form.Item>
            </Col>
            <Col xs={24} md={6}>
              <Form.Item label="严重程度">
                <Select value={bugForm.severity} onChange={v => setBugForm({...bugForm, severity: v})}
                  options={BUG_SEVERITIES.map(s => ({ value: s, label: s }))} />
              </Form.Item>
            </Col>
            <Col xs={24} md={6}>
              <Form.Item label="关联提交">
                <Select allowClear value={bugForm.commitId || undefined}
                  onChange={v => setBugForm({...bugForm, commitId: v || ""})}
                  placeholder="选择提交"
                  options={commits.map(c => ({ value: c.id, label: c.title }))} />
              </Form.Item>
            </Col>
            <Col span={24}>
              <Form.Item label="描述">
                <TextArea rows={3} value={bugForm.description} onChange={e => setBugForm({...bugForm, description: e.target.value})} />
              </Form.Item>
            </Col>
          </Row>
          <Button type="primary" htmlType="submit" loading={busy}>登记 Bug</Button>
        </Form>
      </Card>
      <Card
        className="console-card"
        title="Bug 清单"
        bordered={false}
        extra={
          <Space wrap>
            <Input value={bugSearch} onChange={e => setBugSearch(e.target.value)}
              placeholder="搜索 Bug" style={{ width: 180 }} />
            <Select value={bugStatusFilter || undefined} allowClear placeholder="全部状态"
              style={{ width: 140 }} onChange={v => setBugStatusFilter(v || "")}
              options={BUG_STATUSES.map(s => ({ value: s, label: s }))} />
            <Select value={bugSeverityFilter || undefined} allowClear placeholder="全部严重程度"
              style={{ width: 160 }} onChange={v => setBugSeverityFilter(v || "")}
              options={BUG_SEVERITIES.map(s => ({ value: s, label: s }))} />
          </Space>
        }
      >
        <Table rowKey="id" columns={columns} dataSource={filteredBugs} pagination={{ pageSize: 8 }} />
      </Card>
    </div>
  );
}
