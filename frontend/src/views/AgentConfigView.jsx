import { useState, useEffect } from "react";
import { Button, Card, Form, Input, Select, Collapse, Space, Tabs, Checkbox, Typography, message, Tag } from "antd";
import { api } from "../utils/api";

const { Title, Text } = Typography;

function extraEnvToArray(obj) {
  if (!obj || typeof obj !== "object" || Array.isArray(obj)) return [];
  return Object.entries(obj).map(([key, value]) => ({ key, value: String(value) }));
}
function extraEnvToMap(arr) {
  if (!Array.isArray(arr)) return {};
  const map = {};
  for (const item of arr) { if (item && item.key) map[item.key] = item.value || ""; }
  return map;
}

const EFFORT_OPTS = [{ value: "min", label: "min" }, { value: "medium", label: "medium" }, { value: "max", label: "max" }];
const REASON_OPTS = [
  { value: "low", label: "low" }, { value: "medium", label: "medium" }, { value: "high", label: "high" },
];

const AUTO_INJECT = {
  claude: [
    { key: "ANTHROPIC_BASE_URL", desc: "Proxy address" },
    { key: "ANTHROPIC_AUTH_TOKEN", desc: "local" },
    { key: "ANTHROPIC_MODEL", desc: "model name (from config above)" },
    { key: "CLAUDE_CODE_SUBAGENT_MODEL", desc: "SubAgent model" },
    { key: "ANTHROPIC_DEFAULT_OPUS_MODEL", desc: "Opus model" },
    { key: "ANTHROPIC_DEFAULT_SONNET_MODEL", desc: "Sonnet model" },
    { key: "ANTHROPIC_DEFAULT_HAIKU_MODEL", desc: "Haiku model" },
    { key: "ANTHROPIC_SMALL_FAST_MODEL", desc: "SmallFast model" },
    { key: "CLAUDE_CODE_EFFORT_LEVEL", desc: "Effort Level" },
  ],
  codex: [
    { key: "OPENAI_API_KEY", desc: "local" },
    { key: "OPENAI_BASE_URL", desc: "specified by proxy.config.toml" },
  ],
  gemini: [
    { key: "GEMINI_API_KEY", desc: "local" }, { key: "GOOGLE_API_KEY", desc: "local" },
    { key: "GEMINI_API_BASE", desc: "Proxy address" }, { key: "GOOGLE_API_BASE", desc: "Proxy address" },
    { key: "GOOGLE_GEMINI_BASE_URL", desc: "Proxy address" },
  ],
};

const AGENT_LABELS = { claude: "Claude Code", codex: "Codex CLI", gemini: "Gemini CLI", opencode: "OpenCode" };

export default function AgentConfigView({ models = [], keys = {} }) {
  const [activeTab, setActiveTab] = useState("claude");
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(null);
  const [modelList, setModelList] = useState(models);
  const [currentModel, setCurrentModel] = useState({});
  const [visionModel, setVisionModel] = useState("");
  const [switchingModel, setSwitchingModel] = useState(false);
  const [claudeForm] = Form.useForm();
  const [codexForm] = Form.useForm();
  const [geminiForm] = Form.useForm();
  const [opencodeForm] = Form.useForm();

    // Build model option label showing provider key status.
  function modelLabel(m) {
    const routes = m.routes || [];
    const parts = routes.map(r => {
      const hasKey = !!keys[r.provider];
      return r.provider + (hasKey ? "✓" : "✗");
    });
    const anyKey = routes.some(r => keys[r.provider]);
    const label = m.id + " (" + (parts.join(", ") || "-") + ")";
    return { value: m.id, label, disabled: !anyKey && routes.length > 0 };
  }

const modelOpts = (models.length > 0 ? models : modelList).map(m => modelLabel(m));

// Vision-capable models for the aipmc_vision selector. Local models legitimately
// have no API key, so unlike modelLabel these options are never disabled.
const visionModelOpts = (models.length > 0 ? models : modelList)
  .filter(m => (m.tags || []).includes("vision"))
  .map(m => {
    const routes = m.routes || [];
    const parts = routes.map(r => r.provider + (keys[r.provider] ? "✓" : "✗"));
    return { value: m.id, label: (m.display_name || m.id) + " (" + (parts.join(", ") || "-") + ")" };
  });

  async function loadProfiles() {
    setLoading(true);
    try {
      const data = await api("/pmai/config");
      setModelList(data.models || []);
      if (data.claude) claudeForm.setFieldsValue({ ...data.claude, extra_env: extraEnvToArray(data.claude.extra_env) });
      if (data.codex) codexForm.setFieldsValue({ ...data.codex, extra_env: extraEnvToArray(data.codex.extra_env) });
      if (data.gemini) geminiForm.setFieldsValue({ ...data.gemini, extra_env: extraEnvToArray(data.gemini.extra_env) });
      setVisionModel(data.vision_model || "");
      if (data.opencode) {
        const o = data.opencode;
        opencodeForm.setFieldsValue({ ...o, extra_env: extraEnvToArray(o.extra_env), models: o.models || [] });
      }
    } catch (e) { /* */ }
    setLoading(false);
  }

  async function fetchCurrentModels() {
    try { const m = await api("/pmai/model"); setCurrentModel(m.current || {}); } catch (e) { /* */ }
  }

  async function switchModel(agent, modelId) {
    setSwitchingModel(true);
    try {
      const d = await api("/pmai/model/switch", {
        method: "POST",
        body: JSON.stringify({ model: modelId, agent }),
      });
      if (d.ok) {
        message.success(modelId ? `${agent} switched to ${modelId}` : `${agent} switched to Auto`);
        setCurrentModel(prev => ({ ...prev, [agent]: modelId || "" }));
      } else message.error(d.error);
    } catch (e) { message.error(e.message); }
    setSwitchingModel(false);
  }

  async function saveVisionModel(v) {
    setSaving("vision");
    try {
      await api("/pmai/config", { method: "POST", body: JSON.stringify({ vision_model: v || "" }) });
      setVisionModel(v || "");
      message.success(v ? `Vision model set to ${v}` : "Vision model cleared (auto)");
    } catch (e) { message.error(e.message); }
    setSaving(null);
  }

  useEffect(() => { loadProfiles(); fetchCurrentModels(); }, []);

  async function saveProfile(agent) {
    setSaving(agent);
    const map = { claude: claudeForm, codex: codexForm, gemini: geminiForm, opencode: opencodeForm };
    const values = map[agent].getFieldsValue();
    if (values.extra_env) values.extra_env = extraEnvToMap(values.extra_env);
    try {
      await api("/pmai/config", { method: "POST", body: JSON.stringify({ [agent]: values }) });
      message.success(`${AGENT_LABELS[agent]} saved`);
    } catch (e) { message.error(e.message); }
    setSaving(null);
  }

  function extraEnvSection(form) {
    return (
      <Collapse ghost size="small" items={[{ key: "extra", label: "Extra Env (advanced)", children: (
        <Form.Item noStyle><Form.List name="extra_env">{(fields, { add, remove }) => (<>
          <Text type="secondary" style={{ display: "block", marginBottom: 8, fontSize: 12 }}>Custom KEY=VALUE pairs. Agent extra_env overrides global.</Text>
          {fields.map(({ key, name }) => (
            <Space key={key} style={{ display: "flex", marginBottom: 8 }} align="baseline">
              <Form.Item name={[name, "key"]} noStyle><Input placeholder="KEY" style={{ width: 260 }} /></Form.Item>
              <Form.Item name={[name, "value"]} noStyle><Input placeholder="VALUE" style={{ width: 260 }} /></Form.Item>
              <Button size="small" danger onClick={() => remove(name)}>&times;</Button>
            </Space>
          ))}
          <Button type="dashed" onClick={() => add({ key: "", value: "" })}>+ Add</Button>
        </>)}</Form.List></Form.Item>
      )}]} />
    );
  }

  function injectedSection(agent) {
    const entries = AUTO_INJECT[agent];
    if (!entries || entries.length === 0) return null;
    return (
      <Collapse ghost size="small" items={[{ key: "injected", label: "Auto-injected env vars (view only)", children: (
        <div>
          <Text type="secondary" style={{ display: "block", marginBottom: 8, fontSize: 12 }}>These are automatically set when the agent is launched. No manual config needed.</Text>
          {entries.map(({ key, desc }) => (
            <div key={key} style={{ marginBottom: 4 }}>
              <Tag style={{ fontFamily: "monospace" }}>{key}</Tag>
              <Text type="secondary" style={{ fontSize: 12 }}>{desc}</Text>
            </div>
          ))}
        </div>
      )}]} />
    );
  }

  function currentModelSelect(agent) {
    const allModels = models.length > 0 ? models : modelList;
    return (
      <div style={{ marginBottom: 16, display: "flex", alignItems: "center", gap: 8 }}>
        <Text strong style={{ fontSize: 12, whiteSpace: "nowrap" }}>Current:</Text>
        <Select
          value={currentModel[agent] || undefined}
          placeholder="Auto (use default)"
          style={{ width: 200 }}
          size="small"
          loading={switchingModel}
          onChange={v => switchModel(agent, v || "")}
          allowClear
          options={[
            { value: "", label: "Auto (use default)" },
            ...allModels.map(m => ({ value: m.id, label: m.display_name || m.id })),
          ]}
        />
      </div>
    );
  }

  const claudeTab = (
    <Form form={claudeForm} layout="vertical" key="claude-form">
      {currentModelSelect("claude")}
      <Form.Item name="model" label="Default Model" tooltip="ANTHROPIC_MODEL. Virtual model name from LLM Gateway." style={{ maxWidth: 400 }}>
        <Select placeholder="select virtual model" options={modelOpts} allowClear showSearch />
      </Form.Item>
      <Space wrap>
        <Form.Item name="sub_agent_model" label="SubAgent Model" style={{ width: 300 }}>
          <Select placeholder="optional" options={modelOpts} allowClear showSearch />
        </Form.Item>
        <Form.Item name="effort_level" label="Effort Level" style={{ width: 180 }}>
          <Select placeholder="medium" options={EFFORT_OPTS} allowClear />
        </Form.Item>
      </Space>

      <Collapse ghost size="small" items={[{ key: "advanced", label: "Complexity-based auto routing (advanced)", children: (
        <div>
          <Space wrap>
            <Form.Item name="opus_model" label="Opus (complex)" style={{ width: 280 }}>
              <Select placeholder="optional" options={modelOpts} allowClear showSearch />
            </Form.Item>
            <Form.Item name="sonnet_model" label="Sonnet (medium)" style={{ width: 280 }}>
              <Select placeholder="optional" options={modelOpts} allowClear showSearch />
            </Form.Item>
          </Space>
          <Space wrap>
            <Form.Item name="haiku_model" label="Haiku (simple)" style={{ width: 280 }}>
              <Select placeholder="optional" options={modelOpts} allowClear showSearch />
            </Form.Item>
            <Form.Item name="small_fast_model" label="SmallFast (quick)" style={{ width: 280 }}>
              <Select placeholder="optional" options={modelOpts} allowClear showSearch />
            </Form.Item>
          </Space>
        </div>
      )}]} />

      {extraEnvSection(claudeForm)}
      {injectedSection("claude")}
      <Form.Item style={{ marginTop: 12 }}>
        <Button type="primary" onClick={() => saveProfile("claude")} loading={saving === "claude"}>Save Claude Code</Button>
      </Form.Item>
    </Form>
  );

  const codexTab = (
    <Form form={codexForm} layout="vertical" key="codex-form">
      {currentModelSelect("codex")}
      <Form.Item name="model" label="Default Model" style={{ maxWidth: 400 }}>
        <Select placeholder="select virtual model" options={modelOpts} allowClear showSearch />
      </Form.Item>
      <Form.Item name="reasoning_effort" label="Reasoning Effort" style={{ width: 200 }}>
        <Select placeholder="medium" options={REASON_OPTS} allowClear />
      </Form.Item>
      {extraEnvSection(codexForm)}
      {injectedSection("codex")}
      <Form.Item style={{ marginTop: 12 }}>
        <Button type="primary" onClick={() => saveProfile("codex")} loading={saving === "codex"}>Save Codex CLI</Button>
      </Form.Item>
    </Form>
  );

  const geminiTab = (
    <Form form={geminiForm} layout="vertical" key="gemini-form">
      <Text type="secondary" style={{ display: "block", marginBottom: 16 }}>Gemini model is resolved by proxy dynamically. No model config needed.</Text>
      {extraEnvSection(geminiForm)}
      {injectedSection("gemini")}
      <Form.Item style={{ marginTop: 12 }}>
        <Button type="primary" onClick={() => saveProfile("gemini")} loading={saving === "gemini"}>Save Gemini CLI</Button>
      </Form.Item>
    </Form>
  );

  const opencodeTab = (
    <Form form={opencodeForm} layout="vertical" key="opencode-form">
      {currentModelSelect("opencode")}
      <Form.Item name="model" label="Default Model" style={{ maxWidth: 400 }}>
        <Select placeholder="select virtual model" options={modelOpts} allowClear showSearch />
      </Form.Item>
      <Form.Item name="models" label="Available Models" tooltip="Checked models are written to opencode.json for /model switching.">
        {(models.length > 0 ? models : modelList).length === 0
          ? <Text type="secondary">No models in Gateway. Add models first.</Text>
          : <Checkbox.Group><Space direction="vertical">{(models.length > 0 ? models : modelList).map(m => <Checkbox key={m.id} value={m.id} disabled={modelLabel(m).disabled}>{modelLabel(m).label}</Checkbox>)}</Space></Checkbox.Group>}
      </Form.Item>
      {extraEnvSection(opencodeForm)}
      <Form.Item style={{ marginTop: 12 }}>
        <Button type="primary" onClick={() => saveProfile("opencode")} loading={saving === "opencode"}>Save OpenCode</Button>
      </Form.Item>
    </Form>
  );

  return (
    <div>
      <div style={{ marginBottom: 16, padding: "12px 16px", border: "1px solid #f0f0f0", borderRadius: 8 }}>
        <Text strong>Vision 模型</Text>
        <Text type="secondary" style={{ display: "block", fontSize: 12, marginBottom: 8 }}>
          aipmc_vision 图片分析（截图描述 / UI 验证）使用的模型。Agent 调用时显式传 model 参数优先于此处选择；清空则恢复自动（本地优先）。
        </Text>
        <Select
          value={visionModel || undefined}
          placeholder="Auto（本地优先）"
          style={{ width: 320 }}
          size="small"
          loading={saving === "vision"}
          onChange={v => saveVisionModel(v || "")}
          allowClear
          showSearch
          options={visionModelOpts}
        />
        {visionModelOpts.length === 0 && (
          <Text type="secondary" style={{ display: "block", fontSize: 12, marginTop: 4 }}>
            暂无 vision 模型。请先在 models.json 中添加 tags 含 "vision" 的模型（如 qwen3.5-4b-vision）。
          </Text>
        )}
      </div>
      <Tabs activeKey={activeTab} onChange={setActiveTab}
        items={[
          { key: "claude", label: "Claude Code", children: claudeTab },
          { key: "codex",  label: "Codex CLI",  children: codexTab },
          { key: "opencode", label: "OpenCode",    children: opencodeTab },
          { key: "gemini",   label: "Gemini CLI", children: geminiTab },
        ]} />
    </div>
  );
}