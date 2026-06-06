import { useState, useEffect } from "react";
import { Button, Card, Input, Select, Space, Table, Tag, Typography } from "antd";
import { ReloadOutlined, SearchOutlined } from "@ant-design/icons";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeRaw from "rehype-raw";
import { api } from "../utils/api";

const { Title } = Typography;

function processData(data) {
  if (!data.length) return data;
  const result = data.map(d => ({ ...d }));
  // Compute rowSpan for date column (same date = merged)
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

function renderContent(c, role) {
  const text = (c || "").replace(/\\n/g, "\n").trim();
  const isTool = /^[🔧✏️👁🔍📂🌐🔎🛠]/.test(text);
  const md = !isTool && role === "assistant";
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

  async function load(p = 1) {
    setLoading(true);
    const params = new URLSearchParams();
    params.set("page", p);
    if (query) params.set("q", query);
    if (source) params.set("source", source);
    const data = await api(`/pmai/discussions?${params}`);
    console.log("[discussions] loaded:", (data.discussions || []).length, "total:", data.total);
    (data.discussions || []).slice(0, 3).forEach(d => {
      console.log("[discussions]", d.role, d.content?.slice(0, 60), "hash:", (d.content||"").indexOf("\\n"), "nl:", (d.content||"").indexOf("\n"));
    });
    setDiscussions(processData(data.discussions || []));
    setTotal(data.total || 0);
    const srcs = new Set();
    (data.discussions || []).forEach(d => { if (d.source) srcs.add(d.source); });
    setSources(Array.from(srcs));
    setLoading(false);
  }

  useEffect(() => { load(1); }, []);

  return (
    <div>
      <style>{mdStyles}</style>
      <Space style={{ marginBottom: 16 }}>
        <Title level={4} style={{ margin: 0 }}>💬 讨论日志</Title>
        <Button icon={<ReloadOutlined />} onClick={() => load(page)} loading={loading}>刷新</Button>
      </Space>
      <Card size="small" style={{ marginBottom: 16 }}>
        <Space>
          <Input prefix={<SearchOutlined />} placeholder="搜索..." value={query}
            onChange={(e) => setQuery(e.target.value)} onPressEnter={() => load(1)} style={{ width: 280 }} />
          <Select allowClear placeholder="来源" value={source || undefined}
            onChange={(v) => { setSource(v || ""); }} style={{ width: 140 }}
            options={sources.map(s => ({ value: s, label: s }))} />
          <Button type="primary" onClick={() => load(1)}>搜索</Button>
        </Space>
      </Card>
      <Table
        dataSource={discussions} columns={[
          { title: "日期", dataIndex: "created_at", key: "date", width: 100,
            render: (t, row) => ({ children: row._dateRowSpan > 0 ? (t || "").slice(0, 10) : "", props: { rowSpan: row._dateRowSpan } }) },
          { title: "时间", dataIndex: "created_at", key: "time", width: 70,
            render: (t) => <span style={{ fontSize: 12, color: "#999" }}>{(t || "").slice(11, 19)}</span> },
          { title: "", dataIndex: "role", key: "role", width: 45,
            render: (r, row) => /^[🔧✏️👁🔍📂🌐🔎🛠]/.test(row.content || "")
              ? <Tag color="default" style={{ fontSize: 11 }}>🔧</Tag>
              : <Tag color={r === "user" ? "blue" : "green"} style={{ fontSize: 11 }}>{r === "user" ? "👤" : "🤖"}</Tag> },
          { title: "内容", dataIndex: "content", key: "content", render: (c, row) => renderContent(c, row.role) },
        ]}
        rowKey="_key" loading={loading} size="small"
        pagination={{ current: page, total, pageSize: 20, onChange: (p) => { setPage(p); load(p); }, showTotal: (t) => `共 ${t} 条` }} />
    </div>
  );
}
