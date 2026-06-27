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
            <Input placeholder="https://api.openai.com/v1" />
          </Form.Item>
          <Form.Item name="ai_embedding_endpoint" label="Embedding API 端点 (可选)">
            <Input placeholder="留空则使用对话端点" />
          </Form.Item>
          <Space>
            <Form.Item name="ai_chat_model" label="对话模型">
              <Input placeholder="gpt-4o-mini" style={{ width: 200 }} />
            </Form.Item>
            <Form.Item name="ai_model" label="Embedding 模型">
              <Input placeholder="text-embedding-3-small" style={{ width: 220 }} />
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
      </Card>

      <Card size="small" title="Proxy 配置">
        <Form form={form} layout="vertical" onFinish={onFinish}>
          <Form.Item name="upstream_url" label="上游 API 端点 (OpenAI 协议)">
            <Input placeholder="https://api.deepseek.com" />
          </Form.Item>
          <Form.Item name="anthropic_url" label="Anthropic 端点 (Claude Code 透传)">
            <Input placeholder="https://api.deepseek.com/anthropic" />
          </Form.Item>
          <Space>
            <Form.Item name="proxy_port" label="Proxy 端口">
              <Input placeholder="19530" style={{ width: 120 }} />
            </Form.Item>
            <Form.Item name="proxy_model" label="模型覆写 (可选)">
              <Input placeholder="留空透传 Agent 的模型名" style={{ width: 220 }} />
            </Form.Item>
          </Space>
          <Form.Item name="proxy_log_dir" label="流量日志目录 (可选)">
            <Input placeholder="/tmp/aipmc-traces" />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={loading}>保存</Button>
          </Form.Item>
        </Form>
        <div style={{ color: "#888", fontSize: 12, marginTop: 8 }}>
          API Key 通过环境变量 UPSTREAM_KEY 设置，不保存在文件中。<br/>
          设置 Anthropic 端点后，Claude Code 请求将直接透传，绕过 OpenAI 翻译层，
          完整保留 thinking/signature/tool_use 结构。
        </div>
      </Card>
    </div>
  );
}
