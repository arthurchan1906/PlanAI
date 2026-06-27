import { useEffect, useState, useCallback } from "react";
import { Table, Tag, Button, Drawer, Typography, Tabs, Space, App } from "antd";
import { ClearOutlined, ReloadOutlined } from "@ant-design/icons";

const { Text } = Typography;

const AGENT_COLORS = { gemini: "blue", codex: "green", claude: "orange", cursor: "purple", unknown: "default" };

function fmtSize(n) {
  if (!n) return "0B";
  return n > 1024 ? (n / 1024).toFixed(1) + "KB" : n + "B";
}

function tryFmt(s) {
  if (!s) return "(empty)";
  try { return JSON.stringify(JSON.parse(s), null, 2); } catch (e) { return s; }
}

export default function ProxyView() {
  const { message } = App.useApp();
  const [list, setList] = useState([]);
  const [loading, setLoading] = useState(false);
  const [detail, setDetail] = useState(null);
  const [drawerOpen, setDrawerOpen] = useState(false);

  const loadList = useCallback(async () => {
    setLoading(true);
    try {
      const r = await fetch("/__proxy/capture");
      const d = await r.json();
      setList(d.captures || []);
    } catch (e) {
      message.error("Failed to load proxy captures");
    }
    setLoading(false);
  }, [message]);

  useEffect(() => { loadList(); }, [loadList]);

  const loadDetail = async (id) => {
    try {
      const r = await fetch(`/__proxy/capture?id=${id}`);
      const d = await r.json();
      setDetail(d);
      setDrawerOpen(true);
    } catch (e) {
      message.error("Failed to load detail");
    }
  };

  const clearAll = async () => {
    await fetch("/__proxy/capture/clear", { method: "POST" });
    setList([]);
    message.success("Cleared");
  };

  const columns = [
    { title: "Time", dataIndex: "time", key: "time", width: 100 },
    {
      title: "Agent", dataIndex: "agent", key: "agent", width: 80,
      render: (a) => <Tag color={AGENT_COLORS[a] || "default"}>{a}</Tag>
    },
    {
      title: "Path", dataIndex: "path", key: "path", ellipsis: true,
      render: (_, r) => <Text style={{ fontSize: 12 }}>{r.method} {r.path}</Text>
    },
    { title: "Model", dataIndex: "model", key: "model", width: 160, ellipsis: true },
    {
      title: "Status", dataIndex: "status", key: "status", width: 70,
      render: (s) => <Tag color={s < 400 ? "green" : "red"}>{s}</Tag>
    },
    { title: "Duration", dataIndex: "duration", key: "duration", width: 80 },
    { title: "Req", dataIndex: "req_size", key: "req_size", width: 70, render: fmtSize },
    { title: "Resp", dataIndex: "resp_size", key: "resp_size", width: 70, render: fmtSize },
    {
      title: "", key: "action", width: 60,
      render: (_, r) => <Button type="link" size="small" onClick={() => loadDetail(r.id)}>View</Button>
    },
  ];

  const tabItems = detail ? [
    { key: "request", label: "Original Request", children: <pre style={{ maxHeight: "60vh", overflow: "auto", fontSize: 12, background: "#f5f5f5", padding: 12, borderRadius: 8 }}>{tryFmt(detail.req_body)}</pre> },
    { key: "unified", label: "Unified Request", children: <pre style={{ maxHeight: "60vh", overflow: "auto", fontSize: 12, background: "#f5f5f5", padding: 12, borderRadius: 8 }}>{tryFmt(detail.req_unified || "(not captured)")}</pre> },
    { key: "response", label: "Response", children: <pre style={{ maxHeight: "60vh", overflow: "auto", fontSize: 12, background: "#f5f5f5", padding: 12, borderRadius: 8 }}>{tryFmt(detail.resp_body)}</pre> },
    { key: "events", label: "SSE Events", children: <pre style={{ maxHeight: "60vh", overflow: "auto", fontSize: 12, background: "#f5f5f5", padding: 12, borderRadius: 8 }}>{tryFmt(detail.resp_events || "(not captured)")}</pre> },
  ] : [];

  return (
    <div style={{ padding: 16 }}>
      <Space style={{ marginBottom: 16 }}>
        <Button icon={<ReloadOutlined />} onClick={loadList} loading={loading}>Refresh</Button>
        <Button icon={<ClearOutlined />} danger onClick={clearAll}>Clear All</Button>
      </Space>
      <Table
        dataSource={list}
        columns={columns}
        rowKey="id"
        size="small"
        loading={loading}
        pagination={{ pageSize: 50, size: "small" }}
      />
      <Drawer
        title={detail ? `${detail.method} ${detail.path}` : ""}
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        width={780}
        placement="right"
      >
        {detail && (
          <>
            <Space style={{ marginBottom: 12 }} wrap>
              <Tag color={AGENT_COLORS[detail.agent] || "default"}>{detail.agent}</Tag>
              <Text type="secondary">{detail.time}</Text>
              <Text type="secondary">Model: {detail.model}</Text>
              <Text type="secondary">Duration: {detail.duration}</Text>
              <Tag color={detail.status < 400 ? "green" : "red"}>Status: {detail.status}</Tag>
            </Space>
            <Tabs items={tabItems} defaultActiveKey="request" />
          </>
        )}
      </Drawer>
    </div>
  );
}
