import { useMemo } from "react";
import { Button, Card, Col, Form, Input, Row, Select, Space, Table, Tag, Typography } from "antd";
import { statusColor, toTitleMap, buildCommitPayload } from "../utils/helpers";

const { Text } = Typography;
const { TextArea } = Input;

const COMMIT_STATUSES = ["draft", "committed", "merged", "released", "dropped"];
const COMMIT_REVIEW_STATUSES = ["pending", "approved", "changes_requested"];

export default function CommitsView({
  commits,
  tasks,
  decisions,
  commitSearch,
  commitStatusFilter,
  setCommitSearch,
  setCommitStatusFilter,
  commitForm,
  setCommitForm,
  onCreateCommit,
  onUpdateCommit,
  onDeleteLink,
  busy,
}) {
  const taskTitleMap = useMemo(() => toTitleMap(tasks), [tasks]);
  const decisionTitleMap = useMemo(() => toTitleMap(decisions), [decisions]);
  const filteredCommits = useMemo(() => {
    return commits.filter((commit) => {
      const query = `${commit.title} ${commit.summary} ${commit.branch} ${commit.commit_hash}`.toLowerCase();
      return (!commitStatusFilter || commit.status === commitStatusFilter) && (!commitSearch || query.includes(commitSearch.toLowerCase()));
    });
  }, [commitSearch, commitStatusFilter, commits]);

  const columns = [
    {
      title: "提交内容",
      key: "info",
      render: (_, record) => (
        <Space direction="vertical" size={2}>
          <Text strong>{record.title}</Text>
          <Text type="secondary" size="small">{record.short_hash || record.commit_hash?.slice(0, 8)}</Text>
          {!!record.summary && <Text type="secondary">{record.summary}</Text>}
        </Space>
      ),
    },
    {
      title: "关联项",
      key: "links",
      render: (_, record) => (
        <Space direction="vertical" size={2}>
          {record.task_id && <Tag color="blue">Task: {record.task_title || taskTitleMap.get(record.task_id) || record.task_id}</Tag>}
          {record.decision_id && <Tag color="gold">Decision: {record.decision_title || decisionTitleMap.get(record.decision_id) || record.decision_id}</Tag>}
          {!!(record.files || []).length && <Text type="secondary">{record.file_count || record.files.length} files</Text>}
        </Space>
      ),
    },
    {
      title: "审查状态",
      key: "status",
      render: (_, record) => (
        <Space wrap>
          <Tag color={statusColor(record.status)}>{record.status}</Tag>
          <Tag color={record.review_status === "approved" ? "green" : "gold"}>{record.review_status}</Tag>
          <Tag color={record.status_hint === "ready" ? "green" : "gold"}>{record.status_hint}</Tag>
        </Space>
      ),
    },
    {
      title: "操作",
      key: "actions",
      render: (_, record) => (
        <Space>
          <Button size="small" onClick={() => onUpdateCommit(record.id, { review_status: 'approved' })}>批准</Button>
        </Space>
      ),
    },
  ];

  return (
    <div className="view-stack">
      <Card className="console-card" title="登记交付提交" bordered={false}>
        <Form layout="vertical" onFinish={onCreateCommit}>
          <Row gutter={16}>
            <Col span={12}><Form.Item label="标题" required><Input value={commitForm.title} onChange={e => setCommitForm({...commitForm, title: e.target.value})} /></Form.Item></Col>
            <Col span={12}><Form.Item label="分支"><Input value={commitForm.branch} onChange={e => setCommitForm({...commitForm, branch: e.target.value})} /></Form.Item></Col>
            <Col span={24}><Form.Item label="摘要"><TextArea rows={2} value={commitForm.summary} onChange={e => setCommitForm({...commitForm, summary: e.target.value})} /></Form.Item></Col>
            <Col span={12}>
              <Form.Item label="关联任务">
                <Select allowClear value={commitForm.taskId} onChange={v => setCommitForm({...commitForm, taskId: v})} options={tasks.map(t => ({ value: t.id, label: t.title }))} />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item label="关联决策">
                <Select allowClear value={commitForm.decisionId} onChange={v => setCommitForm({...commitForm, decisionId: v})} options={decisions.map(d => ({ value: d.id, label: d.title }))} />
              </Form.Item>
            </Col>
            <Col span={24}><Form.Item label="Files"><Input value={commitForm.files} placeholder="a.py | b.py" onChange={e => setCommitForm({...commitForm, files: e.target.value})} /></Form.Item></Col>
          </Row>
          <Button type="primary" htmlType="submit" loading={busy}>登记交付</Button>
        </Form>
      </Card>
      <Card className="console-card" title="交付清单" bordered={false}>
        <Table rowKey="id" columns={columns} dataSource={filteredCommits} pagination={{ pageSize: 8 }} />
      </Card>
    </div>
  );
}
