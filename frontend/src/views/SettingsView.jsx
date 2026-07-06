import { useState, useEffect, useCallback, useRef } from "react";
import { Button, Card, Form, Input, Select, Tag, Typography, message, Collapse, Space, Tooltip, Row, Col, Divider, Modal, Dropdown } from "antd";
import { PauseCircleOutlined, PlayCircleOutlined, CodeOutlined, RobotOutlined, ThunderboltOutlined, ApiOutlined, CloudServerOutlined, SettingOutlined, KeyOutlined, LockOutlined, UnlockOutlined, CopyOutlined } from "@ant-design/icons";
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
  const [aiForm] = Form.useForm();
  const [proxyForm] = Form.useForm();
  const [keyForm] = Form.useForm();
  async function load() {
    setLoading(true);
    try {
      const [cfg, cred] = await Promise.all([
        api("/pmai/config"),
        api("/pmai/credentials").catch(() => ({})),
      ]);
      aiForm.setFieldsValue(cfg); proxyForm.setFieldsValue(cfg);
      setAiStatus(cfg.ai_enabled ? "enabled" : "disabled");
      // Fetch per-agent current models
      setProviders(cfg.providers || []);
      setModels(cfg.models || []);
      setKeys(cred.keys || {});
      setKeyUn(cred.unlocked || false);
      setKeyExists(cred.exists || false);
    } catch (e) { /* */ }
    setLoading(false);
  }
  async function handleKeyChange(provider, key, password) {
    await api("/pmai/credentials", {
      method: "POST",
      body: JSON.stringify({ action: "set", provider, key, password }),
    });
    setKeys(prev => ({ ...prev, [provider]: key.startsWith("sk-") ? key.slice(0, 6) + "..." + key.slice(-4) : "***" }));
    if (password) { setKeyUn(true); setKeyExists(true); }
    message.success(`Key for ${provider} saved`);
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
  useEffect(() => { load(); }, []);
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
    const body = { action: keyModal.type, ...v };
    if (keyModal.type === "passwd") { body.old_password = v.oldPassword; body.new_password = v.newPassword; }
    try {
      const res = await api("/pmai/credentials", { method: "POST", body: JSON.stringify(body) });
      message.success(keyModal.type === "unlock" ? `Unlocked — ${res.unlocked} keys` : "Done");
      load(); setKeyModal({ open: false, type: "" }); keyForm.resetFields();
      if (keyModal.type === "unlock") toggleProxy("restart", true);
    } catch (e) { message.error(e.message); }
  }
  function openKeyModal(type) { keyForm.resetFields(); setKeyModal({ open: true, type }); }
  // ── Section: LLM 中转代理 ──────────────────────────────────────
  const proxySection = (
    <div style={{ marginBottom: 24 }}>
      <Title level={5} style={{ marginBottom: 12 }}><CloudServerOutlined /> LLM 中转代理</Title>
      <Card size="small" title="LLM Gateway" extra={<Text type="secondary" style={{ fontSize: 11 }}>models.json</Text>}>
        <ModelRegistryEditor key={providers.length + "-" + models.length}
          providers={providers} models={models}
          keys={apiKeys} unlocked={keyUnlocked}
          onKeyChange={handleKeyChange} onChange={saveRegistry} />
        {!keyExists && <div style={{ marginTop: 12, borderTop: "1px solid #f0f0f0", paddingTop: 8 }}>
          <Button type="link" size="small" onClick={() => openKeyModal("init")}>Init credentials</Button>
        </div>}
        {keyExists && keyUnlocked && <div style={{ marginTop: 12, borderTop: "1px solid #f0f0f0", paddingTop: 8 }}>
          <Space size={8}>
            <Button type="link" size="small" onClick={() => openKeyModal("passwd")}>Change password</Button>
            <Button type="link" size="small" danger onClick={() => openKeyModal("lock")}>Lock keys</Button>
          </Space>
        </div>}
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
        <AgentConfigView key={agentKey} models={models} />
      </Card>
    </div>
  );
  // ── Section: AIPM 自身 AI ─────────────────────────────────────
  const aiSection = (
    <div>
      <Divider style={{ margin: "8px 0 16px" }} />
      <Title level={5} style={{ marginBottom: 4 }}><SettingOutlined /> AIPM AI 配置</Title>
      <Text type="secondary" style={{ fontSize: 12, display: "block", marginBottom: 12 }}>
        AIPM 本身使用的 AI（项目管理、讨论分析、embedding 等），与中转代理无关。
      </Text>
      <Card size="small">
        <Form form={aiForm} layout="vertical" onFinish={v => save("AI", v)}>
          <Row gutter={16}>
            <Col span={12}><Form.Item name="ai_endpoint" label="Chat API Endpoint"><Input placeholder="https://api.openai.com/v1" /></Form.Item></Col>
            <Col span={12}><Form.Item name="ai_embedding_endpoint" label="Embedding Endpoint"><Input placeholder="uses chat endpoint if empty" /></Form.Item></Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}><Form.Item name="ai_chat_model" label="Chat Model"><Input placeholder="gpt-4o-mini" /></Form.Item></Col>
            <Col span={12}><Form.Item name="ai_model" label="Embedding Model"><Input placeholder="text-embedding-3-small" /></Form.Item></Col>
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
