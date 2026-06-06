import { useState, useMemo } from "react";
import { Button, Input, Select, Space, Table, Tag, Typography } from "antd";
import { ReloadOutlined, SearchOutlined } from "@ant-design/icons";

const { Title } = Typography;

export default function AuditView({ auditLogs, loading, loadAll }) {
  const [search, setSearch] = useState("");
  const [actorType, setActorType] = useState("");

  const filteredLogs = useMemo(() => {
    return auditLogs.filter(log => {
      const matchSearch = !search || (log.summary || "").toLowerCase().includes(search.toLowerCase());
      const matchActor = !actorType || log.actor_type === actorType;
      return matchSearch && matchActor;
    });
  }, [auditLogs, search, actorType]);

  const columns = [
    { title: "时间", dataIndex: "created_at", key: "created_at", width: 160 },
    { title: "操作者", dataIndex: "actor_type", key: "actor_type", width: 80, render: (t) => <Tag>{t}</Tag> },
    { title: "操作", dataIndex: "action", key: "action", width: 180 },
    { title: "实体", dataIndex: "entity_type", key: "entity_type", width: 120 },
    { title: "摘要", dataIndex: "summary", key: "summary", ellipsis: true },
  ];

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Title level={4} style={{ margin: 0 }}>📝 审计日志</Title>
        <Input prefix={<SearchOutlined />} placeholder="搜索摘要..." value={search}
          onChange={e => setSearch(e.target.value)} style={{ width: 220 }} />
        <Select allowClear placeholder="操作者类型" value={actorType || undefined}
          onChange={v => setActorType(v || "")} style={{ width: 120 }}
          options={[{ value: "human", label: "human" }, { value: "agent", label: "agent" }]} />
        <Button icon={<ReloadOutlined />} onClick={loadAll} loading={loading}>刷新</Button>
      </Space>
      <Table dataSource={filteredLogs} columns={columns} rowKey="id" loading={loading} size="small" />
    </div>
  );
}
