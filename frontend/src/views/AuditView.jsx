import { Button, Select, Space, Table, Tag, Typography } from "antd";
import { ReloadOutlined } from "@ant-design/icons";

const { Title } = Typography;

export default function AuditView({ auditLogs, loading, loadAll }) {
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
        <Button icon={<ReloadOutlined />} onClick={loadAll} loading={loading}>刷新</Button>
      </Space>
      <Table dataSource={auditLogs} columns={columns} rowKey="id" loading={loading} size="small" />
    </div>
  );
}
