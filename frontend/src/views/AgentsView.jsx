import { useState, useMemo } from "react";
import { Button, Input, Space, Table, Tag, Typography } from "antd";
import { ReloadOutlined, SearchOutlined } from "@ant-design/icons";

const { Title } = Typography;

export default function AgentsView({ agents, loading, loadAll }) {
  const [search, setSearch] = useState("");

  const filteredAgents = useMemo(() => {
    if (!search) return agents;
    const s = search.toLowerCase();
    return agents.filter(a =>
      (a.name || "").toLowerCase().includes(s) ||
      (a.role || "").toLowerCase().includes(s)
    );
  }, [agents, search]);

  const columns = [
    { title: "名称", dataIndex: "name", key: "name" },
    {
      title: "角色", dataIndex: "role", key: "role",
      render: (r) => {
        const colors = { reviewer: "purple", implementer: "blue", insight: "green", coder: "blue" };
        return <Tag color={colors[r] || "default"}>{r || "coder"}</Tag>;
      },
    },
    { title: "能力", dataIndex: "capabilities", key: "capabilities", render: (c) => Array.isArray(c) ? c.join(", ") : String(c || "") },
    { title: "状态", dataIndex: "status", key: "status", render: (s) => <Tag color={s === "active" ? "green" : "default"}>{s}</Tag> },
    { title: "注册时间", dataIndex: "created_at", key: "created_at" },
  ];

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Title level={4} style={{ margin: 0 }}>🤖 Agent 列表</Title>
        <Input prefix={<SearchOutlined />} placeholder="搜索 Agent..." value={search}
          onChange={e => setSearch(e.target.value)} style={{ width: 220 }} />
        <Button icon={<ReloadOutlined />} onClick={loadAll} loading={loading}>刷新</Button>
      </Space>
      <div style={{ color: "#888", marginBottom: 12 }}>
        Agent 通过 MCP 自动注册，无需手动创建。
      </div>
      <Table dataSource={filteredAgents} columns={columns} rowKey="id" loading={loading} size="small" />
    </div>
  );
}
