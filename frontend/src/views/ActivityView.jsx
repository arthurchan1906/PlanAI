import { useState, useEffect, useMemo } from "react";
import { Card, Tag, Typography, Empty, Spin, Alert, Space, Button } from "antd";
import {
  ClockCircleOutlined, BugOutlined, BranchesOutlined,
  FileTextOutlined, AlertOutlined, NodeIndexOutlined,
  RobotOutlined, CodeOutlined, ArrowLeftOutlined,
} from "@ant-design/icons";
import {
  ReactFlow, MiniMap, Background,
  useNodesState, useEdgesState, MarkerType,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { forceSimulation, forceManyBody, forceLink, forceCenter, forceCollide } from "d3-force";
import { api } from "../utils/api";

const { Title, Text, Paragraph } = Typography;

const SOURCE_ICONS = {
  "claude-code": <RobotOutlined />,
  cursor: <CodeOutlined />,
  opencode: <CodeOutlined />,
  "gemini-cli": <CodeOutlined />,
  "codex-cli": <CodeOutlined />,
};

// ── Entity colors ──
const ENTITY_COLORS = {
  bug: "#ff4d4f", commit: "#2f6fec", task: "#52c41a",
  plan: "#722ed1", decision: "#faad14",
};
const SESSION_COLOR = "#13c2c2";
const FILE_COLOR = "#8c8c8c";

// ── Force layout ──
function layoutNodes(nodes, edges) {
  const simNodes = nodes.map((n) => ({ id: n.id }));
  const simLinks = edges
    .filter((e) => nodes.some((n) => n.id === e.source) && nodes.some((n) => n.id === e.target))
    .map((e) => ({ source: e.source, target: e.target }));

  if (simLinks.length === 0) {
    const pos = {};
    simNodes.forEach((n, i) => {
      pos[n.id] = { x: (i % 3) * 180 - 180, y: Math.floor(i / 3) * 100 - 50 };
    });
    return pos;
  }

  const sim = forceSimulation(simNodes)
    .force("charge", forceManyBody().strength(-300))
    .force("link", forceLink(simLinks).distance(120).strength(0.4))
    .force("center", forceCenter(0, 0))
    .force("collide", forceCollide(45))
    .stop();

  const N = Math.ceil(Math.log(sim.alphaMin()) / Math.log(1 - sim.alphaDecay()));
  for (let i = 0; i < N; i++) sim.tick();

  const pos = {};
  for (const n of sim.nodes()) pos[n.id] = { x: n.x, y: n.y };
  return pos;
}

// ── Graph view ──
function ActivityGraphView({ sessionId, graphEdges, sessions, onClose }) {
  const { nodes: rfNodes, edges: rfEdges } = useMemo(() => {
    if (!sessionId || !graphEdges?.length) return { nodes: [], edges: [] };

    // Filter edges connected to target session (1-hop)
    const connected = new Set([sessionId]);
    const relevant = graphEdges.filter(([s, t]) => {
      if (s === sessionId) { connected.add(t); return true; }
      if (t === sessionId) { connected.add(s); return true; }
      return false;
    });

    // Build nodes
    const nodeMap = {};
    // Center session node
    nodeMap[sessionId] = {
      id: sessionId, type: "session", label: sessionId.slice(0, 8),
      x: 0, y: 0,
    };

    for (const [s, t, rel] of relevant) {
      const other = s === sessionId ? t : s;
      if (nodeMap[other]) continue;

      if (other.startsWith("file:")) {
        const fname = other.slice(5).split("/").pop();
        nodeMap[other] = { id: other, type: "file", label: fname };
      } else {
        const parts = other.split("-");
        const etype = parts[0] || "unknown";
        nodeMap[other] = {
          id: other, type: "entity", etype,
          label: other.length > 20 ? etype + ":" + other.slice(other.length - 8) : other,
        };
      }
    }
    const nodes = Object.values(nodeMap);

    // Build edges
    const edges = relevant.map(([s, t, rel], i) => {
      const isHard = rel.includes("fixes") || rel.includes("produced") || rel.includes("refers_to");
      return {
        id: `e-${i}`,
        source: s, target: t,
        style: { stroke: isHard ? "#2f6fec" : "#ccc", strokeWidth: isHard ? 2 : 1,
                 strokeDasharray: isHard ? "" : "5,5" },
        markerEnd: { type: MarkerType.ArrowClosed, width: 12, height: 12, color: isHard ? "#2f6fec" : "#ccc" },
      };
    });

    // Layout
    const pos = layoutNodes(nodes, edges);
    for (const n of nodes) {
      if (pos[n.id]) { n.x = pos[n.id].x; n.y = pos[n.id].y; }
    }

    return { nodes, edges };
  }, [sessionId, graphEdges]);

  const [nodes, setNodes, onNodesChange] = useNodesState(rfNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(rfEdges);

  useEffect(() => { setNodes(rfNodes); setEdges(rfEdges); }, [rfNodes, rfEdges]);

  return (
    <div style={{ position: "relative", width: "100%", height: "100%" }}>
      <div style={{ position: "absolute", top: 12, left: 12, zIndex: 10 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={onClose} size="small">返回</Button>
      </div>
      <ReactFlow
        nodes={nodes} edges={edges}
        onNodesChange={onNodesChange} onEdgesChange={onEdgesChange}
        nodeTypes={graphNodeTypes}
        fitView
        style={{ background: "#fafafa" }}
      >
        <MiniMap />
        <Background gap={20} />
      </ReactFlow>
      <div style={{ position: "absolute", bottom: 12, left: 12, zIndex: 10, background: "rgba(255,255,255,0.9)", padding: "6px 10px", borderRadius: 6, fontSize: 11 }}>
        <Space size={12}>
          <span><span style={{display:"inline-block",width:10,height:10,borderRadius:"50%",background:SESSION_COLOR,marginRight:4}} /> Session</span>
          <span><span style={{display:"inline-block",width:10,height:10,borderRadius:2,background:"#2f6fec",marginRight:4}} /> Entity</span>
          <span><span style={{display:"inline-block",width:6,height:6,borderRadius:"50%",background:FILE_COLOR,marginRight:4}} /> File</span>
          <span><span style={{display:"inline-block",width:14,height:0,borderTop:"2px solid #2f6fec",marginRight:4}} /> 关联</span>
          <span><span style={{display:"inline-block",width:14,height:0,borderTop:"1px dashed #ccc",marginRight:4}} /> 推断</span>
        </Space>
      </div>
    </div>
  );
}

// ── Node type renderers ──
function SessionNode({ data }) {
  return (
    <div style={{
      width: 28, height: 28, borderRadius: "50%",
      background: SESSION_COLOR, border: "2px solid #fff",
      boxShadow: "0 1px 4px rgba(0,0,0,0.15)",
      display: "flex", alignItems: "center", justifyContent: "center",
      fontSize: 9, color: "#fff", fontWeight: 600,
    }} title={data.label}>{data.label?.slice(0, 3)}</div>
  );
}

function EntityNode({ data }) {
  const color = ENTITY_COLORS[data.etype] || "#2f6fec";
  return (
    <div style={{
      padding: "2px 8px", borderRadius: 4,
      background: "#fff", border: `1.5px solid ${color}`,
      fontSize: 9, color: "#333", maxWidth: 100,
      whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis",
      boxShadow: "0 1px 2px rgba(0,0,0,0.08)",
    }} title={data.label}>{data.label}</div>
  );
}

function FileNode({ data }) {
  return (
    <div style={{
      width: 8, height: 8, borderRadius: "50%",
      background: FILE_COLOR, border: "1px solid #fff",
      boxShadow: "0 1px 2px rgba(0,0,0,0.1)",
    }} title={data.label} />
  );
}

const graphNodeTypes = {
  session: SessionNode,
  entity: EntityNode,
  file: FileNode,
};

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
    return (
      <div style={{ position: "fixed", inset: 0, zIndex: 1000, background: "#fff" }}>
        <ActivityGraphView
          sessionId={graphSession}
          graphEdges={graph_edges}
          sessions={sessions}
          onClose={() => setGraphSession(null)}
        />
      </div>
    );
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
