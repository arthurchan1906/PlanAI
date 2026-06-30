import { useState, useEffect } from "react";
import { Button, Card, Form, Input, Collapse, Space, Tabs, Typography, message, Tag } from "antd";
import { api } from "../utils/api";

const { Title, Text } = Typography;

// ── Convert extra_env map → Form.List array ──
function extraEnvToArray(obj) {
  if (!obj || typeof obj !== "object" || Array.isArray(obj)) return [];
  return Object.entries(obj).map(([key, value]) => ({ key, value: String(value) }));
}

// ── Convert Form.List array → extra_env map ──
function extraEnvToMap(arr) {
  if (!Array.isArray(arr)) return {};
  const map = {};
  for (const item of arr) {
    if (item && item.key) map[item.key] = item.value || "";
  }
  return map;
}

// ── Auto-injected env vars (read-only, shown for reference) ──
const AUTO_INJECT = {
  claude: [
    { key: "ANTHROPIC_BASE_URL",       desc: "Proxy 地址" },
    { key: "ANTHROPIC_AUTH_TOKEN",     desc: "API Key" },
    { key: "ANTHROPIC_MODEL",          desc: "模型名（来自上方配置）" },
    { key: "CLAUDE_CODE_SUBAGENT_MODEL", desc: "子 Agent 模型（来自上方配置）" },
  ],
  codex: [
    { key: "OPENAI_API_KEY",   desc: "API Key" },
    { key: "OPENAI_BASE_URL",  desc: "由 proxy.config.toml 指定" },
  ],
  gemini: [
    { key: "GEMINI_API_KEY",        desc: "API Key" },
    { key: "GOOGLE_API_KEY",        desc: "API Key (备用)" },
    { key: "GEMINI_API_BASE",       desc: "Proxy 地址" },
    { key: "GOOGLE_API_BASE",       desc: "Proxy 地址 (备用)" },
    { key: "GOOGLE_GEMINI_BASE_URL", desc: "Proxy 地址 (备用)" },
  ],
};

const AGENT_LABELS = { claude: "Claude Code", codex: "Codex CLI", gemini: "Gemini CLI", opencode: "OpenCode" };

export default function AgentConfigView() {
  const [activeTab, setActiveTab] = useState("claude");
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(null); // which agent is saving
  const [claudeForm] = Form.useForm();
  const [codexForm] = Form.useForm();
  const [geminiForm] = Form.useForm();
  const [opencodeForm] = Form.useForm();

  async function loadProfiles() {
    setLoading(true);
    try {
      const data = await api("/pmai/config");
      if (data.claude) {
        claudeForm.setFieldsValue({
          ...data.claude,
          extra_env: extraEnvToArray(data.claude.extra_env),
        });
      }
      if (data.codex) {
        codexForm.setFieldsValue({
          ...data.codex,
          extra_env: extraEnvToArray(data.codex.extra_env),
        });
      }
      if (data.gemini) {
        geminiForm.setFieldsValue({
          ...data.gemini,
          extra_env: extraEnvToArray(data.gemini.extra_env),
        });
      }
      if (data.opencode) {
        opencodeForm.setFieldsValue({
          ...data.opencode,
          extra_env: extraEnvToArray(data.opencode.extra_env),
          models: (data.opencode.models || []).map(m => ({ name: m })),
        });
      }
    } catch (e) {
      // ignore
    }
    setLoading(false);
  }

  useEffect(() => { loadProfiles(); }, []);

  async function saveProfile(agent) {
    setSaving(agent);
    let form;
    switch (agent) {
      case "claude": form = claudeForm; break;
      case "codex":  form = codexForm; break;
      case "gemini":   form = geminiForm;   break;
      case "opencode": form = opencodeForm; break;
    }
    const values = form.getFieldsValue();
    // Convert Form.List extra_env array back to map
    if (values.extra_env) {
      values.extra_env = extraEnvToMap(values.extra_env);
    }
    // Convert Form.List models array back to string array (for opencode)
    if (values.models && Array.isArray(values.models)) {
      values.models = values.models.map(m => m.name).filter(Boolean);
    }
    try {
      await api("/pmai/config", {
        method: "POST",
        body: JSON.stringify({ [agent]: values }),
      });
      message.success(`${AGENT_LABELS[agent]} 配置已保存`);
    } catch (e) {
      message.error(e.message);
    }
    setSaving(null);
  }

  // ── Shared ExtraEnv Form.List section ──
  function extraEnvSection(form) {
    return (
      <Collapse ghost size="small" items={[{
        key: "extra",
        label: "额外环境变量（高级）",
        children: (
          <Form.Item noStyle>
            <Form.List name="extra_env">
              {(fields, { add, remove }) => (
                <>
                  <Text type="secondary" style={{ display: "block", marginBottom: 8, fontSize: 12 }}>
                    不常用的配置项以 KEY=VALUE 形式注入。Agent 自身的 extra_env 会覆盖全局设置。
                  </Text>
                  {fields.map(({ key, name }) => (
                    <Space key={key} style={{ display: "flex", marginBottom: 8 }} align="baseline">
                      <Form.Item name={[name, "key"]} noStyle>
                        <Input placeholder="变量名" style={{ width: 260 }} />
                      </Form.Item>
                      <Form.Item name={[name, "value"]} noStyle>
                        <Input placeholder="值" style={{ width: 260 }} />
                      </Form.Item>
                      <Button size="small" danger onClick={() => remove(name)}>×</Button>
                    </Space>
                  ))}
                  <Button type="dashed" onClick={() => add({ key: "", value: "" })} block>
                    + 添加
                  </Button>
                </>
              )}
            </Form.List>
          </Form.Item>
        ),
      }]} />
    );
  }

  // ── Auto-injected env vars (read-only) ──
  function injectedSection(agent) {
    return (
      <Collapse ghost size="small" items={[{
        key: "injected",
        label: "自动注入的环境变量（只读）",
        children: (
          <div style={{ fontFamily: "monospace", fontSize: 12, color: "#8b949e" }}>
            {AUTO_INJECT[agent].map((env) => (
              <div key={env.key} style={{ marginBottom: 2 }}>
                <code>{env.key}</code> — {env.desc}
              </div>
            ))}
          </div>
        ),
      }]} />
    );
  }

  // ── Claude Code Tab ──
  const claudeTab = (
    <Form form={claudeForm} layout="vertical" key="claude-form">
      <Form.Item name="model" label="模型" tooltip="→ ANTHROPIC_MODEL。支持 [1m] 后缀指定上下文长度。" style={{ maxWidth: 400 }}>
        <Input placeholder="deepseek-v4-pro[1m]" />
      </Form.Item>
      <Space wrap>
        <Form.Item name="sub_agent_model" label="子 Agent 模型" tooltip="→ CLAUDE_CODE_SUBAGENT_MODEL">
          <Input placeholder="deepseek-v4-flash" style={{ width: 220 }} />
        </Form.Item>
        <Form.Item name="effort_level" label="Effort Level" tooltip="→ CLAUDE_CODE_EFFORT_LEVEL (min / medium / max)">
          <Input placeholder="max" style={{ width: 120 }} />
        </Form.Item>
      </Space>
      <Space wrap>
        <Form.Item name="opus_model" label="Opus 模型" tooltip="→ ANTHROPIC_DEFAULT_OPUS_MODEL">
          <Input placeholder="deepseek-v4-pro[1m]" style={{ width: 220 }} />
        </Form.Item>
        <Form.Item name="sonnet_model" label="Sonnet 模型" tooltip="→ ANTHROPIC_DEFAULT_SONNET_MODEL">
          <Input placeholder="deepseek-v4-pro[1m]" style={{ width: 220 }} />
        </Form.Item>
      </Space>
      <Space wrap>
        <Form.Item name="haiku_model" label="Haiku 模型" tooltip="→ ANTHROPIC_DEFAULT_HAIKU_MODEL">
          <Input placeholder="deepseek-v4-flash" style={{ width: 220 }} />
        </Form.Item>
        <Form.Item name="small_fast_model" label="Small Fast 模型" tooltip="→ ANTHROPIC_SMALL_FAST_MODEL">
          <Input placeholder="deepseek-v4-flash" style={{ width: 220 }} />
        </Form.Item>
      </Space>
      {extraEnvSection(claudeForm)}
      {injectedSection("claude")}
      <Form.Item style={{ marginTop: 12 }}>
        <Button type="primary" onClick={() => saveProfile("claude")} loading={saving === "claude"}>
          保存 Claude Code 配置
        </Button>
      </Form.Item>
    </Form>
  );

  // ── Codex CLI Tab ──
  const codexTab = (
    <Form form={codexForm} layout="vertical" key="codex-form">
      <Form.Item name="model" label={
        <span>模型 <Tag color="processing" style={{ fontSize: 10, marginLeft: 6 }}>不含 [1m] 后缀</Tag></span>
      } tooltip="→ 写入 proxy.config.toml。Anthopic 专用的 [1m] 等后缀无需填写。" style={{ maxWidth: 400 }}>
        <Input placeholder="deepseek-v4-pro" />
      </Form.Item>
      <Form.Item name="reasoning_effort" label="Reasoning Effort" tooltip="→ proxy.config.toml model_reasoning_effort (low / medium / high)" style={{ maxWidth: 240 }}>
        <Input placeholder="medium" />
      </Form.Item>
      {extraEnvSection(codexForm)}
      {injectedSection("codex")}
      <Form.Item style={{ marginTop: 12 }}>
        <Button type="primary" onClick={() => saveProfile("codex")} loading={saving === "codex"}>
          保存 Codex CLI 配置
        </Button>
      </Form.Item>
    </Form>
  );

  // ── Gemini CLI Tab ──
  const geminiTab = (
    <Form form={geminiForm} layout="vertical" key="gemini-form">
      <Text type="secondary" style={{ display: "block", marginBottom: 16 }}>
        Gemini 的模型由 proxy 根据请求动态选择，无需在此配置。
      </Text>
      {extraEnvSection(geminiForm)}
      {injectedSection("gemini")}
      <Form.Item style={{ marginTop: 12 }}>
        <Button type="primary" onClick={() => saveProfile("gemini")} loading={saving === "gemini"}>
          保存 Gemini CLI 配置
        </Button>
      </Form.Item>
    </Form>
  );

  // ── OpenCode Tab ──
  const opencodeTab = (
    <Form form={opencodeForm} layout="vertical" key="opencode-form">
      <Text type="secondary" style={{ display: "block", marginBottom: 16 }}>
        OpenCode 通过 opencode.json 自定义 provider 连接 AIPM proxy。<br/>
        每个模型会写入 <code>provider.aipm.models</code>，启动时传入第一个模型。<br/>
        Provider（baseURL 等）请在 opencode.json 中手动配置。
      </Text>
      <Form.Item label="模型列表" tooltip="写入 opencode.json 的 provider.aipm.models。保存后自动同步。" style={{ maxWidth: 500 }}>
        <Form.List name="models">
          {(fields, { add, remove }) => (
            <>
              {fields.map(({ key, name }) => (
                <Space key={key} style={{ display: "flex", marginBottom: 8 }} align="baseline">
                  <Form.Item name={[name, "name"]} noStyle>
                    <Input placeholder="deepseek-v4-pro" style={{ width: 300 }} />
                  </Form.Item>
                  <Button size="small" danger onClick={() => remove(name)}>×</Button>
                </Space>
              ))}
              <Button type="dashed" onClick={() => add({ name: "" })} block>
                + 添加模型
              </Button>
            </>
          )}
        </Form.List>
      </Form.Item>
      {extraEnvSection(opencodeForm)}
      <Form.Item style={{ marginTop: 12 }}>
        <Button type="primary" onClick={() => saveProfile("opencode")} loading={saving === "opencode"}>
          保存 OpenCode 配置
        </Button>
      </Form.Item>
    </Form>
  );

  return (
    <Card
      size="small"
      title={<Title level={5} style={{ margin: 0 }}>Agent 配置</Title>}
      extra={<Button size="small" onClick={loadProfiles} loading={loading}>刷新</Button>}
      style={{ marginTop: 16 }}
    >
      <Tabs
        activeKey={activeTab}
        onChange={setActiveTab}
        items={[
          { key: "claude", label: "Claude Code", children: claudeTab },
          { key: "codex",  label: "Codex CLI",  children: codexTab },
          { key: "gemini",   label: "Gemini CLI", children: geminiTab },
          { key: "opencode", label: "OpenCode",    children: opencodeTab },
        ]}
      />
    </Card>
  );
}
