import { useState, useRef, useEffect } from "react";
import { Button, Card, Form, Input, Select, Space, Table, Tag, Typography, message, Switch } from "antd";
import { PlusOutlined, ReloadOutlined, SendOutlined, RobotOutlined, UserOutlined } from "@ant-design/icons";
import { api } from "../utils/api";

const { Title, Text, Paragraph } = Typography;
const { TextArea } = Input;

export default function MeetingsView({ meetings, agents, loading, loadAll, busy }) {
  const [formVisible, setFormVisible] = useState(false);
  const [selected, setSelected] = useState(null);
  const [msgText, setMsgText] = useState("");
  const [callAgent, setCallAgent] = useState("");
  const [form] = Form.useForm();
  const chatEnd = useRef(null);

  useEffect(() => { chatEnd.current?.scrollIntoView({ behavior: "smooth" }); }, [selected?.turns]);

  const refresh = () => {
    loadAll();
    if (selected) {
      const updated = meetings.find(m => m.id === selected.id);
      if (updated) setSelected(updated);
    }
  };

  async function onFinish(values) {
    values.auto_arbitrate = values.auto_arbitrate || false;
    values.topic = values.title;
    await api("/pmai/meetings", { method: "POST", body: JSON.stringify(values) });
    message.success("会议已创建");
    form.resetFields();
    setFormVisible(false);
    loadAll();
  }

  async function sendMessage() {
    if (!msgText.trim() && !callAgent) return;
    const text = msgText.trim() || "(点名发言)";
    if (callAgent) {
      // PM names an agent to speak
      await api(`/pmai/meetings/${selected.id}/turns`, {
        method: "POST",
        body: JSON.stringify({ speaker_type: "agent", speaker_id: callAgent, question: text }),
      });
    } else {
      // PM sends a general message
      await api(`/pmai/meetings/${selected.id}/turns`, {
        method: "POST",
        body: JSON.stringify({ speaker_type: "human", speaker_id: "PM", question: text }),
      });
    }
    setMsgText("");
    setCallAgent("");
    refresh();
  }

  async function closeRoom(id) {
    await api(`/pmai/meetings/${id}/close`, { method: "POST" });
    message.success("会议已结束");
    loadAll();
    setSelected(null);
  }

  async function arbitrateNext(roomId) {
    const res = await api(`/pmai/arbitrate`, { method: "POST", body: JSON.stringify({ room_id: roomId }) });
    message.success("仲裁完成");
    refresh();
  }

  function msgStyle(turn) {
    const isAgent = turn.speaker_type === "agent";
    const isWaiting = turn.status === "waiting";
    const isProcessing = turn.status === "processing";
    let bg = isAgent ? "#f6ffed" : "#e6f7ff";
    if (isWaiting) bg = "#fff7e6";
    if (isProcessing) bg = "#fffbe6";
    return {
      alignSelf: isAgent ? "flex-start" : "flex-end",
      background: bg,
      border: isWaiting ? "1px solid #faad14" : isProcessing ? "1px solid #faad14" : "1px solid #f0f0f0",
      borderRadius: 8,
      padding: "8px 12px",
      maxWidth: "75%",
      marginBottom: 8,
    };
  }

  const columns = [
    { title: "主题", dataIndex: "title", key: "title", ellipsis: true },
    { title: "模式", dataIndex: "meeting_mode", key: "mode", width: 70, render: (m) => <Tag>{m || "discussion"}</Tag> },
    { title: "仲裁", dataIndex: "auto_arbitrate", key: "arb", width: 40, render: (a) => a ? "🔮" : "" },
    { title: "状态", dataIndex: "status", key: "status", width: 60, render: (s) => <Tag color={s === "active" ? "blue" : "default"}>{s}</Tag> },
  ];

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Title level={4} style={{ margin: 0 }}>📞 会议管理</Title>
        <Button icon={<ReloadOutlined />} onClick={loadAll} loading={loading}>刷新</Button>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setFormVisible(!formVisible)}>创建会议</Button>
      </Space>

      {formVisible && (
        <Card size="small" style={{ marginBottom: 16 }}>
          <Form form={form} layout="vertical" onFinish={onFinish} initialValues={{ meeting_mode: "discussion", auto_arbitrate: true }}>
            <Form.Item name="title" label="会议主题" rules={[{ required: true }]}>
              <TextArea rows={2} placeholder="本次会议要讨论的核心问题" />
            </Form.Item>
            <Form.Item name="context" label="背景材料 (可选)">
              <TextArea rows={3} placeholder="相关文档、现有方案、约束条件等" />
            </Form.Item>
            <Space>
              <Form.Item name="meeting_mode" label="会议模式">
                <Select options={[{ value: "discussion", label: "讨论 (不改代码)" }, { value: "debugging", label: "会诊 (可改代码)" }]} />
              </Form.Item>
              <Form.Item name="auto_arbitrate" label="AI 自动仲裁" valuePropName="checked">
                <Switch />
              </Form.Item>
              <Form.Item name="created_by" label="创建者" initialValue="PM">
                <Input style={{ width: 120 }} />
              </Form.Item>
            </Space>
            <Form.Item><Button type="primary" htmlType="submit" loading={busy}>创建会议</Button></Form.Item>
          </Form>
        </Card>
      )}

      <Table
        dataSource={meetings} columns={columns} rowKey="id" loading={loading} size="small"
        onRow={(r) => ({ onClick: () => setSelected(r), style: { cursor: "pointer" } })}
      />

      {/* Chat-style meeting drawer */}
      {selected && (
        <div style={{
          position: "fixed", right: 0, top: 0, width: 520, height: "100vh",
          background: "#fff", boxShadow: "-2px 0 8px rgba(0,0,0,0.1)", zIndex: 1000,
          display: "flex", flexDirection: "column",
        }}>
          {/* Header */}
          <div style={{ padding: "12px 16px", borderBottom: "1px solid #f0f0f0", display: "flex", justifyContent: "space-between", alignItems: "center" }}>
            <div>
              <Title level={5} style={{ margin: 0 }}>{selected.title}</Title>
              <Space size={4}>
                <Tag color="blue">{selected.meeting_mode || "discussion"}</Tag>
                {selected.auto_arbitrate ? <Tag color="purple">🔮</Tag> : null}
                <Tag>{selected.status}</Tag>
              </Space>
            </div>
            <Space>
              <Button size="small" onClick={refresh}>刷新</Button>
              <Button size="small" onClick={() => setSelected(null)}>✕</Button>
            </Space>
          </div>

          {/* Context */}
          {selected.context && (
            <div style={{ padding: "8px 16px", fontSize: 12, color: "#888", borderBottom: "1px solid #f0f0f0" }}>
              {selected.context}
            </div>
          )}

          {/* Participants */}
          <div style={{ padding: "6px 16px", borderBottom: "1px solid #f0f0f0" }}>
            {selected.participants?.map((p, i) => (
              <Tag key={i} color={p.status === "ready" ? "green" : "default"}>{p.name} ({p.role})</Tag>
            ))}
            {(!selected.participants || selected.participants.length === 0) && <Text type="secondary" style={{ fontSize: 12 }}>暂无 Agent 加入</Text>}
          </div>

          {/* Message list */}
          <div style={{ flex: 1, overflow: "auto", padding: "12px 16px", background: "#f5f5f5" }}>
            {(!selected.turns || selected.turns.length === 0) ? (
              <Text type="secondary">暂无消息</Text>
            ) : (
              selected.turns.map((turn, i) => {
                const isAgent = turn.speaker_type === "agent";
                const isHuman = turn.speaker_type === "human";
                const name = isHuman ? "PM" : turn.speaker_id;
                const text = turn.question !== "(点名发言)" && turn.question !== "[主动发言]" ? turn.question : "";
                const resp = turn.response;
                return (
                  <div key={turn.id} style={{ display: "flex", flexDirection: "column", alignItems: isHuman ? "flex-end" : "flex-start" }}>
                    <Text style={{ fontSize: 11, color: "#888", marginBottom: 2, paddingLeft: 4 }}>
                      {name} {isAgent && turn.status === "waiting" && "⏳"} {isAgent && turn.status === "processing" && "🔄"}
                      {turn.address_to && ` → ${turn.address_to}`}
                    </Text>
                    <div style={msgStyle(turn)}>
                      <Paragraph style={{ margin: 0, whiteSpace: "pre-wrap" }}>
                        {text && <span>{text}</span>}
                        {resp && <span style={{ color: isAgent ? "#389e0d" : undefined }}>{text ? "\n→ " : ""}{resp}</span>}
                        {!text && !resp && <span style={{ color: "#888" }}>(点名发言)</span>}
                      </Paragraph>
                    </div>
                  </div>
                );
              })
            )}
            <div ref={chatEnd} />
          </div>

          {/* Input area */}
          {selected.status === "active" && (
            <div style={{ padding: "12px 16px", borderTop: "1px solid #f0f0f0", background: "#fafafa" }}>
              <Space style={{ width: "100%", marginBottom: 8 }}>
                <Select
                  allowClear
                  style={{ width: 200 }}
                  placeholder="点名 (可选)"
                  value={callAgent || undefined}
                  onChange={(v) => setCallAgent(v || "")}
                  options={agents.map(a => ({ value: a.id, label: a.name }))}
                />
                {selected.auto_arbitrate && (
                  <Button size="small" icon={<RobotOutlined />} onClick={() => arbitrateNext(selected.id)}>仲裁</Button>
                )}
              </Space>
              <Input.TextArea
                rows={2}
                placeholder={callAgent ? "对 Agent 的提问 (可选)" : "输入消息..."}
                value={msgText}
                onChange={(e) => setMsgText(e.target.value)}
                onPressEnter={(e) => { if (!e.shiftKey) { e.preventDefault(); sendMessage(); } }}
              />
              <Button type="primary" icon={<SendOutlined />} onClick={sendMessage} style={{ marginTop: 8 }}
                disabled={!msgText.trim() && !callAgent}>
                发送
              </Button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
