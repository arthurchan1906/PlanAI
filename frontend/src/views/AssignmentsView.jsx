import { useState } from "react";
import { Button, Card, Form, Input, Select, Space, Table, Tag, Typography, message } from "antd";
import { PlusOutlined, ReloadOutlined } from "@ant-design/icons";
import { api } from "../utils/api";

const { Title } = Typography;

export default function AssignmentsView({ assignments, agents, tasks, loading, loadAll, busy }) {
  const [formVisible, setFormVisible] = useState(false);
  const [form] = Form.useForm();

  async function onFinish(values) {
    await api("/pmai/assignments", { method: "POST", body: JSON.stringify(values) });
    message.success("分配已创建");
    form.resetFields();
    setFormVisible(false);
    loadAll();
  }

  const statusColors = { assigned: "default", in_progress: "blue", done: "green" };

  const columns = [
    { title: "Agent", dataIndex: "agent_id", key: "agent_id", width: 120 },
    { title: "角色", dataIndex: "role", key: "role", render: (r) => <Tag>{r}</Tag> },
    { title: "范围", dataIndex: "scope", key: "scope", ellipsis: true },
    { title: "状态", dataIndex: "status", key: "status", render: (s) => <Tag color={statusColors[s]}>{s}</Tag> },
    { title: "分配时间", dataIndex: "assigned_at", key: "assigned_at" },
  ];

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Title level={4} style={{ margin: 0 }}>📋 角色分配</Title>
        <Button icon={<ReloadOutlined />} onClick={loadAll} loading={loading}>刷新</Button>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setFormVisible(!formVisible)}>创建分配</Button>
      </Space>

      {formVisible && (
        <Card size="small" style={{ marginBottom: 16 }}>
          <Form form={form} layout="inline" onFinish={onFinish}>
            <Form.Item name="agent_id" rules={[{ required: true }]}>
              <Select style={{ width: 200 }} placeholder="选择 Agent"
                options={agents.map(a => ({ value: a.id, label: `${a.name} (${a.role})` }))} />
            </Form.Item>
            <Form.Item name="role" rules={[{ required: true }]}>
              <Select style={{ width: 130 }} options={[
                { value: "implementer", label: "implementer" },
                { value: "reviewer", label: "reviewer" },
                { value: "insight", label: "insight" },
              ]} />
            </Form.Item>
            <Form.Item name="task_id">
              <Select style={{ width: 250 }} allowClear placeholder="关联 Task (可选)"
                options={tasks.map(t => ({ value: t.id, label: t.title }))} />
            </Form.Item>
            <Form.Item name="scope" rules={[{ required: true }]}>
              <Input placeholder="工作范围说明" style={{ width: 250 }} />
            </Form.Item>
            <Form.Item name="assigned_by" initialValue="PM">
              <Input style={{ width: 120 }} />
            </Form.Item>
            <Form.Item><Button type="primary" htmlType="submit" loading={busy}>创建</Button></Form.Item>
          </Form>
        </Card>
      )}

      <Table dataSource={assignments} columns={columns} rowKey="id" loading={loading} size="small" />
    </div>
  );
}
