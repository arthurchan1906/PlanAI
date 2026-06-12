import { useState, useEffect, useRef, useCallback } from "react";
import {
  Layout,
  Input,
  Button,
  Card,
  Spin,
  Typography,
  Space,
  Tag,
  Select,
  Empty,
  message,
  Tabs,
  Badge,
} from "antd";
import { SendOutlined, RobotOutlined, UserOutlined, ToolOutlined, PlusOutlined, BugOutlined } from "@ant-design/icons";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import "highlight.js/styles/github.css";
import { chatSend, chatGetSessions, chatGetSession } from "../utils/api";

const { TextArea } = Input;
const { Text, Title } = Typography;

// Tool call card in chat
function ToolCallCard({ tool }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div style={{ margin: "8px 0" }}>
      <Tag color="geekblue" style={{ cursor: "pointer", fontSize: 12 }} onClick={() => setExpanded(!expanded)}>
        <ToolOutlined /> {tool.name}
        {expanded ? " ▲" : " ▼"}
      </Tag>
      {expanded && (
        <Card size="small" style={{ marginTop: 4, background: "#f0f5ff" }}>
          <Text type="secondary" style={{ fontSize: 11 }}>参数:</Text>
          <pre style={{ fontSize: 11, margin: "2px 0", whiteSpace: "pre-wrap" }}>
            {JSON.stringify(tool.args, null, 1)}
          </pre>
        </Card>
      )}
    </div>
  );
}

// Chat message bubble
function MessageBubble({ evt }) {
  if (evt.role === "user") {
    return (
      <div style={{ marginBottom: 16, display: "flex", justifyContent: "flex-end" }}>
        <Card size="small" style={{ maxWidth: "75%", background: "#2f6fec", borderColor: "#2f6fec" }} bodyStyle={{ padding: "8px 14px" }}>
          <Text style={{ color: "#fff", whiteSpace: "pre-wrap" }}>{evt.content}</Text>
        </Card>
        <UserOutlined style={{ marginLeft: 8, marginTop: 8, color: "#2f6fec" }} />
      </div>
    );
  }
  if (evt.role === "assistant") {
    return (
      <div style={{ marginBottom: 16, display: "flex", justifyContent: "flex-start" }}>
        <RobotOutlined style={{ marginRight: 8, marginTop: 8, color: "#52c41a" }} />
        <div style={{ maxWidth: "75%" }}>
          {evt.content && (
            <Card size="small" bodyStyle={{ padding: "8px 14px" }}>
              <div className="markdown-body" style={{ fontSize: 14 }}>
                <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]}>
                  {evt.content}
                </ReactMarkdown>
              </div>
            </Card>
          )}
          {evt.tool_calls?.map((tc, i) => (
            <ToolCallCard key={tc.id || i} tool={tc} />
          ))}
        </div>
      </div>
    );
  }
  if (evt.role === "tool") {
    return (
      <div style={{ marginBottom: 8, marginLeft: 40 }}>
        <Tag color="blue" style={{ fontSize: 11 }}><ToolOutlined /> {evt.tool_name}</Tag>
        <Text type="secondary" style={{ fontSize: 11, marginLeft: 4 }}>
          {evt.tool_result?.slice?.(0, 80) || ""}
        </Text>
      </div>
    );
  }
  return null;
}

// Trace viewer — shows raw LLM request/response for one turn
function TraceTurnCard({ trace, turnIdx }) {
  const [expanded, setExpanded] = useState(turnIdx === 0);
  let reqObj, respObj;
  try { reqObj = JSON.parse(trace.request); } catch { reqObj = trace.request; }
  try { respObj = JSON.parse(trace.response); } catch { respObj = trace.response; }

  // Count tools in response
  const toolCount = respObj?.tool_calls?.length || 0;

  return (
    <Card
      size="small"
      style={{ marginBottom: 8 }}
      title={
        <span style={{ cursor: "pointer" }} onClick={() => setExpanded(!expanded)}>
          <Badge count={trace.turn} style={{ backgroundColor: "#2f6fec", marginRight: 8 }} />
          Turn {trace.turn}
          {toolCount > 0 && <Tag color="geekblue" style={{ marginLeft: 8 }}>{toolCount} tool calls</Tag>}
        </span>
      }
      extra={
        <Button type="link" size="small" onClick={() => setExpanded(!expanded)}>
          {expanded ? "收起" : "展开"}
        </Button>
      }
    >
      {expanded && (
        <Tabs
          size="small"
          items={[
            {
              key: "request",
              label: `请求 (${Array.isArray(reqObj) ? reqObj.length : 0} 条消息)`,
              children: (
                <pre style={{ fontSize: 12, maxHeight: 400, overflow: "auto", whiteSpace: "pre-wrap", background: "#fafafa", padding: 8, borderRadius: 4 }}>
                  {JSON.stringify(reqObj, null, 2)}
                </pre>
              ),
            },
            {
              key: "response",
              label: "响应",
              children: (
                <pre style={{ fontSize: 12, maxHeight: 400, overflow: "auto", whiteSpace: "pre-wrap", background: "#fafafa", padding: 8, borderRadius: 4 }}>
                  {JSON.stringify(respObj, null, 2)}
                </pre>
              ),
            },
          ]}
        />
      )}
    </Card>
  );
}

export default function ChatView() {
  const [sessions, setSessions] = useState([]);
  const [currentSessionId, setCurrentSessionId] = useState(null);
  const [events, setEvents] = useState([]);
  const [traces, setTraces] = useState([]);
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(false);
  const [showTraces, setShowTraces] = useState(true);
  const messagesEndRef = useRef(null);

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, []);

  useEffect(() => {
    chatGetSessions().then((data) => {
      const list = data.sessions || [];
      setSessions(list);
      if (list.length > 0 && !currentSessionId) handleSelectSession(list[0].id);
    }).catch(() => {});
  }, []);

  useEffect(() => { scrollToBottom(); }, [events]);

  function handleSelectSession(id) {
    setCurrentSessionId(id);
    chatGetSession(id).then((data) => {
      setEvents(data.events || []);
      setTraces(data.traces || []);
    }).catch(() => { setEvents([]); setTraces([]); });
  }

  function handleNewSession() {
    setCurrentSessionId(null);
    setEvents([]);
    setTraces([]);
    setInput("");
  }

  async function handleSend() {
    const text = input.trim();
    if (!text) return;
    setInput("");
    setLoading(true);
    setEvents((prev) => [...prev, { role: "user", content: text }]);
    try {
      const data = await chatSend(text, currentSessionId);
      if (data.session_id && data.session_id !== currentSessionId) {
        setCurrentSessionId(data.session_id);
        chatGetSessions().then((d) => setSessions(d.sessions || [])).catch(() => {});
      }
      setEvents(data.events || []);
      setTraces(data.traces || []);
    } catch (err) {
      message.error(`发送失败: ${err.message}`);
    } finally {
      setLoading(false);
    }
  }

  function handleKeyDown(e) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }

  const sidebarW = showTraces ? "50%" : 0;

  return (
    <Layout style={{ height: "100%", background: "#fff" }}>
      {/* Top bar: session selector */}
      <div style={{ padding: "12px 16px", borderBottom: "1px solid #f0f0f0", display: "flex", alignItems: "center", gap: 12 }}>
        <Select
          style={{ flex: 1 }}
          placeholder="选择会话..."
          value={currentSessionId}
          onChange={handleSelectSession}
          options={sessions.map((s) => ({ value: s.id, label: `${s.id} (${s.events} 条消息)` }))}
          notFoundContent={<Empty description="暂无会话" image={Empty.PRESENTED_IMAGE_SIMPLE} />}
        />
        <Button icon={<PlusOutlined />} onClick={handleNewSession}>新会话</Button>
        <Button
          icon={<BugOutlined />}
          type={showTraces ? "primary" : "default"}
          onClick={() => setShowTraces(!showTraces)}
        >
          Trace
        </Button>
        <Text type="secondary" style={{ fontSize: 12 }}>{events.length} 条消息 {traces.length > 0 && `| ${traces.length} 轮`}</Text>
      </div>

      {/* Main area: chat + trace */}
      <div style={{ flex: 1, display: "flex", overflow: "hidden" }}>
        {/* Left: Chat messages */}
        <div style={{ flex: showTraces ? "1 1 50%" : "1 1 100%", overflow: "auto", padding: "16px 24px", borderRight: showTraces ? "1px solid #f0f0f0" : "none" }}>
          {events.length === 0 && !loading && (
            <Empty description="开始一个新会话，与 AI 编程助手对话" style={{ marginTop: 80 }} />
          )}
          {events.map((evt, i) => (
            <MessageBubble key={i} evt={evt} />
          ))}
          {loading && (
            <div style={{ textAlign: "center", padding: 16 }}>
              <Spin tip="思考中..." />
            </div>
          )}
          <div ref={messagesEndRef} />
        </div>

        {/* Right: Trace viewer */}
        {showTraces && (
          <div style={{ flex: "1 1 50%", overflow: "auto", padding: "12px 16px", background: "#fafafa" }}>
            <Title level={5} style={{ marginBottom: 12 }}>
              <BugOutlined /> Agent-LLM 交互 Trace
              <Tag style={{ marginLeft: 8 }}>{traces.length} 轮</Tag>
            </Title>
            {traces.length === 0 && (
              <Empty description="发送消息后，这里将显示每一轮 agent 与 LLM 的完整交互内容" image={Empty.PRESENTED_IMAGE_SIMPLE} />
            )}
            {traces.map((t, i) => (
              <TraceTurnCard key={i} trace={t} turnIdx={i} />
            ))}
          </div>
        )}
      </div>

      {/* Bottom: Input */}
      <div style={{ padding: "12px 24px", borderTop: "1px solid #f0f0f0" }}>
        <Space.Compact style={{ width: "100%" }}>
          <TextArea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="输入消息，Enter 发送，Shift+Enter 换行"
            rows={2}
            disabled={loading}
            style={{ flex: 1 }}
          />
          <Button
            type="primary"
            icon={<SendOutlined />}
            onClick={handleSend}
            loading={loading}
            disabled={!input.trim()}
            style={{ height: "auto" }}
          >
            发送
          </Button>
        </Space.Compact>
      </div>
    </Layout>
  );
}
