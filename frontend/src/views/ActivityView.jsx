import { useState, useEffect } from "react";
import { Card, Tag, Typography, Empty, Spin, Alert, Space } from "antd";
import {
  ClockCircleOutlined, BugOutlined, BranchesOutlined,
  FileTextOutlined, AlertOutlined, NodeIndexOutlined,
  RobotOutlined, CodeOutlined,
} from "@ant-design/icons";
import { api } from "../utils/api";

const { Title, Text, Paragraph } = Typography;

const SOURCE_ICONS = {
  "claude-code": <RobotOutlined />,
  cursor: <CodeOutlined />,
  opencode: <CodeOutlined />,
  "gemini-cli": <CodeOutlined />,
  "codex-cli": <CodeOutlined />,
};

function ActivityGraphView({ sessionId, onClose }) {
  return (
    <div style={{ padding: 24 }}>
      <Title level={5}>关联图 — {sessionId?.slice(0, 8)}</Title>
      <Text type="secondary">图视图将在下一步实现。</Text>
    </div>
  );
}

export default function ActivityView() {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [expanded, setExpanded] = useState({});
  const [graphSession, setGraphSession] = useState(null);

  useEffect(() => {
    api("/pmai/web/activity")
      .then((d) => { setData(d); setLoading(false); })
      .catch(() => setLoading(false));
  }, []);

  if (loading) return <div style={{ padding: 48, textAlign: "center" }}><Spin size="large" /></div>;
  if (!data || !data.sessions || data.sessions.length === 0) {
    return <Empty style={{ marginTop: 80 }} description="暂无 Agent 活动记录" />;
  }

  const { sessions, alerts, graph_edges } = data;

  // Group sessions by date
  const groups = {};
  for (const s of sessions) {
    const day = s.first_seen?.slice(0, 10) || "unknown";
    if (!groups[day]) groups[day] = [];
    groups[day].push(s);
  }

  const toggleExpand = (sid) => {
    setExpanded((prev) => ({ ...prev, [sid]: !prev[sid] }));
  };

  if (graphSession) {
    return <ActivityGraphView sessionId={graphSession} onClose={() => setGraphSession(null)} />;
  }

  return (
    <div style={{ padding: "16px 24px", maxWidth: 960, margin: "0 auto" }}>
      <Title level={4} style={{ marginBottom: 4 }}>Activity</Title>
      <Text type="secondary">Agent 会话时间线 — 谁做了什么、关联了什么</Text>

      {/* Alert bar */}
      {alerts && ((alerts.file_hotspots?.length > 0) || alerts.tentative_links > 0) && (
        <Alert
          style={{ marginTop: 16, marginBottom: 8 }}
          type="warning"
          showIcon
          icon={<AlertOutlined />}
          message={
            <Space size={16}>
              {alerts.file_hotspots?.length > 0 && (
                <span>🔥 热点文件: {alerts.file_hotspots.map((h) => h.file).join(", ")}</span>
              )}
              {alerts.tentative_links > 0 && (
                <span>🔗 {alerts.tentative_links} 个待确认关联</span>
              )}
            </Space>
          }
        />
      )}

      {/* Timeline */}
      {Object.keys(groups)
        .sort((a, b) => b.localeCompare(a))
        .map((day) => (
          <div key={day} style={{ marginTop: 24 }}>
            <Text type="secondary" style={{ fontSize: 13, fontWeight: 500 }}>
              {day}
            </Text>
            {groups[day].map((s) => {
              const isOpen = expanded[s.session_id];
              return (
                <Card
                  key={s.session_id}
                  size="small"
                  style={{ marginTop: 8, cursor: "pointer" }}
                  onClick={() => toggleExpand(s.session_id)}
                  hoverable
                >
                  {/* Collapsed row */}
                  <div style={{ display: "flex", alignItems: "flex-start", gap: 12 }}>
                    <span style={{ fontSize: 18, marginTop: 2 }}>
                      {SOURCE_ICONS[s.source] || <RobotOutlined />}
                    </span>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div>
                        <Text strong style={{ fontSize: 14 }}>
                          {s.goal || s.first_prompt?.slice(0, 60) || "(无标题)"}
                        </Text>
                      </div>
                      <div style={{ marginTop: 4 }}>
                        <Space size={8} wrap>
                          <Tag color={s.intent === "coding" ? "blue" : s.intent === "discussion" ? "purple" : "green"}>
                            {s.intent}
                          </Tag>
                          {s.has_l2 && <Tag color="orange">AI 摘要</Tag>}
                          {s.directive && <Tag>指令</Tag>}
                          <Text type="secondary" style={{ fontSize: 12 }}>
                            {s.source} · {s.quality_score}分 · {s.msg_count}条消息
                          </Text>
                        </Space>
                      </div>
                      {/* Entity links */}
                      {s.entities?.length > 0 && (
                        <div style={{ marginTop: 6 }}>
                          {s.entities.slice(0, 5).map((eid) => {
                            const parts = eid.split("-");
                            const etype = parts[0];
                            const icons = { bug: <BugOutlined />, commit: <BranchesOutlined />, task: <FileTextOutlined /> };
                            return (
                              <Tag key={eid} style={{ marginBottom: 2 }} icon={icons[etype]}>
                                {eid.length > 24 ? eid.slice(0, 24) + "..." : eid}
                              </Tag>
                            );
                          })}
                          {s.entities.length > 5 && (
                            <Text type="secondary" style={{ fontSize: 11 }}>+{s.entities.length - 5}</Text>
                          )}
                        </div>
                      )}
                      {/* Files */}
                      {s.touched_files?.length > 0 && (
                        <div style={{ marginTop: 4 }}>
                          {s.touched_files.slice(0, 3).map((f) => (
                            <Tag key={f} style={{ fontSize: 11 }}>{f.split("/").pop()}</Tag>
                          ))}
                          {s.touched_files.length > 3 && (
                            <Text type="secondary" style={{ fontSize: 11 }}>+{s.touched_files.length - 3}</Text>
                          )}
                        </div>
                      )}
                    </div>
                    <div style={{ textAlign: "right", flexShrink: 0 }}>
                      <Text style={{ fontSize: 12, color: "#888" }}>
                        <ClockCircleOutlined /> {s.first_seen?.slice(11, 16)}
                      </Text>
                    </div>
                  </div>

                  {/* Expanded: L2 details */}
                  {isOpen && (
                    <div style={{ marginTop: 16, padding: "12px 16px", background: "#fafafa", borderRadius: 8 }}>
                      {s.has_l2 && s.goal && (
                        <Paragraph style={{ marginBottom: 8 }}>
                          <Text strong>目标：</Text>{s.goal}
                        </Paragraph>
                      )}
                      {!s.has_l2 && (
                        <Paragraph type="secondary" style={{ marginBottom: 8 }}>
                          {s.first_prompt || "暂无详细摘要（需配置 AI 以启用 L2 语义分析）"}
                        </Paragraph>
                      )}
                      <Space style={{ marginTop: 8 }}>
                        <Tag
                          color="blue"
                          style={{ cursor: "pointer" }}
                          onClick={(e) => { e.stopPropagation(); setGraphSession(s.session_id); }}
                        >
                          <NodeIndexOutlined /> 查看关联图
                        </Tag>
                      </Space>
                    </div>
                  )}
                </Card>
              );
            })}
          </div>
        ))}
    </div>
  );
}
