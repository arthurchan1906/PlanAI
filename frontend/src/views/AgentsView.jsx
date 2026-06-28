import { useState, useMemo, useEffect, useCallback } from "react";
import { Button, Card, Input, Space, Table, Tag, Typography, message } from "antd";
import { ReloadOutlined, SearchOutlined, CodeOutlined, RobotOutlined, ThunderboltOutlined } from "@ant-design/icons";
import { api } from "../utils/api";

const { Title } = Typography;

function relativeTime(ts) {
  const diff = Math.floor((Date.now() / 1000) - ts);
  if (diff < 60) return "刚刚";
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`;
  if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`;
  return `${Math.floor(diff / 86400)} 天前`;
}

export default function AgentsView({ agents, loading, loadAll }) {
  const [search, setSearch] = useState("");
  const [launching, setLaunching] = useState(null);
  const [sessions, setSessions] = useState([]);

  const fetchSessions = useCallback(async () => {
    try {
      const data = await api("/pmai/agent/sessions");
      setSessions(data.sessions || []);
    } catch { /* ignore */ }
  }, []);

  useEffect(() => { fetchSessions(); }, [fetchSessions]);

  async function launchAgent(name) {
    setLaunching(name);
    try {
      const data = await api("/pmai/agent/launch", {
        method: "POST",
        body: JSON.stringify({ agent: name }),
      });
      if (data.ok) {
        message.success(`${name} 已启动`);
        setTimeout(fetchSessions, 500);
      } else {
        message.error(data.error || "启动失败");
      }
    } catch (e) {
      message.error(e.message);
    }
    setLaunching(null);
  }

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
      <Card size="small" title="Agent 启动器" style={{ marginBottom: 16 }}>
        <Space>
          <Button icon={<RobotOutlined />} loading={launching === "claude"}
            onClick={() => launchAgent("claude")}>
            Claude Code
          </Button>
          <Button icon={<CodeOutlined />} loading={launching === "codex"}
            onClick={() => launchAgent("codex")}>
            Codex CLI
          </Button>
          <Button icon={<ThunderboltOutlined />} loading={launching === "gemini"}
            onClick={() => launchAgent("gemini")}>
            Gemini CLI
          </Button>
        </Space>
        <div style={{ color: "#888", fontSize: 12, marginTop: 8 }}>
          在系统原生终端中启动 AI 编码 Agent，自动配置代理连接。
        </div>
      </Card>

      {sessions.length > 0 && (
        <Card size="small" title="运行中的 Agent" style={{ marginBottom: 16 }}>
          {sessions.map((s, i) => (
            <Tag key={i} color="processing" style={{ marginBottom: 4 }}>
              {s.agent} · {relativeTime(s.started_at)}
            </Tag>
          ))}
          <div style={{ color: "#888", fontSize: 11, marginTop: 6 }}>
            在系统终端窗口运行中。关闭终端窗口即终止。
          </div>
        </Card>
      )}

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
