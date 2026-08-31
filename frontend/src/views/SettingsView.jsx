import { useState, useEffect, useCallback, useRef } from "react";
import { Button, Card, Form, Input, Select, Tag, Typography, message, Collapse, Space, Tooltip, Row, Col, Divider, Modal, Dropdown } from "antd";
import { BookOutlined, PauseCircleOutlined, PlayCircleOutlined, CodeOutlined, RobotOutlined, ThunderboltOutlined, ApiOutlined, CloudServerOutlined, SettingOutlined, KeyOutlined, LockOutlined, UnlockOutlined, CopyOutlined } from "@ant-design/icons";
import { api } from "../utils/api";
import AgentConfigView from "./AgentConfigView";
import ModelRegistryEditor from "../components/ModelRegistryEditor";
const { Title, Text, Paragraph } = Typography;
export default function SettingsView() {
  const [loading, setLoading] = useState(false);
  const [testing, setTesting] = useState(false);
  const [aiStatus, setAiStatus] = useState(null);
  const [proxyStatus, setProxySt] = useState(null);
  const [launching, setLaunching] = useState(null);
  const [agentKey, setAgentKey] = useState(0);
  const [providers, setProviders] = useState([]);
  const [models, setModels] = useState([]);
  const [apiKeys, setKeys] = useState({});
  const [keyUnlocked, setKeyUn] = useState(false);
  const [keyExists, setKeyExists] = useState(false);
  const [keyModal, setKeyModal] = useState({ open: false, type: "" });
  const [profile, setProfile] = useState(() => sessionStorage.getItem("aipmc_profile") || "default");
  const [profileList, setProfileList] = useState([]);
  const [aiForm] = Form.useForm();
  const [proxyForm] = Form.useForm();
  const [keyForm] = Form.useForm();
  const [guidelines, setGuidelines] = useState("");
  const [guidelinesLoading, setGuidelinesLoading] = useState(false);
  const [guidelinesSaving, setGuidelinesSaving] = useState(false);
  async function load() {
    setLoading(true);
    try {
      const [cfg, cred] = await Promise.all([
        api("/pmai/config"),
        api(`/pmai/credentials?profile=${encodeURIComponent(profile)}`).catch(() => ({})),
      ]);
      aiForm.setFieldsValue(cfg); proxyForm.setFieldsValue(cfg);
      setAiStatus(cfg.ai_enabled ? "enabled" : "disabled");
      // Fetch per-agent current models
      setProviders(cfg.providers || []);
      setModels(cfg.models || []);
      setKeys(cred.keys || {});
      setKeyUn(cred.unlocked || false);
      setKeyExists(cred.exists || false);
      setProfileList(cfg.all_profiles || []);
    } catch (e) { /* */ }
    setLoading(false);
  }
  async function handleKeyChange(provider, key, password) {
    try {
      await api("/pmai/credentials", {
        method: "POST",
        body: JSON.stringify({ action: "set", provider, key, password, profile }),
      });
      setKeys(prev => ({ ...prev, [provider]: key.startsWith("sk-") ? key.slice(0, 6) + "..." + key.slice(-4) : "***" }));
      if (password) { setKeyUn(true); setKeyExists(true); }
      message.success(`Key for ${provider} saved`);
      return true;
    } catch (e) {
      message.error(`Key for ${provider} failed: ${e.message}`);
      return false;
    }
  }
  async function saveRegistry(provs, mods) {
    setProviders(provs); setModels(mods);
    const body = { providers: provs };
    if (mods.length > 0) body.models = mods;
    try {
      await api("/pmai/config", { method: "POST", body: JSON.stringify(body) });
      message.success("Gateway config saved");
    } catch (e) { message.error(e.message); }
  }
  const checkProxy = useCallback(async () => {
    try { const d = await api("/pmai/proxy-status"); setProxySt(d?.running ? d : null); }
    catch { setProxySt(null); }
  }, []);
  useEffect(() => { load(); loadGuidelinesData(); }, []);

  async function loadGuidelinesData() {
    setGuidelinesLoading(true);
    try {
      const data = await api("/pmai/web/guidelines");
      setGuidelines(data.content || "");
    } catch (_) { setGuidelines(""); }
    setGuidelinesLoading(false);
  }

  async function saveGuidelines() {
    setGuidelinesSaving(true);
    try {
      await api("/pmai/web/guidelines", { method: "POST", body: JSON.stringify({ content: guidelines }) });
      message.success("Guidelines saved");
    } catch (e) { message.error(e.message); }
    setGuidelinesSaving(false);
  }
  const proxyRef = useRef(proxyStatus);
  proxyRef.current = proxyStatus;
  useEffect(() => {
    checkProxy(); // first check immediately
    const t = setInterval(() => { if (proxyRef.current) checkProxy(); }, 10000);
    return () => clearInterval(t);
  }, [checkProxy]);
  async function save(section, values) {
    try {
      await api("/pmai/config", { method: "POST", body: JSON.stringify(values) });
      message.success(`${section} saved`); load(); setAgentKey(k => k + 1); setTimeout(checkProxy, 500);
    } catch (e) { message.error(e.message); }
  }
  async function toggleProxy(action, skipCredCheck) {
    if (!skipCredCheck && action === "restart" && keyExists && !keyUnlocked) {
      openKeyModal("unlock"); return;
    }
    try {
      const d = await api(`/pmai/proxy/${action}`, { method: "POST", body: "{}" });
      if (d.ok) { message.success(`Proxy ${action === "stop" ? "stopped" : "restarted"}`); if (action === "stop") setProxySt(null); else setTimeout(checkProxy, 500); }
      else message.error(d.error);
    } catch (e) { message.error(e.message); }
  }
  async function launchAgent(name) {
    setLaunching(name);
    try {
      const d = await api("/pmai/agent/launch", { method: "POST", body: JSON.stringify({ agent: name }) });
      if (d.ok) message.success(`${name} launched`); else message.error(d.error);
    } catch (e) { message.error(e.message); }
    setLaunching(null);
  }
  async function copyAgentCmd(name, fmt) {
    try {
      const d = await api(`/pmai/web/agent/cmd?agent=${name}`);
      if (d.error) { message.error(d.error); return; }
      const cmd = (fmt === "win") ? d.win : d.unix;
      const label = (fmt === "win") ? "CMD" : "Terminal / Git Bash";
      try {
        await navigator.clipboard.writeText(cmd);
      } catch (_) {
        const ta = document.createElement("textarea");
        ta.value = cmd; ta.style.position = "fixed"; ta.style.opacity = "0";
        document.body.appendChild(ta); ta.select();
        document.execCommand("copy"); document.body.removeChild(ta);
      }
      message.success(`已复制 ${name} 命令 (${label})`);
    } catch (e) { message.error(e.message); }
  }
  async function testAI() {
    setTesting(true);
    const d = await api("/pmai/ai-test", { method: "POST" });
    if (d.ok) { message.success(d.message); setAiStatus("enabled"); } else { message.error(d.error); setAiStatus("error"); }
    setTesting(false);
  }
  async function handleKeySession(v) {
    const body = { action: keyModal.type, profile, ...v };
    if (keyModal.type === "passwd") { body.old_password = v.oldPassword; body.new_password = v.newPassword; }
    try {
      const res = await api("/pmai/credentials", { method: "POST", body: JSON.stringify(body) });
      message.success(keyModal.type === "unlock" ? `Unlocked — ${res.unlocked} keys` : "Done");
      load(); setKeyModal({ open: false, type: "" }); keyForm.resetFields();
      if (keyModal.type === "unlock") toggleProxy("restart", true);
    } catch (e) { message.error(e.message); }
  }
  function openKeyModal(type) { keyForm.resetFields(); setKeyModal({ open: true, type }); }
  async function switchProfile(name) {
    setProfile(name); sessionStorage.setItem("aipmc_profile", name); setKeys({}); setKeyUn(false);
    // Reload credentials for new profile
    try {
      const cred = await api(`/pmai/credentials?profile=${encodeURIComponent(name)}`).catch(() => ({}));
      setKeys(cred.keys || {});
      setKeyUn(cred.unlocked || false);
      setKeyExists(cred.exists || false);
    } catch (_) {}
  }
  async function createProfile(name, password) {
    try {
      await api("/pmai/credentials", { method: "POST", body: JSON.stringify({ action: "create-profile", profile: name, password }) });
      message.success(`Profile "${name}" created`);
      load(); setProfile(name); sessionStorage.setItem("aipmc_profile", name);
    } catch (e) { message.error(e.message); }
  }
  async function deleteProfile(name) {
    try {
      await api("/pmai/credentials", { method: "POST", body: JSON.stringify({ action: "delete-profile", profile: name }) });
      message.success(`Profile "${name}" deleted`);
      if (name === profile) { setProfile("default"); sessionStorage.removeItem("aipmc_profile"); setKeys({}); setKeyUn(false); }
      load();
    } catch (e) { message.error(e.message); }
  }
  // ?? Section: LLM ???? ?????????????????????????????????
  const proxySection = (
    <div style={{ marginBottom: 24 }}>
      <Title level={5} style={{ marginBottom: 12 }}><CloudServerOutlined /> LLM KEY Profile</Title>

      {/* Profile Selector */}
      <Card size="small" style={{ marginBottom: 12 }}>
        <Row align="middle" gutter={16}>
          <Col flex="auto">
            <Space size={8} align="center">
              <Text strong style={{ fontSize: 13 }}><KeyOutlined /> Credentials Profile</Text>
              <Select
                value={profile}
                onChange={switchProfile}
                style={{ width: 160 }}
                size="small"
                dropdownRender={menu => (
                  <>
                    {menu}
                    <Divider style={{ margin: "4px 0" }} />
                    <Space style={{ padding: "4px 8px" }}>
                      <Button type="text" size="small" icon={<KeyOutlined />}
                        onClick={() => {
                          const name = prompt("New profile name:");
                          if (!name) return;
                          const pw = prompt("Set master password for this profile:");
                          if (!pw) return;
                          createProfile(name, pw);
                        }}
                      >New Profile</Button>
                    </Space>
                  </>
                )}
              >
                {(profileList.length > 0 ? profileList : ["default"]).map(p => (
                  <Select.Option key={p} value={p}>{p}</Select.Option>
                ))}
              </Select>
              {!keyExists && (
                <Button type="link" size="small" onClick={() => openKeyModal("init")}>
                  Init this profile
                </Button>
              )}
              {keyExists && !keyUnlocked && (
                <Button type="link" size="small" onClick={() => openKeyModal("unlock")}>
                  <UnlockOutlined /> Unlock
                </Button>
              )}
              {keyExists && keyUnlocked && (
                <Space size={4}>
                  <Tag color="green" style={{ margin: 0 }}>unlocked</Tag>
                  <Button type="link" size="small" onClick={() => openKeyModal("passwd")}>passwd</Button>
                  <Button type="link" size="small" danger onClick={() => openKeyModal("lock")}>lock</Button>
                  {profile !== "default" && (
                    <Button type="link" size="small" danger
                      onClick={() => { if (confirm(`Delete profile "${profile}"?`)) deleteProfile(profile); }}
                    >delete</Button>
                  )}
                </Space>
              )}
            </Space>
          </Col>
        </Row>
      </Card>

      <Card size="small" title="LLM Gateway" extra={<Text type="secondary" style={{ fontSize: 11 }}>models.json</Text>}>
        <ModelRegistryEditor key={providers.length + "-" + models.length}
          providers={providers} models={models}
          keys={apiKeys} unlocked={keyUnlocked}
          onKeyChange={handleKeyChange} onChange={saveRegistry} />
      </Card>

      <Card size="small" title={<Space><span style={{ width: 8, height: 8, borderRadius: "50%", background: proxyStatus ? "#52c41a" : "#d9d9d9", display: "inline-block" }} /><Text>{proxyStatus ? <>Proxy · running <Text type="secondary" style={{ fontSize: 11, fontWeight: "normal" }}>:{proxyStatus.port} · {proxyStatus.uptime} · {proxyStatus.requests || 0} req</Text></> : "Proxy · stopped"}</Text></Space>} extra={<Text type="secondary" style={{ fontSize: 11 }}>config.json</Text>}
        style={{ marginTop: 12 }}>
        <Form form={proxyForm} layout="inline" onFinish={v => save("Proxy", v)} style={{ flexWrap: "nowrap" }}>
          <Form.Item name="proxy_port" label={<span>Port <Tooltip title="restart required">⚠</Tooltip></span>} style={{ minWidth: 100 }}>
            <Input placeholder="19530" style={{ width: 80 }} />
          </Form.Item>
          <Form.Item name="proxy_log_dir" label="Log Dir" style={{ flex: "1 1 0", minWidth: 200 }}>
            <Input placeholder="/tmp/aipmc-traces" />
          </Form.Item>
          <Form.Item>
            <Space size={8}>
              <Button type="primary" htmlType="submit" loading={loading} size="small">Save</Button>
              {proxyStatus
                ? <Button size="small" danger onClick={() => toggleProxy("stop")}>Stop</Button>
                : <Button size="small" type="primary" onClick={() => toggleProxy("restart")}>Start</Button>}
            </Space>
          </Form.Item>
        </Form>
      </Card>
      <Card size="small" title="Agents" style={{ marginTop: 12 }}>
        <Text strong style={{ display: "block", marginBottom: 8, fontSize: 13 }}>Launch Agent</Text>
        <Space wrap style={{ marginBottom: 16 }}>
          {[{ k: "claude", icon: <RobotOutlined />, label: "Claude Code" },
          { k: "codex", icon: <CodeOutlined />, label: "Codex CLI" },
          { k: "opencode", icon: <ApiOutlined />, label: "OpenCode" },
          { k: "gemini", icon: <ThunderboltOutlined />, label: "Gemini CLI" }].map(a => (
            <Button.Group key={a.k}>
              <Button icon={a.icon} loading={launching === a.k} onClick={() => launchAgent(a.k)}>{a.label}</Button>
              <Dropdown menu={{ items: [
                { key: "unix", label: "复制命令 (Terminal / Git Bash)", onClick: () => copyAgentCmd(a.k, "unix") },
                { key: "win",  label: "复制命令 (CMD)", onClick: () => copyAgentCmd(a.k, "win") },
              ]}} trigger={["click"]}>
                <Tooltip title="复制启动命令">
                  <Button icon={<CopyOutlined />} />
                </Tooltip>
              </Dropdown>
            </Button.Group>
          ))}
        </Space>
        <AgentConfigView key={agentKey} models={models} keys={apiKeys} />
      </Card>
    </div>
  );
  // ── Section: AIPM 自身 AI ─────────────────────────────────────
  // Build model options from LLM Gateway, ensuring current values are always selectable
  const gatewayModelOpts = (models || []).map(m => {
    const firstProvider = (m.routes && m.routes[0]) ? m.routes[0].provider : "";
    const label = m.display_name ? `${m.display_name} (${m.id})` : m.id;
    const sub = firstProvider ? ` — ${firstProvider}` : "";
    return { value: m.id, label: label + sub, provider: firstProvider };
  });
  function modelOptsWithCurrent(currentVal) {
    if (currentVal && !gatewayModelOpts.some(o => o.value === currentVal)) {
      return [{ value: currentVal, label: `${currentVal} (当前)` }, ...gatewayModelOpts];
    }
    return gatewayModelOpts;
  }
  // Auto-fill endpoint from a selected model's OpenAI-compatible provider URL, then auto-save.
  // AIPM 自身 AI 只走 OpenAI-compatible 协议：模型必须有 openai_url 的 provider 才能用。
  // 只取 routes[0] 且缺 openai_url 时静默沿用旧 endpoint，会把云端模型名发到旧地址（如本地 8080）。
  function applyModelChange(modelId, endpointField) {
    if (!modelId) return;
    const model = (models || []).find(m => m.id === modelId);
    let endpoint = "";
    if (model && model.routes && model.routes.length > 0) {
      for (const route of model.routes) {
        const provider = (providers || []).find(p => p.name === route.provider);
        if (provider && provider.openai_url) { endpoint = provider.openai_url; break; }
      }
    }
    if (!endpoint) {
      message.warning(`模型 ${modelId} 无 OpenAI-compatible endpoint，AIPM AI 无法使用，已取消选择`);
      // 清空模型选择并中止自动保存，避免旧 endpoint + 新模型名发到错误服务器
      const field = endpointField === "ai_endpoint" ? "ai_chat_model" : "ai_model";
      aiForm.setFieldsValue({ [field]: undefined });
      return;
    }
    aiForm.setFieldsValue({ [endpointField]: endpoint });
    // 动态更新：选完模型立刻保存生效，无需手动点 Save
    const values = aiForm.getFieldsValue();
    api("/pmai/config", { method: "POST", body: JSON.stringify(values) })
      .then(d => {
        if (d.ok) {
          message.success(`AI 模型已切换为 ${modelId}`);
          setAiStatus(d.ai_enabled ? "enabled" : "disabled");
          setAgentKey(k => k + 1);
          setTimeout(checkProxy, 500);
        }
      })
      .catch(e => message.error(e.message));
  }
  const aiSection = (
    <div>
      <Divider style={{ margin: "8px 0 16px" }} />
      <Title level={5} style={{ marginBottom: 4 }}><SettingOutlined /> AIPM AI 配置</Title>
      <Text type="secondary" style={{ fontSize: 12, display: "block", marginBottom: 12 }}>
        AIPM 本身使用的 AI（项目管理、讨论分析、embedding 等）。模型和 Endpoint 均从 LLM Gateway 自动获取。
        {models.length === 0 && (
          <Tag color="warning" style={{ marginLeft: 8, fontSize: 11 }}>LLM Gateway 未配置模型，请先在上方添加</Tag>
        )}
      </Text>
      <Card size="small">
        <Form form={aiForm} layout="vertical" onFinish={v => save("AI", v)}>
          {/* Endpoint 由选中模型自动推断，隐藏字段仅用于提交 */}
          <Form.Item name="ai_endpoint" hidden><Input /></Form.Item>
          <Form.Item name="ai_embedding_endpoint" hidden><Input /></Form.Item>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="ai_chat_model" label="Chat Model" tooltip="从 LLM Gateway 选择，Endpoint 自动从对应 Provider 获取">
                <Select
                  placeholder="选择或搜索 Gateway 模型…"
                  showSearch
                  allowClear
                  options={modelOptsWithCurrent(aiForm.getFieldValue("ai_chat_model"))}
                  filterOption={(input, option) =>
                    (option?.label ?? '').toLowerCase().includes(input.toLowerCase())
                  }
                  notFoundContent={
                    <span style={{ fontSize: 12, color: '#999', padding: '4px 8px', display: 'block' }}>
                      LLM Gateway 中无匹配模型，请先在上方添加
                    </span>
                  }
                  onChange={val => { if (val) applyModelChange(val, "ai_endpoint"); }}
                />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="ai_model" label="Embedding Model" tooltip="从 LLM Gateway 选择，Endpoint 自动从对应 Provider 获取">
                <Select
                  placeholder="选择或搜索 Gateway 模型…"
                  showSearch
                  allowClear
                  options={modelOptsWithCurrent(aiForm.getFieldValue("ai_model"))}
                  filterOption={(input, option) =>
                    (option?.label ?? '').toLowerCase().includes(input.toLowerCase())
                  }
                  notFoundContent={
                    <span style={{ fontSize: 12, color: '#999', padding: '4px 8px', display: 'block' }}>
                      LLM Gateway 中无匹配模型，请先在上方添加
                    </span>
                  }
                  onChange={val => { if (val) applyModelChange(val, "ai_embedding_endpoint"); }}
                />
              </Form.Item>
            </Col>
          </Row>
          <Space>
            <Button type="primary" htmlType="submit" loading={loading} size="small">Save</Button>
            <Button size="small" onClick={testAI} loading={testing}>Test</Button>
          </Space>
        </Form>
        {aiStatus && <div style={{ marginTop: 8 }}>{aiStatus === "enabled" ? <Tag color="green">Connected</Tag> : <Tag color="red">Failed</Tag>}</div>}
      </Card>
    </div>
  );
  return (
    <div>
      {proxySection}
      {aiSection}
      <Divider style={{ margin: "8px 0 16px" }} />
      <Card size="small" title={<Space><BookOutlined /> Guidelines (.pmai/guidelines.md)</Space>}
        extra={<Text type="secondary" style={{fontSize:11}}>注入到 Agent system prompt 的项目编码规范</Text>}>
        <Input.TextArea
          value={guidelines}
          onChange={e => setGuidelines(e.target.value)}
          rows={8}
          placeholder={"# Project coding guidelines\n\n* Use consistent naming\n* Add logs at data pipeline boundaries\n* ..."}
          style={{ fontFamily: "monospace", fontSize: 13 }}
        />
        <div style={{ marginTop: 8, display: "flex", justifyContent: "flex-end", gap: 8 }}>
          <Button size="small" onClick={loadGuidelinesData} loading={guidelinesLoading}>Reload</Button>
          <Button type="primary" size="small" onClick={saveGuidelines} loading={guidelinesSaving}>Save</Button>
        </div>
      </Card>

      <Modal title={{
        init: "Initialize Encryption",
        unlock: "Master Password",
        lock: "Lock",
        passwd: "Change Password",
      }[keyModal.type]}
        open={keyModal.open}
        onOk={() => keyForm.submit()}
        onCancel={() => { setKeyModal({ open: false, type: "" }); keyForm.resetFields(); }}
        destroyOnClose>
        <Form form={keyForm} layout="vertical" onFinish={handleKeySession}>
          {keyModal.type === "init" && <>
            <Form.Item name="password" label="Master Password" rules={[{ required: true }]}><Input.Password /></Form.Item>
            <Form.Item name="confirm" label="Confirm" rules={[{ required: true }]}><Input.Password /></Form.Item>
          </>}
          {keyModal.type === "unlock" && (
            <Form.Item name="password" label="Password" rules={[{ required: true }]}><Input.Password placeholder="enter password to unlock" /></Form.Item>
          )}
          {keyModal.type === "lock" && (
            <Paragraph>Lock the credential store? All keys will be cleared from memory.</Paragraph>
          )}
          {keyModal.type === "passwd" && <>
            <Form.Item name="oldPassword" label="Old Password" rules={[{ required: true }]}><Input.Password /></Form.Item>
            <Form.Item name="newPassword" label="New Password" rules={[{ required: true }]}><Input.Password /></Form.Item>
            <Form.Item name="confirm" label="Confirm" rules={[{ required: true }]}><Input.Password /></Form.Item>
          </>}
        </Form>
      </Modal>
    </div>
  );
}
