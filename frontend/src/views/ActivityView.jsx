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
  const sessionLabel = {};
  if (sessions) {
    for (const s of sessions) {
      // Label priority: goal (L2) > intent (B1) > first_prompt (raw) > session_id (fallback)
      let label = s.goal;
      if (!label || label.startsWith("{")) {
        label = s.intent && s.intent !== "unknown" ? s.intent : null;
      }
      sessionLabel[s.session_id] = label || s.first_prompt?.slice(0, 40) || s.session_id?.slice(0, 8);
    }
  }

  // ── Build deduped, typed graph ──
  const nodeMap = {};
  const edgeList = [];
  const seenEdges = new Set();

  for (const [s, t, rel] of graphEdges) {
    // File edges: keep but mark for visual distinction (don't skip)
    const hasFile = s.startsWith("file:") || t.startsWith("file:");
    const key = s + "|" + t;
    if (seenEdges.has(key)) continue;
    seenEdges.add(key);

    // Resolve source node type (session, entity, or file)
    // TODO: pass source_type explicitly from backend edge data instead of heuristic detection.
    //       Session IDs are UUID/hex today (no ':'), but this breaks if format changes.
    if (!nodeMap[s]) {
      const isSourceSession = !s.includes(":");
      nodeMap[s] = {
        id: s,
        type: s.startsWith("file:") ? "file" : (isSourceSession ? "session" : "entity"),
        data: { label: sessionLabel[s] || el[s] || s.slice(0, 8) }
      };
    }
    // Resolve target node type and label
    if (!nodeMap[t]) {
      const isTargetFile = t.startsWith("file:");
      const cp = t.indexOf(":");
      const ep = cp >= 0 ? t.substring(cp + 1) : t;
      const dp = ep.indexOf("-");
      const et = dp >= 0 ? ep.substring(0, dp) : "entity";
      // Try entityLabels with prefix, then without, then raw
      let title = el[t] || el[ep] || ep;
      if (!el[t] && !t.includes(":")) {
        const prefixed = et + ":" + t;
        title = el[prefixed] || title;
      }
      nodeMap[t] = {
        id: t, type: isTargetFile ? "file" : "entity",
        data: { label: title, etype: et }
      };
    }

    // Edge type for visual distinction
    const isPipeline = rel.startsWith("file_touch:") || rel.startsWith("relates_to:") ||
      /^(fixes|implements|blocked_by|depends_on)$/.test(rel);
    const isMCP = rel.startsWith("refers_to:");
    const edgeType = hasFile ? "file" : isPipeline ? "pipeline" : isMCP ? "mcp" : "fallback";
    const isHard = isMCP || isPipeline;

    edgeList.push({
      source: nodeMap[s],
      target: nodeMap[t],
      hard: isHard,
      edgeType,
      relation: rel,
    });
  }

  // ── Limit entity nodes: keep top 20 by edge count ──
  const entityDegree = {};
  for (const e of edgeList) {
    entityDegree[e.target.id] = (entityDegree[e.target.id] || 0) + 1;
    // Count source entities too (for non-session sources like entity→entity edges)
    if (e.source.type === "entity") {
      entityDegree[e.source.id] = (entityDegree[e.source.id] || 0) + 1;
    }
  }
  const sortedEntities = Object.entries(entityDegree)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 25)
    .map(([id]) => id);
  const keepEntity = new Set(sortedEntities);

  // Filter edges to kept entities
  const filteredEdges = edgeList.filter(e =>
    keepEntity.has(e.target.id) || (e.source.type === "entity" && keepEntity.has(e.source.id))
  );
  const keepSession = new Set();
  for (const e of filteredEdges) {
    if (e.source.type === "session") keepSession.add(e.source.id);
    if (e.target.type === "session") keepSession.add(e.target.id);
  }
  const nodes = Object.values(nodeMap).filter(n =>
    n.type === "session" ? keepSession.has(n.id) : keepEntity.has(n.id)
  );

  console.log("Graph: nodes", nodes.length, "edges", filteredEdges.length,
    "(filtered from", edgeList.length, "edges,", Object.keys(nodeMap).length, "nodes)");

  // ── Simulation ──
  const simNodes = nodes.map(n => ({ id: n.id, ref: n }));
  const nodeById = {};
  for (const sn of simNodes) nodeById[sn.id] = sn;
  const simLinks = filteredEdges.map(e => ({
    source: nodeById[e.source.id],
    target: nodeById[e.target.id]
  }));

  const sim = forceSimulation(simNodes)
    .force("charge", forceManyBody().strength(-400))
    .force("link", forceLink(simLinks).distance(180).strength(0.15))
    .force("center", forceCenter(0, 0))
    .force("collide", forceCollide(60))
    .stop();
  for (let i = 0; i < 300; i++) sim.tick();

  for (const sn of simNodes) { sn.ref.x = sn.x; sn.ref.y = sn.y; }

  // ── Compute SVG viewport ──
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const n of nodes) {
    if (n.x < minX) minX = n.x; if (n.y < minY) minY = n.y;
    if (n.x > maxX) maxX = n.x; if (n.y > maxY) maxY = n.y;
  }
  const pad = 80, w = Math.max(900, maxX - minX + pad * 2), h = Math.max(650, maxY - minY + pad * 2);
  const ox = -minX + pad, oy = -minY + pad;

  const svgRef = useRef(null);
  const [tooltip, setTooltip] = useState(null);

  useEffect(() => {
    const svg = d3.select(svgRef.current);
    svg.selectAll("*").remove();

    // ── Defs: arrow marker ──
    const defs = svg.append("defs");
    defs.append("marker")
      .attr("id", "arrow-hard")
      .attr("viewBox", "0 0 10 10").attr("refX", 24).attr("refY", 5)
      .attr("markerWidth", 8).attr("markerHeight", 8).attr("orient", "auto")
      .append("path").attr("d", "M0,0 L10,5 L0,10 Z").attr("fill", "#2f6fec").attr("opacity", 0.6);
    defs.append("marker")
      .attr("id", "arrow-soft")
      .attr("viewBox", "0 0 10 10").attr("refX", 24).attr("refY", 5)
      .attr("markerWidth", 6).attr("markerHeight", 6).attr("orient", "auto")
      .append("path").attr("d", "M0,0 L10,5 L0,10 Z").attr("fill", "#aaa").attr("opacity", 0.4);

    const g = svg.append("g");
    const zoom = d3.zoom().scaleExtent([0.3, 4]).on("zoom", (ev) => g.attr("transform", ev.transform));
    svg.call(zoom);

    // ── Edges: curved paths ──
    g.selectAll("path.edge").data(filteredEdges).enter().append("path")
      .attr("class", "edge")
      .attr("d", e => {
        const sx = e.source.x + ox, sy = e.source.y + oy;
        const tx = e.target.x + ox, ty = e.target.y + oy;
        const mx = (sx + tx) / 2, my = (sy + ty) / 2 - 30;
        return `M${sx},${sy} Q${mx},${my} ${tx},${ty}`;
      })
      .attr("fill", "none")
      .attr("stroke", e => {
        if (e.edgeType === "pipeline") return "#52c41a";  // green: pipeline computed
        if (e.edgeType === "mcp") return "#2f6fec";       // blue: Agent MCP refs
        if (e.edgeType === "file") return "#8c8c8c";      // gray: file touched
        return "#bbb";                                      // light gray: fallback
      })
      .attr("stroke-width", e => e.edgeType === "file" ? 0.8 : e.hard ? 2 : 1.2)
      .attr("stroke-dasharray", e => {
        if (e.edgeType === "file") return "4,4";
        if (e.edgeType === "fallback") return "5,5";
        return ""; // solid for pipeline + mcp
      })
      .attr("opacity", e => e.edgeType === "file" ? 0.25 : e.hard ? 0.5 : 0.35)
      .attr("marker-end", e => e.edgeType === "fallback" || e.edgeType === "file" ? null : "url(#arrow-hard)");

    // ── Entity nodes (rectangles) ──
    const ents = nodes.filter(n => n.type === "entity");
    g.selectAll("rect.entity").data(ents).enter().append("rect")
      .attr("class", "entity")
      .attr("x", n => n.x + ox).attr("y", n => n.y + oy - 10)
      .attr("width", n => Math.min(180, (n.data.label?.length || 4) * 7 + 16))
      .attr("height", 22).attr("rx", 4)
      .attr("fill", "#fff")
      .attr("stroke", n => ENTITY_COLORS[n.data.etype] || "#2f6fec")
      .attr("stroke-width", 1.5)
      .on("mouseenter", (ev, n) => setTooltip({ x: ev.pageX, y: ev.pageY, text: n.data.label }))
      .on("mouseleave", () => setTooltip(null));

    g.selectAll("text.entity").data(ents).enter().append("text")
      .attr("class", "entity")
      .attr("x", n => n.x + ox + 8).attr("y", n => n.y + oy + 5)
      .attr("font-size", 9).attr("fill", "#333").attr("pointer-events", "none")
      .text(n => n.data.label?.length > 26 ? n.data.label.slice(0, 26) + ".." : n.data.label);

    // ── Session nodes (circles) ──
    const sess = nodes.filter(n => n.type === "session");
    g.selectAll("circle.session").data(sess).enter().append("circle")
      .attr("class", "session")
      .attr("cx", n => n.x + ox).attr("cy", n => n.y + oy)
      .attr("r", 16).attr("fill", SESSION_COLOR).attr("stroke", "#fff").attr("stroke-width", 2.5)
      .on("mouseenter", (ev, n) => setTooltip({ x: ev.pageX, y: ev.pageY, text: n.data.label }))
      .on("mouseleave", () => setTooltip(null));

    g.selectAll("text.session").data(sess).enter().append("text")
      .attr("class", "session")
      .attr("x", n => n.x + ox + 22).attr("y", n => n.y + oy + 4)
      .attr("font-size", 10).attr("fill", "#555").attr("pointer-events", "none")
      .text(n => n.data.label?.length > 18 ? n.data.label.slice(0, 18) + ".." : n.data.label);

    // ── File nodes (small gray dots) ──
    const files = nodes.filter(n => n.type === "file");
    g.selectAll("circle.file").data(files).enter().append("circle")
      .attr("class", "file")
      .attr("cx", n => n.x + ox).attr("cy", n => n.y + oy)
      .attr("r", 5).attr("fill", FILE_COLOR).attr("stroke", "#fff").attr("stroke-width", 1)
      .on("mouseenter", (ev, n) => setTooltip({ x: ev.pageX, y: ev.pageY, text: "file: " + n.data.label }))
      .on("mouseleave", () => setTooltip(null));

    g.selectAll("text.file").data(files).enter().append("text")
      .attr("class", "file")
      .attr("x", n => n.x + ox + 8).attr("y", n => n.y + oy + 4)
      .attr("font-size", 8).attr("fill", "#999").attr("pointer-events", "none")
      .text(n => n.data.label?.length > 22 ? n.data.label.slice(0, 22) + ".." : n.data.label);

    // ── Drag ──
    const drag = d3.drag()
      .on("start", function() { d3.select(this).raise().attr("stroke-width", 3); })
      .on("drag", (ev, n) => {
        if (!ev.sourceEvent?.movementX && !ev.sourceEvent?.movementY) return;
        n.x += ev.dx; n.y += ev.dy;
        updatePositions();
      })
      .on("end", function() { d3.select(this).attr("stroke-width", null); });

    function updatePositions() {
      g.selectAll("path.edge").attr("d", e => {
        const sx = e.source.x + ox, sy = e.source.y + oy;
        const tx = e.target.x + ox, ty = e.target.y + oy;
        const mx = (sx + tx) / 2, my = (sy + ty) / 2 - 30;
        return `M${sx},${sy} Q${mx},${my} ${tx},${ty}`;
      });
      g.selectAll("rect.entity")
        .attr("x", n => n.x + ox).attr("y", n => n.y + oy - 10);
      g.selectAll("text.entity")
        .attr("x", n => n.x + ox + 8).attr("y", n => n.y + oy + 5);
      g.selectAll("circle.session")
        .attr("cx", n => n.x + ox).attr("cy", n => n.y + oy);
      g.selectAll("text.session")
        .attr("x", n => n.x + ox + 22).attr("y", n => n.y + oy + 4);
      g.selectAll("circle.file")
        .attr("cx", n => n.x + ox).attr("cy", n => n.y + oy);
      g.selectAll("text.file")
        .attr("x", n => n.x + ox + 8).attr("y", n => n.y + oy + 4);
    }

    g.selectAll("rect.entity, circle.session, circle.file").call(drag);
  }, []);

  return (
    <div style={{ position: "relative", width: "100%", height: "100%", background: "#f8f9fa", overflow: "hidden" }}>
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
      <div style={{ position: "absolute", bottom: 12, left: 12, zIndex: 10, background: "rgba(255,255,255,0.9)", padding: "6px 12px", borderRadius: 6, fontSize: 11 }}>
        <Space size={14}>
          <span><span style={{display:"inline-block",width:10,height:10,borderRadius:"50%",background:SESSION_COLOR,marginRight:4}} /> Session</span>
          <span><span style={{display:"inline-block",width:10,height:10,borderRadius:3,background:"#2f6fec",marginRight:4}} /> Entity</span>
          <span><span style={{display:"inline-block",width:6,height:6,borderRadius:"50%",background:FILE_COLOR,marginRight:4}} /> File</span>
          <span style={{color:"#52c41a",fontSize:10}}>━ pipeline</span>
          <span style={{color:"#8c8c8c",fontSize:10}}>┅ file</span>
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
  const [tentativeLoading, setTentativeLoading] = useState(false);
  const [tentativeDetails, setTentativeDetails] = useState(null);

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
