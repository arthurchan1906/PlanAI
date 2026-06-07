import { useState, useEffect } from "react";
import { Button, Card, Form, Input, Space, Tag, Typography, message } from "antd";
import { ReloadOutlined, CheckCircleOutlined } from "@ant-design/icons";
import { api } from "../utils/api";

const { Title } = Typography;

export default function SettingsView() {
  const [loading, setLoading] = useState(false);
  const [testing, setTesting] = useState(false);
  const [aiStatus, setAiStatus] = useState(null);
  const [form] = Form.useForm();

  async function load() {
    setLoading(true);
    const data = await api("/pmai/config");
    form.setFieldsValue(data);
    setAiStatus(data.ai_enabled ? "enabled" : "disabled");
    setLoading(false);
  }

  useEffect(() => { load(); }, []);

  async function onFinish(values) {
    await api("/pmai/config", { method: "POST", body: JSON.stringify(values) });
    message.success("配置已保存");
    load();
  }

  async function embedAll() {
    setTesting(true);
    const data = await api("/pmai/discussions/embed", { method: "POST" });
    if (data.ok) {
      message.success(`已为 ${data.embedded} 条讨论生成 embedding`);
    } else {
      message.error(data.error || "embedding 失败");
    }
    setTesting(false);
  }

  async function testConnection() {
    setTesting(true);
    const data = await api("/pmai/ai-test", { method: "POST" });
    if (data.ok) {
      message.success(data.message || "AI 连接正常");
      setAiStatus("enabled");
    } else {
      message.error(data.error || "连接失败");
      setAiStatus("error");
    }
    setTesting(false);
  }

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Title level={4} style={{ margin: 0 }}>⚙️ 设置</Title>
        <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>
      </Space>

      <Card size="small" title="AI 配置" style={{ marginBottom: 16 }}>
        <Form form={form} layout="vertical" onFinish={onFinish}>
          <Form.Item name="ai_endpoint" label="对话 API 端点">
            <Input placeholder="http://127.0.0.1:8080/v1 或 https://api.openai.com/v1" />
          </Form.Item>
          <Form.Item name="ai_embedding_endpoint" label="Embedding API 端点 (可选，默认同对话端点)">
            <Input placeholder="留空则使用对话端点，或单独设置如 http://127.0.0.1:8081/v1" />
          </Form.Item>
          <Space>
            <Form.Item name="ai_chat_model" label="对话模型">
              <Input placeholder="qwen3 / gpt-4o-mini" style={{ width: 200 }} />
            </Form.Item>
            <Form.Item name="ai_model" label="Embedding 模型">
              <Input placeholder="bge-m3 / text-embedding-3-small" style={{ width: 220 }} />
            </Form.Item>
          </Space>
          <Space>
            <Form.Item>
              <Button type="primary" htmlType="submit" loading={loading}>保存</Button>
            </Form.Item>
            <Form.Item>
              <Button icon={<CheckCircleOutlined />} onClick={testConnection} loading={testing}>测试连接</Button>
              <Button onClick={embedAll} loading={testing}>为讨论生成 embedding</Button>
            </Form.Item>
          </Space>
        </Form>

        <div style={{ marginTop: 12 }}>
          AI 状态: {aiStatus === "enabled" ? <Tag color="green">已连接</Tag>
            : aiStatus === "error" ? <Tag color="red">连接失败</Tag>
            : <Tag color="default">未配置</Tag>}
        </div>
        <div style={{ color: "#888", fontSize: 12, marginTop: 8 }}>
          本地模型示例: endpoint=http://127.0.0.1:8080/v1, chat=你的模型名, embedding=bge-m3<br/>
          远程 OpenAI: endpoint=https://api.openai.com/v1, chat=gpt-4o-mini, embedding=text-embedding-3-small
        </div>
      </Card>
    </div>
  );
}
