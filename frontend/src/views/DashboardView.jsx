import { useState, useEffect } from "react";
import { Button, Card, Space, Tag, Table, Typography, message } from "antd";
import { ReloadOutlined, DeleteOutlined } from "@ant-design/icons";
import { api } from "../utils/api";

const { Title, Text } = Typography;

export default function DashboardView() {
  const [loading, setLoading] = useState(false);
  const [status, setStatus] = useState(null);
  const [traffic, setTraffic] = useState([]);

  async function load() {
    setLoading(true);
    try {
      const s = await api("/pmai/proxy-status");
      setStatus(s);
      const t = await api("/pmai/proxy-traffic");
      setTraffic(t.traffic || []);
    } catch {
      setStatus({ running: false });
    }
    setLoading(false);
  }

  useEffect(() => { load(); }, []);

  async function clearTraffic() {
    await api("/pmai/proxy-traffic", { method: "DELETE" });
    message.success("流量记录已清空");
    load();
  }

  const columns = [
    { title: "Time", dataIndex: "time", key: "time", width: 80 },
    { title: "Agent", dataIndex: "agent", key: "agent", width: 80,
      render: (v) => <Tag>{v}</Tag> },
    { title: "Method", dataIndex: "method", key: "method", width: 60 },
    { title: "Path", dataIndex: "path", key: "path", ellipsis: true },
    { title: "Status", dataIndex: "status", key: "status", width: 70,
      render: (v) => <Tag color={v < 400 ? "green" : "red"}>{v}</Tag> },
    { title: "Size", dataIndex: "size", key: "size", width: 70,
      render: (v) => `${(v / 1024).toFixed(1)}K` },
  ];

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Title level={4} style={{ margin: 0 }}>Proxy 仪表板</Title>
        <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>
        <Button icon={<DeleteOutlined />} onClick={clearTraffic} danger>清空流量</Button>
      </Space>

      {status && (
        <Card size="small" title="运行状态" style={{ marginBottom: 16 }}>
          <Space wrap>
            {status.running
              ? <Tag color="green">运行中</Tag>
              : <Tag color="red">未启动</Tag>}
            {status.uptime && <Text>运行时间: {status.uptime}</Text>}
            {status.requests !== undefined && <Text>请求: {status.requests}</Text>}
            {status.errors !== undefined && <Text type={status.errors > 0 ? "danger" : "secondary"}>错误: {status.errors}</Text>}
            {status.upstream && <Text>上游: {status.upstream}</Text>}
            {status.model_override && <Text>模型: {status.model_override}</Text>}
          </Space>
        </Card>
      )}

      <Card size="small" title="最近流量">
        <Table
          dataSource={traffic}
          columns={columns}
          rowKey={(r, i) => `${r.time}-${i}`}
          size="small"
          pagination={{ pageSize: 20, size: "small" }}
          loading={loading}
          locale={{ emptyText: "暂无流量记录" }}
        />
      </Card>
    </div>
  );
}
