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
} from "antd";
import { SendOutlined, RobotOutlined, UserOutlined, ToolOutlined, PlusOutlined } from "@ant-design/icons";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import "highlight.js/styles/github.css";
import { chatSend, chatGetSessions, chatGetSession } from "../utils/api";

const { TextArea } = Input;
const { Text, Title } = Typography;

function ToolCallCard({ tool }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div style={{ margin: "8px 0" }}>
      <Tag
        color="geekblue"
        style={{ cursor: "pointer", fontSize: 12 }}
        onClick={() => setExpanded(!expanded)}
      >
        <ToolOutlined /> {tool.name}
        {expanded ? " ▲" : " ▼"}
      </Tag>
      {expanded && (
        <Card size="small" style={{ marginTop: 4, background: "#f0f5ff" }}>
          <Text type="secondary" style={{ fontSize: 11 }}>参数:</Text>
          <pre style={{ fontSize: 11, margin: "2px 0", whiteSpace: "pre-wrap" }}>
            {JSON.stringify(tool.args, null, 1)}
          </pre>
          {tool.result && (
            <>
              <Text type="secondary" style={{ fontSize: 11 }}>结果:</Text>
              <pre style={{ fontSize: 11, margin: "2px 0", whiteSpace: "pre-wrap", maxHeight: 200, overflow: "auto" }}>
                {tool.result}
              </pre>
            </>
          )}
        </Card>
      )}
    </div>
  );
}

function MessageBubble({ evt }) {
  if (evt.role === "user") {
    return (
      <div style={{ marginBottom: 16, display: "flex", justifyContent: "flex-end" }}>
        <Card
          size="small"
          style={{ maxWidth: "75%", background: "#2f6fec", borderColor: "#2f6fec" }}
          bodyStyle={{ padding: "8px 14px" }}
        >
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
                <ReactMarkdown
                  remarkPlugins={[remarkGfm]}
                  rehypePlugins={[rehypeHighlight]}
                >
                  {evt.content}
                </ReactMarkdown>
              </div>
            </Card>
          )}
          {evt.tool_calls?.map((tc, i) => (
            <ToolCallCard key={tc.id || i} tool={{ ...tc, result: evt.tool_results?.[i] }} />
          ))}
        </div>
      </div>
    );
  }

  if (evt.role === "tool") {
    return (
      <div style={{ marginBottom: 8, marginLeft: 40 }}>
        <Tag color="blue" style={{ fontSize: 11 }}>
          <ToolOutlined /> {evt.tool_name}
        </Tag>
        <Text type="secondary" style={{ fontSize: 11, marginLeft: 4 }}>
          {evt.tool_result?.slice?.(0, 80) || ""}
        </Text>
      </div>
    );
  }

  return null;
}

export default function ChatView() {
  const [sessions, setSessions] = useState([]);
  const [currentSessionId, setCurrentSessionId] = useState(null);
  const [events, setEvents] = useState([]);
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(false);
  const messagesEndRef = useRef(null);

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, []);

  // Load session list on mount
  useEffect(() => {
    chatGetSessions()
      .then((data) => {
        const list = data.sessions || [];
        setSessions(list);
        if (list.length > 0 && !currentSessionId) {
          handleSelectSession(list[0].id);
        }
      })
      .catch(() => {});
  }, []);

  // Scroll on new events
  useEffect(() => {
    scrollToBottom();
  }, [events]);

  function handleSelectSession(id) {
    setCurrentSessionId(id);
    chatGetSession(id)
      .then((data) => setEvents(data.events || []))
      .catch(() => setEvents([]));
  }

  function handleNewSession() {
    setCurrentSessionId(null);
    setEvents([]);
    setInput("");
  }

  async function handleSend() {
    const text = input.trim();
    if (!text) return;
    setInput("");
    setLoading(true);

    // Show user message immediately
    setEvents((prev) => [...prev, { role: "user", content: text }]);

    try {
      const data = await chatSend(text, currentSessionId);
      // Refresh session ID if new
      if (data.session_id && data.session_id !== currentSessionId) {
        setCurrentSessionId(data.session_id);
        // Refresh session list
        chatGetSessions().then((d) => setSessions(d.sessions || [])).catch(() => {});
      }
      // Replace with full event list from server
      setEvents(data.events || []);
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

  return (
    <Layout style={{ height: "100%", background: "#fff" }}>
      {/* Session selector */}
      <div style={{ padding: "12px 16px", borderBottom: "1px solid #f0f0f0", display: "flex", alignItems: "center", gap: 12 }}>
        <Select
          style={{ flex: 1 }}
          placeholder="选择会话..."
          value={currentSessionId}
          onChange={handleSelectSession}
          options={sessions.map((s) => ({
            value: s.id,
            label: `${s.id} (${s.events} 条消息)`,
          }))}
          notFoundContent={<Empty description="暂无会话" image={Empty.PRESENTED_IMAGE_SIMPLE} />}
        />
        <Button icon={<PlusOutlined />} onClick={handleNewSession}>
          新会话
        </Button>
        <Text type="secondary" style={{ fontSize: 12 }}>
          {events.length} 条消息
        </Text>
      </div>

      {/* Messages */}
      <div style={{ flex: 1, overflow: "auto", padding: "16px 24px" }}>
        {events.length === 0 && !loading && (
          <Empty
            description="开始一个新会话，与 AI 编程助手对话"
            style={{ marginTop: 80 }}
          />
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

      {/* Input */}
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
