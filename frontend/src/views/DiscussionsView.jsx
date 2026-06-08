import { useState, useEffect } from "react";
import { Button, Card, Checkbox, Input, Select, Space, Table, Tag, Typography } from "antd";
import { SearchOutlined, FullscreenOutlined, FullscreenExitOutlined } from "@ant-design/icons";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeRaw from "rehype-raw";
import { api } from "../utils/api";

const { Title } = Typography;

// DiffPanel — wraps diff body with file header and fullscreen toggle
function DiffPanel({ filePath, hunks, children }) {
  const [full, setFull] = useState(false);
  const hunkCount = hunks ? hunks.length : 0;
  if (!full) {
    return (
      <div style={{ border: "1px solid #e8e8e8", borderRadius: 6, overflow: "hidden" }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "4px 8px", borderBottom: "1px solid #e8e8e8", background: "#fafafa" }}>
          <span>
            <span style={{ fontSize: 12, fontFamily: "monospace", color: "#2f6fec", fontWeight: 500 }}>📄 {filePath}</span>
            {hunkCount > 1 && <Tag style={{ marginLeft: 8, fontSize: 10 }}>{hunkCount} chunks</Tag>}
          </span>
          <Button size="small" type="text" icon={<FullscreenOutlined />} onClick={() => setFull(true)} />
        </div>
        {children}
      </div>
    );
  }
  return (
    <div style={{ position: "fixed", inset: 0, zIndex: 9999, background: "#fff", display: "flex", flexDirection: "column" }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "6px 16px", borderBottom: "2px solid #e8e8e8", background: "#fafafa", flexShrink: 0 }}>
        <span>
          <span style={{ fontSize: 13, fontFamily: "monospace", color: "#2f6fec", fontWeight: 500 }}>📄 {filePath}</span>
          {hunkCount > 1 && <Tag style={{ marginLeft: 8, fontSize: 10 }}>{hunkCount} chunks</Tag>}
        </span>
        <Button size="small" type="primary" icon={<FullscreenExitOutlined />} onClick={() => setFull(false)}>退出全屏</Button>
      </div>
      <div style={{ flex: 1, overflow: "auto" }}>{children}</div>
    </div>
  );
}

function processData(data) {
  if (!data.length) return data;
  const result = data.map(d => ({ ...d }));
  for (const r of result) {
    if (r.metadata) {
      try { r._meta = JSON.parse(r.metadata); } catch (e) { r._meta = null; }
    }
  }
  const dates = result.map(r => (r.created_at || "").slice(0, 10));
  for (let i = 0; i < result.length; i++) {
    result[i]._key = result[i].id;
    if (i > 0 && dates[i] === dates[i - 1]) {
      result[i]._dateRowSpan = 0;
    } else {
      let count = 1;
      for (let j = i + 1; j < result.length && dates[j] === dates[i]; j++) count++;
      result[i]._dateRowSpan = count;
    }
  }
  return result;
}

// Single-column unified diff from structuredPatch hunks
function renderUnifiedHunks(hunks) {
  // Fixed width for both line number columns — enough for 4-digit numbers
  const numW = 40;
  const numStyle = {
    display: "inline-block", width: numW, minWidth: numW,
    textAlign: "right", paddingRight: 6,
    color: "#aaa", fontSize: 10, userSelect: "none",
    lineHeight: "20px", minHeight: 20, verticalAlign: "top",
  };
  const codeStyle = (type) => ({
    padding: "0 6px", fontSize: 11,
    fontFamily: "Consolas, 'Courier New', monospace",
    whiteSpace: "pre", lineHeight: "20px", minHeight: 20,
    background: type === "removed" ? "#fff1f0"
      : type === "added" ? "#f6ffed"
      : "transparent",
    color: type === "removed" ? "#cf1322"
      : type === "added" ? "#389e0d"
      : "#555",
  });
  const wrapperStyle = { overflowX: "auto" };

  return hunks.map((h, hi) => {
    let oldNum = h.oldStart;
    let newNum = h.newStart;
    const rows = [];
    for (const raw of h.lines) {
      const m = raw[0];
      if (m === "-") {
        rows.push({ old: oldNum++, _new: null, text: raw.slice(1), type: "removed" });
      } else if (m === "+") {
        rows.push({ old: null, _new: newNum++, text: raw.slice(1), type: "added" });
      } else {
        rows.push({ old: oldNum++, _new: newNum++, text: raw, type: "context" });
      }
    }

    return (
      <div key={hi} style={{ marginBottom: hi < hunks.length - 1 ? 8 : 0 }}>
        <div style={{ fontSize: 10, color: "#888", padding: "2px 8px", background: "#f0f5ff", fontFamily: "monospace" }}>
          @@ -{h.oldStart},{h.oldLines} +{h.newStart},{h.newLines} @@
        </div>
        <div style={wrapperStyle}>
          {rows.map((r, ri) => (
            <div key={ri} style={{ display: "flex", borderBottom: "1px solid #f5f5f5", minHeight: 20, background: r.type === "removed" ? "#fff1f0" : r.type === "added" ? "#f6ffed" : "transparent" }}>
              <span style={numStyle}>{r.old != null ? r.old : ""}</span>
              <span style={numStyle}>{r._new != null ? r._new : ""}</span>
              <span style={codeStyle(r.type)}>{r.text}</span>
            </div>
          ))}
        </div>
      </div>
    );
  });
}

// LCS-based fallback for old records that have old_string/new_string but no hunks
function renderLCSFallback(oldStr, newStr) {
  const oldLines = oldStr.split("\n");
  const newLines = newStr.split("\n");
  const m = oldLines.length, n = newLines.length;
  const dp = Array.from({ length: m + 1 }, () => new Int32Array(n + 1));
  for (let i = 1; i <= m; i++)
    for (let j = 1; j <= n; j++)
      dp[i][j] = oldLines[i - 1] === newLines[j - 1]
        ? dp[i - 1][j - 1] + 1
        : Math.max(dp[i - 1][j], dp[i][j - 1]);

  const rows = [];
  let i = m, j = n;
  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && oldLines[i - 1] === newLines[j - 1]) {
      rows.unshift({ old: i, _new: j, text: oldLines[i - 1], type: "context" }); i--; j--;
    } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
      rows.unshift({ old: null, _new: j, text: newLines[j - 1], type: "added" }); j--;
    } else {
      rows.unshift({ old: i, _new: null, text: oldLines[i - 1], type: "removed" }); i--;
    }
  }

  const numW = 40;
  const numStyle = {
    display: "inline-block", width: numW, minWidth: numW,
    textAlign: "right", paddingRight: 6,
    color: "#aaa", fontSize: 10, userSelect: "none",
    lineHeight: "20px", minHeight: 20,
  };
  const codeStyle = (t) => ({
    padding: "0 6px", fontSize: 11,
    fontFamily: "Consolas, 'Courier New', monospace",
    whiteSpace: "pre", lineHeight: "20px", minHeight: 20,
    background: t === "removed" ? "#fff1f0" : t === "added" ? "#f6ffed" : "transparent",
    color: t === "removed" ? "#cf1322" : t === "added" ? "#389e0d" : "#555",
  });
  return (
    <div style={{ maxHeight: 420, overflow: "auto" }}>
      {rows.map((r, ri) => (
        <div key={ri} style={{ display: "flex", borderBottom: "1px solid #f5f5f5", minHeight: 20, background: r.type === "removed" ? "#fff1f0" : r.type === "added" ? "#f6ffed" : "transparent" }}>
          <span style={numStyle}>{r.old != null ? r.old : ""}</span>
          <span style={numStyle}>{r._new != null ? r._new : ""}</span>
          <span style={codeStyle(r.type)}>{r.text}</span>
        </div>
      ))}
    </div>
  );
}

function renderContent(c, role, metadata) {
  const text = (c || "").replace(/\\n/g, "\n").trim();
  const isTool = /^[🔧✏️👁🔍📂🌐🔎🛠]/.test(text);
  const isEdit = isTool && /^📝/.test(text);
  const md = !isTool && role === "assistant";

  // New file
  const newFileMeta = (metadata && metadata.type === "new_file" && metadata.file_path)
    ? metadata : null;
  // Edit/Write with hunks (new format)
  const hunksMeta = (metadata && metadata.type === "edit" && metadata.hunks && metadata.hunks.length > 0)
    ? metadata : null;
  // Old format: old_string/new_string without hunks
  const legacyMeta = (!hunksMeta && metadata && metadata.type === "edit"
    && metadata.old_string !== undefined && metadata.new_string !== undefined)
    ? metadata : null;

  if (newFileMeta) {
    return (
      <div style={{ padding: "3px 8px", display: "flex", alignItems: "center", gap: 6 }}>
        <span style={{ fontSize: 15 }}>📄</span>
        <span style={{ fontSize: 12, fontFamily: "monospace", color: "#2f6fec", fontWeight: 500 }}>
          {newFileMeta.file_path}
        </span>
        <Tag color="green" style={{ fontSize: 10, margin: 0 }}>新建</Tag>
      </div>
    );
  }

  if (hunksMeta) {
    return (
      <DiffPanel filePath={hunksMeta.file_path} hunks={hunksMeta.hunks}>
        {renderUnifiedHunks(hunksMeta.hunks)}
      </DiffPanel>
    );
  }

  if (legacyMeta) {
    return (
      <DiffPanel filePath={legacyMeta.file_path}>
        {renderLCSFallback(legacyMeta.old_string, legacyMeta.new_string)}
      </DiffPanel>
    );
  }

  // Fallback: plain text diff (no metadata at all)
  function renderPlainDiff(content) {
    const lines = content.split("\n");
    return lines.map((line, i) => {
      let style = {};
      if (line.startsWith("- ") || line.startsWith("-")) style = { color: "#cf1322", background: "#fff1f0" };
      else if (line.startsWith("+ ") || line.startsWith("+")) style = { color: "#389e0d", background: "#f6ffed" };
      else if (i === 0) style = { color: "#2f6fec", fontWeight: 500 };
      else style = { color: "#666" };
      return <div key={i} style={{ ...style, padding: "1px 4px", fontSize: 12, fontFamily: "monospace" }}>{line || " "}</div>;
    });
  }

  if (isEdit && text.includes("\n")) {
    return <div style={{ padding: "4px 0" }}>{renderPlainDiff(text)}</div>;
  }

  if (isEdit) {
    return (
      <div style={{ whiteSpace: "pre-wrap", wordBreak: "break-word", fontSize: 12, fontFamily: "monospace", color: "#888", padding: "6px 8px" }}>
        {text}
      </div>
    );
  }

  return (
    <div style={{
      whiteSpace: md ? "normal" : "pre-wrap", wordBreak: "break-word",
      fontSize: isTool ? 12 : 14, fontFamily: isTool ? "monospace" : undefined,
      color: isTool ? "#888" : "#333", padding: "6px 8px",
      background: role === "user" ? "#e6f7ff" : "transparent",
      borderRadius: role === "user" ? 4 : 0,
    }}>
      {isTool || role === "user" ? text : (
        <div className="markdown-body" style={{ fontSize: 14 }}>
          <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]}
            components={{
              table: ({node, ...p}) => <table style={{borderCollapse:"collapse",margin:"0.2em 0",fontSize:13,width:"auto"}} {...p} />,
              th: ({node, ...p}) => <th style={{border:"1px solid #ddd",padding:"4px 8px",textAlign:"left"}} {...p} />,
              td: ({node, ...p}) => <td style={{border:"1px solid #ddd",padding:"3px 8px",textAlign:"left"}} {...p} />,
            }}
          >{text}</ReactMarkdown>
        </div>
      )}
    </div>
  );
}

const mdStyles = `
.markdown-body p { margin: 0.2em 0 !important; }
.markdown-body p:first-child { margin-top: 0 !important; }
.markdown-body p:last-child { margin-bottom: 0 !important; }
.markdown-body table { margin: 0.2em 0 !important; font-size: 13px; width: auto !important; border-collapse: collapse !important; }
.markdown-body thead th { padding: 4px 8px !important; text-align: left !important; border: 1px solid #ddd !important; }
.markdown-body tbody td { padding: 3px 8px !important; text-align: left !important; border: 1px solid #ddd !important; }
.markdown-body h1,.markdown-body h2,.markdown-body h3,.markdown-body h4 { margin: 0.3em 0 !important; }
.markdown-body pre { margin: 0.3em 0 !important; }
.markdown-body blockquote { margin: 0.3em 0 !important; padding: 0.3em 0.8em !important; }
.markdown-body ul,.markdown-body ol { margin: 0.2em 0 !important; padding-left: 1.5em; }
`;

export default function DiscussionsView() {
  const [loading, setLoading] = useState(false);
  const [discussions, setDiscussions] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [query, setQuery] = useState("");
  const [source, setSource] = useState("");
  const [sources, setSources] = useState([]);
  const [showUser, setShowUser] = useState(true);
  const [showAgent, setShowAgent] = useState(true);
  const [showTool, setShowTool] = useState(true);

  function isUserMsg(r) { return r.role === "user"; }
  function isToolMsg(r) { return /^[🔧📝👁🔍🆕🛠📡]/.test(r.content || ""); }
  function isAgentMsg(r) { return r.role === "assistant" && !isToolMsg(r); }

  function filterDiscussions(list) {
    if (showUser && showAgent && showTool) return list;
    return list.filter(r => {
      if (isUserMsg(r)) return showUser;
      if (isToolMsg(r)) return showTool;
      if (isAgentMsg(r)) return showAgent;
      return true;
    });
  }

  async function loadSources() {
    const data = await api("/pmai/discussions/sources");
    if (data.sources) setSources(data.sources);
  }

  async function load(p = 1, srcOverride) {
    setLoading(true);
    const params = new URLSearchParams();
    params.set("page", p);
    if (query) params.set("q", query);
    const finalSource = srcOverride !== undefined ? srcOverride : source;
    if (finalSource) params.set("source", finalSource);
    const data = await api(`/pmai/discussions?${params}`);
    setDiscussions(processData(data.discussions || []));
    setTotal(data.total || 0);
    setLoading(false);
  }

  useEffect(() => { load(1); loadSources(); }, []);

  return (
    <div>
      <style>{mdStyles}</style>
      <Space style={{ marginBottom: 16 }}>
        <Title level={4} style={{ margin: 0 }}>💬 讨论日志</Title>
      </Space>
      <Card size="small" style={{ marginBottom: 16 }}>
        <Space wrap>
          <Input prefix={<SearchOutlined />} placeholder="搜索..." value={query}
            onChange={(e) => setQuery(e.target.value)} onPressEnter={() => load(1)} style={{ width: 280 }} />
          <Select allowClear placeholder="来源" value={source || undefined}
            onChange={(v) => { setSource(v || ""); load(1, v || ""); }} style={{ width: 140 }}
            options={sources.map(s => ({ value: s, label: s }))} />
          <Button type="primary" onClick={() => load(1)}>搜索</Button>
          <Checkbox checked={showUser} onChange={(e) => setShowUser(e.target.checked)}>👤 用户</Checkbox>
          <Checkbox checked={showAgent} onChange={(e) => setShowAgent(e.target.checked)}>🤖 Agent</Checkbox>
          <Checkbox checked={showTool} onChange={(e) => setShowTool(e.target.checked)}>🔧 工具</Checkbox>
        </Space>
      </Card>
      {(() => {
        const filtered = filterDiscussions(discussions);
        return (
          <Table
            dataSource={filtered} columns={[
              { title: "日期", dataIndex: "created_at", key: "date", width: 100,
                render: (t, row) => ({ children: row._dateRowSpan > 0 ? (t || "").slice(0, 10) : "", props: { rowSpan: row._dateRowSpan } }) },
              { title: "时间", dataIndex: "created_at", key: "time", width: 70,
                render: (t) => <span style={{ fontSize: 12, color: "#999" }}>{(t || "").slice(11, 19)}</span> },
              { title: "", dataIndex: "role", key: "role", width: 45,
                render: (r, row) => /^[🔧✏️👁🔍📂🌐🔎🛠]/.test(row.content || "")
                  ? <Tag color="default" style={{ fontSize: 11 }}>🔧</Tag>
                  : <Tag color={r === "user" ? "blue" : "green"} style={{ fontSize: 11 }}>{r === "user" ? "👤" : "🤖"}</Tag> },
              { title: "内容", dataIndex: "content", key: "content", render: (c, row) => renderContent(c, row.role, row._meta) },
            ]}
            rowKey="_key" loading={loading} size="small"
            pagination={{ current: page, total, pageSize: 20, onChange: (p) => { setPage(p); load(p); }, showTotal: (t) => `共 ${t} 条${filtered.length !== t ? ' (筛选后 ' + filtered.length + ' 条)' : ''}` }} />
        );
      })()}
    </div>
  );
}
