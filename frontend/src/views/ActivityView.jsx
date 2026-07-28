import { useState, useEffect, useRef } from "react";
import { Card, Tag, Typography, Empty, Spin, Alert, Space, Button, Modal, Popover, List } from "antd";
import {
  ClockCircleOutlined, BugOutlined, BranchesOutlined,
  FileTextOutlined, AlertOutlined, NodeIndexOutlined,
  RobotOutlined, CodeOutlined, ArrowLeftOutlined,
} from "@ant-design/icons";
import * as d3 from "d3";
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

// ── Global graph view — raw SVG + d3-force ──
function ActivityGraphView({ graphEdges, sessions, entityLabels, onClose }) {
  if (!graphEdges?.length) return null;

  const el = entityLabels || {};
  // Build commit title lookup for session labels
  const sessionCommitTitle = {};
  for (const [s, t] of graphEdges) {
    if (t.startsWith("commit:") && el[t]) sessionCommitTitle[s] = el[t];
  }
  const sessionLabel = {};
  if (sessions) {
    for (const s of sessions) {
      const goal = (s.goal && !s.goal.startsWith("{")) ? s.goal : null; // ignore nested JSON
      sessionLabel[s.session_id] = sessionCommitTitle[s.session_id] || goal || s.first_prompt?.slice(0, 40) || s.session_id?.slice(0, 8);
    }
  }

  const entityRefCount = {};
  for (const [, t, rel] of graphEdges) {
    if (!t.startsWith("file:") && rel.includes("refers_to")) entityRefCount[t] = (entityRefCount[t] || 0) + 1;
  }
  const maxRef = Math.max(1, ...Object.values(entityRefCount));

  const nodeMap = {};
  const edgeList = [];

  let skipped = 0;
  for (const [s, t, rel] of graphEdges) {
    if (s.startsWith("file:") || t.startsWith("file:")) { skipped++; continue; }
    if (!nodeMap[s]) {
      nodeMap[s] = { id: s, type: "session", data: { label: sessionLabel[s] || s.slice(0, 8) } };
    }
    if (!nodeMap[t]) {
      const cp = t.indexOf(":");
      const ep = cp >= 0 ? t.substring(cp + 1) : t;
      const dp = ep.indexOf("-");
      const et = dp >= 0 ? ep.substring(0, dp) : "entity";
      const cnt = entityRefCount[t] || 1;
      const title = el[t] || (ep.length > 24 ? et + ":" + ep.slice(-10) : ep);
      nodeMap[t] = { id: t, type: "entity", data: { label: title, etype: et, refCount: cnt, size: 0.7 + (cnt / maxRef) * 1.3 } };
    }
    edgeList.push({ source: nodeMap[s], target: nodeMap[t], hard: rel.includes("refers_to") });
  }
  // Dedup edges by source+target
  const seen = new Set();
  const deduped = [];
  for (const e of edgeList) {
    const key = e.source.id + "|" + e.target.id;
    if (seen.has(key)) continue;
    seen.add(key);
    deduped.push(e);
  }
  const originalEdgeCount = edgeList.length;
  edgeList.length = 0;
  edgeList.push(...deduped);

  console.log("SVG build: total edges", graphEdges.length, "skipped", skipped, "built edges", originalEdgeCount, "deduped", edgeList.length, "nodes", Object.keys(nodeMap).length);

  // Build simulation — write positions back to nodeMap objects
  const nodes = Object.values(nodeMap);
  const simNodes = nodes.map(n => ({ id: n.id, ref: n }));
  const simLinks = edgeList.map(e => ({
    source: simNodes.find(n => n.id === e.source.id),
    target: simNodes.find(n => n.id === e.target.id)
  }));

  const sim = forceSimulation(simNodes)
    .force("charge", forceManyBody().strength(-600))
    .force("link", forceLink(simLinks).distance(140).strength(0.5))
    .force("center", forceCenter(0, 0))
    .force("collide", forceCollide(50))
    .stop();
  for (let i = 0; i < 200; i++) sim.tick();

  // Write sim positions back to original node objects
  for (const sn of simNodes) { sn.ref.x = sn.x; sn.ref.y = sn.y; }
  console.log("SVG: nodes", nodes.length, "edges", edgeList.length);
  console.log("=== ALL NODES ===");
  nodes.forEach((n, i) => {
    console.log(`  [${i}] type=${n.type} id=${n.id.slice(0,30)} label="${n.data.label?.slice(0,50)}"`);
  });
  console.log("=== ALL EDGES ===");
  edgeList.forEach((e, i) => {
    console.log(`  [${i}] ${e.source.id.slice(0,20)} → ${e.target.id.slice(0,30)}`);
  });

  // Compute bounds
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const n of simNodes) {
    if (n.x < minX) minX = n.x; if (n.y < minY) minY = n.y;
    if (n.x > maxX) maxX = n.x; if (n.y > maxY) maxY = n.y;
  }
  const pad = 60, w = Math.max(800, maxX - minX + pad * 2), h = Math.max(600, maxY - minY + pad * 2);
  const offsetX = -minX + pad, offsetY = -minY + pad;

  const svgRef = useRef(null);
  const [tooltip, setTooltip] = useState(null);
  const dragRef = useRef(null);

  useEffect(() => {
    const svg = d3.select(svgRef.current);
    svg.selectAll("*").remove();

    // Zoom & pan
    const g = svg.append("g");
    const zoom = d3.zoom().scaleExtent([0.3, 4]).on("zoom", (ev) => g.attr("transform", ev.transform));
    svg.call(zoom);

    // Edges
    g.selectAll("line").data(edgeList).enter().append("line")
      .attr("x1", e => e.source.x + offsetX).attr("y1", e => e.source.y + offsetY)
      .attr("x2", e => e.target.x + offsetX).attr("y2", e => e.target.y + offsetY)
      .attr("stroke", e => e.hard ? "#2f6fec" : "#ccc")
      .attr("stroke-width", e => e.hard ? 2 : 1)
      .attr("stroke-dasharray", e => e.hard ? "" : "4,4");

    // Entity nodes
    g.selectAll("rect.entity").data(nodes.filter(n => n.type === "entity")).enter().append("rect")
      .attr("class", "entity")
      .attr("x", n => n.x + offsetX).attr("y", n => n.y + offsetY - 8)
      .attr("width", n => Math.min(160, (n.data.label?.length || 4) * 7 + 12))
      .attr("height", 16).attr("rx", 3)
      .attr("fill", "#fff")
      .attr("stroke", n => ENTITY_COLORS[n.data.etype] || "#2f6fec")
      .attr("stroke-width", n => 1 + (n.data.size || 0) * 0.5)
      .on("mouseenter", (ev, n) => setTooltip({ x: ev.pageX, y: ev.pageY, text: `${n.data.label} — ${n.data.refCount || 1} sessions` }))
      .on("mouseleave", () => setTooltip(null));

    g.selectAll("text.entity").data(nodes.filter(n => n.type === "entity")).enter().append("text")
      .attr("class", "entity")
      .attr("x", n => n.x + offsetX + 6).attr("y", n => n.y + offsetY + 3)
      .attr("font-size", 9).attr("fill", "#333")
      .text(n => n.data.label?.length > 22 ? n.data.label.slice(0, 22) + ".." : n.data.label);

    // Session nodes
    g.selectAll("circle.session").data(nodes.filter(n => n.type === "session")).enter().append("circle")
      .attr("class", "session")
      .attr("cx", n => n.x + offsetX).attr("cy", n => n.y + offsetY)
      .attr("r", 14).attr("fill", SESSION_COLOR).attr("stroke", "#fff").attr("stroke-width", 2)
      .on("mouseenter", (ev, n) => setTooltip({ x: ev.pageX, y: ev.pageY, text: n.data.label }))
      .on("mouseleave", () => setTooltip(null));

    g.selectAll("text.session").data(nodes.filter(n => n.type === "session")).enter().append("text")
      .attr("class", "session")
      .attr("x", n => n.x + offsetX + 18).attr("y", n => n.y + offsetY + 3)
      .attr("font-size", 10).attr("fill", "#555")
      .text(n => n.data.label?.length > 16 ? n.data.label.slice(0, 16) + ".." : n.data.label);

    // Drag — only move on actual drag, not on click
    const drag = d3.drag()
      .on("start", function() { d3.select(this).raise().attr("stroke-width", 3); })
      .on("drag", (ev, n) => {
        if (!ev.sourceEvent?.movementX && !ev.sourceEvent?.movementY) return; // skip click
        n.x += ev.dx; n.y += ev.dy;
        updatePositions();
      })
      .on("end", function() { d3.select(this).attr("stroke-width", null); });

    function updatePositions() {
      g.selectAll("line")
        .attr("x1", e => e.source.x + offsetX).attr("y1", e => e.source.y + offsetY)
        .attr("x2", e => e.target.x + offsetX).attr("y2", e => e.target.y + offsetY);
      g.selectAll("rect.entity")
        .attr("x", n => n.x + offsetX).attr("y", n => n.y + offsetY - 8);
      g.selectAll("text.entity")
        .attr("x", n => n.x + offsetX + 6).attr("y", n => n.y + offsetY + 3);
      g.selectAll("circle.session")
        .attr("cx", n => n.x + offsetX).attr("cy", n => n.y + offsetY);
      g.selectAll("text.session")
        .attr("x", n => n.x + offsetX + 18).attr("y", n => n.y + offsetY + 3);
    }

    g.selectAll("rect.entity, circle.session").call(drag);
    dragRef.current = { g, updatePositions };
  }, []);

  return (
    <div style={{ position: "relative", width: "100%", height: "100%", background: "#fafafa", overflow: "hidden" }}>
      <div style={{ position: "absolute", top: 12, left: 12, zIndex: 10 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={onClose} size="small">返回</Button>
      </div>
      <svg ref={svgRef} width="100%" height="100%" style={{ cursor: "grab" }} />
      {tooltip && (
        <div style={{ position: "fixed", left: tooltip.x + 10, top: tooltip.y + 10, zIndex: 100,
          background: "rgba(0,0,0,0.75)", color: "#fff", padding: "4px 10px", borderRadius: 4, fontSize: 12, pointerEvents: "none" }}>
          {tooltip.text}
        </div>
      )}
      <div style={{ position: "absolute", bottom: 12, left: 12, zIndex: 10, background: "rgba(255,255,255,0.9)", padding: "6px 10px", borderRadius: 6, fontSize: 11 }}>
        <Space size={12}>
          <span><span style={{display:"inline-block",width:10,height:10,borderRadius:"50%",background:SESSION_COLOR,marginRight:4}} /> Session</span>
          <span><span style={{display:"inline-block",width:10,height:10,borderRadius:4,background:"#2f6fec",marginRight:4}} /> Entity</span>
          <span style={{color:"#888"}}>— 连线</span>
        </Space>
      </div>
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

  const { sessions, alerts, graph_edges, entity_labels } = data;

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
          graphEdges={graph_edges}
          sessions={sessions}
          entityLabels={entity_labels}
          onClose={() => setGraphSession(null)}
        />
      </div>
    );
  }

  const fetchTentativeDetails = async () => {
    setTentativeLoading(true);
    try {
      const data = await api("/pmai/web/events");
      setTentativeDetails(data.events || []);
    } catch {
      setTentativeDetails([]);
    }
    setTentativeLoading(false);
  };

  const consumeTentativeAll = async () => {
    try {
      const data = await api("/pmai/web/events/consume", { method: "POST" });
      if (data.ok) {
        setTentativeDetails([]);
        // Refresh the page to update the alert count
        window.location.reload();
      }
    } catch {}
  };

  return (
    <div style={{ padding: "16px 24px", maxWidth: 960, margin: "0 auto" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start" }}>
        <div>
          <Title level={4} style={{ marginBottom: 4 }}>Activity</Title>
          <Text type="secondary">Agent 会话时间线 — 谁做了什么、关联了什么</Text>
        </div>
        <Button icon={<NodeIndexOutlined />} onClick={() => setGraphSession(true)}>关联图</Button>
      </div>

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
                <Popover
                  content={
                    <div style={{ maxWidth: 420, maxHeight: 300, overflow: "auto" }}>
                      {tentativeLoading ? (
                        <Spin size="small" />
                      ) : tentativeDetails ? (
                        tentativeDetails.length === 0 ? (
                          <Text type="secondary">暂无需确认的关联</Text>
                        ) : (
                          <List
                            size="small"
                            dataSource={tentativeDetails.slice(0, 10)}
                            renderItem={(e) => (
                              <List.Item style={{ padding: "4px 0", fontSize: 12 }}>
                                <Text type="secondary">{e.summary}</Text>
                              </List.Item>
                            )}
                          />
                        )
                      ) : (
                        <Text type="secondary">点击查看后加载</Text>
                      )}
                      {tentativeDetails && tentativeDetails.length > 0 && (
                        <Button size="small" type="link" danger style={{ marginTop: 8, padding: 0 }}
                          onClick={consumeTentativeAll}>
                          忽略全部 ({tentativeDetails.length})
                        </Button>
                      )}
                    </div>
                  }
                  title="待确认关联"
                  trigger="click"
                  onOpenChange={(open) => { if (open) fetchTentativeDetails(); }}
                >
                  <Button type="link" size="small" style={{ padding: 0 }}>🔗 {alerts.tentative_links} 个待确认关联</Button>
                </Popover>
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
                        <Tag color="blue" style={{ cursor: "pointer" }}
                          onClick={(e) => { e.stopPropagation(); setGraphSession(true); }}>
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
