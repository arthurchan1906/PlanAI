import { useState } from "react";
import { Button, Modal, Form, Input, Tag, Space, Popconfirm, Select, Typography, Table } from "antd";
import { PlusOutlined, EditOutlined, DeleteOutlined, KeyOutlined, RightOutlined } from "@ant-design/icons";

const { Text } = Typography;

// ── Provider Modal ──

function ProviderModal({ open, provider, onCancel, onSave }) {
  const [form] = Form.useForm();
  return (
    <Modal title={provider ? "Edit Provider" : "Add Provider"} open={open}
      onOk={() => form.submit()} onCancel={() => { form.resetFields(); onCancel(); }} destroyOnClose>
      <Form form={form} layout="vertical" initialValues={provider || {}} onFinish={onSave}>
        <Form.Item name="name" label="Name" rules={[{ required: true }]}>
          <Input placeholder="deepseek" disabled={!!provider} />
        </Form.Item>
        <Form.Item name="openai_url" label="OpenAI URL" rules={[{ required: true }]}>
          <Input placeholder="https://api.deepseek.com" />
        </Form.Item>
        <Form.Item name="anthropic_url" label="Anthropic URL">
          <Input placeholder="https://api.deepseek.com/anthropic" />
        </Form.Item>
      </Form>
    </Modal>
  );
}

// ── Model Modal ──

function ModelModal({ open, model, provider, providers, onCancel, onSave }) {
  const [form] = Form.useForm();
  return (
    <Modal title={model ? "Edit Model" : `Add Model to ${provider}`} open={open}
      onOk={() => form.submit()} onCancel={() => { form.resetFields(); onCancel(); }} destroyOnClose>
      <Form form={form} layout="vertical" initialValues={model || { priority: 0, provider }} onFinish={onSave}>
        <Form.Item name="id" label="Virtual Model ID" rules={[{ required: true }]}>
          <Input placeholder="deepseek-v4-pro" disabled={!!model} />
        </Form.Item>
        <Form.Item name="provider" label="Provider" rules={[{ required: true }]}>
          <Select placeholder="Select provider" disabled={!!provider}>{providers.map(p => <Select.Option key={p.name} value={p.name}>{p.name}</Select.Option>)}</Select>
        </Form.Item>
        <Form.Item name="display_name" label="Display Name"><Input placeholder="DeepSeek V4 Pro" /></Form.Item>
        <Form.Item name="anthropic" label="Anthropic Model Name"><Input placeholder="deepseek-v4-pro[1m]" /></Form.Item>
        <Form.Item name="openai" label="OpenAI Model Name"><Input placeholder="deepseek-v4-pro" /></Form.Item>
        <Form.Item name="tags" label="Tags"><Input placeholder="reasoning, fast" /></Form.Item>
        <Form.Item name="priority" label="Priority"><Input type="number" placeholder="0" /></Form.Item>
      </Form>
    </Modal>
  );
}

// ── Key Modal ──

function KeyModal({ open, provider, unlocked, onCancel, onSave }) {
  const [form] = Form.useForm();
  return (
    <Modal title={`Key: ${provider}`} open={open}
      onOk={() => form.submit()} onCancel={() => { form.resetFields(); onCancel(); }} destroyOnClose>
      <Form form={form} layout="vertical" initialValues={{ key: "" }} onFinish={onSave}>
        <Form.Item name="key" label="API Key" rules={[{ required: true }]}>
          <Input.Password placeholder="sk-xxx" />
        </Form.Item>
        {!unlocked && <Form.Item name="password" label="Master Password" rules={[{ required: true }]}>
          <Input.Password placeholder="credentials password" />
        </Form.Item>}
      </Form>
    </Modal>
  );
}

// ── Main ──

export default function ModelRegistryEditor({ providers = [], models = [], keys = {}, unlocked = false, onKeyChange, onChange }) {
  const [provModal, setProvModal] = useState({ open: false, edit: null });
  const [modelModal, setModelModal] = useState({ open: false, edit: null, provider: "" });
  const [keyModal, setKeyModal] = useState({ open: false, provider: "" });

  function handleProvSave(v) {
    const list = [...providers]; const idx = list.findIndex(p => p.name === v.name);
    if (idx >= 0) list[idx] = { ...list[idx], ...v }; else list.push(v);
    onChange(list, models);
    setProvModal({ open: false, edit: null });
  }

  function handleProvDelete(name) {
    onChange(providers.filter(p => p.name !== name), models.filter(m => m.provider !== name));
  }

  function handleModelSave(raw) {
    const tags = raw.tags ? raw.tags.split(",").map(s => s.trim()).filter(Boolean) : [];
    const m = { ...raw, tags, priority: parseInt(raw.priority, 10) || 0 };
    const list = [...models]; const idx = list.findIndex(x => x.id === m.id);
    if (idx >= 0) list[idx] = { ...list[idx], ...m }; else list.push(m);
    onChange(providers, list);
    setModelModal({ open: false, edit: null, provider: "" });
  }

  function handleModelDelete(id) {
    onChange(providers, models.filter(m => m.id !== id));
  }

  function handleKeySave(v) {
    onKeyChange(keyModal.provider, v.key, v.password || "");
    setKeyModal({ open: false, provider: "" });
  }

  const modelCols = [
    { title: "ID", dataIndex: "id", render: v => <Text code style={{ fontSize: 12 }}>{v}</Text> },
    { title: "Anthropic", dataIndex: "anthropic", width: 200, render: v => v ? <code style={{ fontSize: 11 }}>{v}</code> : "-" },
    { title: "OpenAI", dataIndex: "openai", width: 170, render: v => v ? <code style={{ fontSize: 11 }}>{v}</code> : "-" },
    { title: "Tags", dataIndex: "tags", width: 130, render: ts => ts?.length > 0 ? <Space size={2} wrap>{ts.map(t => <Tag key={t} color="geekblue" style={{ fontSize: 10 }}>{t}</Tag>)}</Space> : "" },
    { title: "", width: 60, render: (_, r) => (
      <Space size={0}>
        <Button type="link" size="small" icon={<EditOutlined />} onClick={() => setModelModal({ open: true, edit: r, provider: r.provider })} />
        <Popconfirm title="Delete?" onConfirm={() => handleModelDelete(r.id)}>
          <Button type="link" size="small" danger icon={<DeleteOutlined />} />
        </Popconfirm>
      </Space>
    )},
  ];

  if (providers.length === 0) {
    return (
      <div>
        <Text type="secondary" style={{ fontSize: 12, display: "block", marginBottom: 8 }}>No providers configured.</Text>
        <Button type="dashed" size="small" icon={<PlusOutlined />} onClick={() => setProvModal({ open: true, edit: null })}>Add Provider</Button>
        <ProviderModal open={provModal.open} provider={provModal.edit}
          onCancel={() => setProvModal({ open: false, edit: null })} onSave={handleProvSave} />
      </div>
    );
  }

  return (
    <div>
      {providers.map(p => {
        const provModels = models.filter(m => m.provider === p.name);
        const hasKey = keys[p.name];
        return (
          <div key={p.name} style={{ marginBottom: 16, border: "1px solid #f0f0f0", borderRadius: 8, overflow: "hidden" }}>
            {/* Provider header */}
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", padding: "8px 12px", background: "#fafafa", borderBottom: provModels.length > 0 ? "1px solid #f0f0f0" : "none" }}>
              <Space size={8}>
                <RightOutlined style={{ fontSize: 10, color: "#bbb" }} />
                <Text strong style={{ fontSize: 13 }}>{p.name}</Text>
                {p.anthropic_url && <Tag color="blue" style={{ fontSize: 10 }}>Anthropic</Tag>}
                {/* Key status */}
                {hasKey
                  ? <Tag color="green" icon={<KeyOutlined />} style={{ cursor: "pointer", fontSize: 11 }}
                    onClick={() => setKeyModal({ open: true, provider: p.name })}>Key set</Tag>
                  : <Button type="link" size="small" icon={<KeyOutlined />} style={{ fontSize: 11, padding: 0 }}
                    onClick={() => setKeyModal({ open: true, provider: p.name })}>Set key</Button>}
              </Space>
              <Space size={4}>
                <Button type="link" size="small" icon={<EditOutlined />} onClick={() => setProvModal({ open: true, edit: p })} />
                <Popconfirm title="Delete provider and its models?" onConfirm={() => handleProvDelete(p.name)}>
                  <Button type="link" size="small" danger icon={<DeleteOutlined />} />
                </Popconfirm>
              </Space>
            </div>

            {/* Models under this provider */}
            {provModels.length > 0 && (
              <Table dataSource={provModels.map((m, i) => ({ ...m, key: m.id || i }))} columns={modelCols}
                size="small" pagination={false} showHeader={false} style={{ margin: 0 }} />
            )}

            {/* Add model for this provider */}
            <div style={{ padding: "4px 12px 6px" }}>
              <Button type="dashed" size="small" icon={<PlusOutlined />}
                onClick={() => setModelModal({ open: true, edit: null, provider: p.name })}>Add model</Button>
            </div>
          </div>
        );
      })}

      <Button type="dashed" size="small" icon={<PlusOutlined />} style={{ marginTop: 4 }}
        onClick={() => setProvModal({ open: true, edit: null })}>Add Provider</Button>

      <ProviderModal open={provModal.open} provider={provModal.edit}
        onCancel={() => setProvModal({ open: false, edit: null })} onSave={handleProvSave} />
      <ModelModal open={modelModal.open} model={modelModal.edit} provider={modelModal.provider} providers={providers}
        onCancel={() => setModelModal({ open: false, edit: null, provider: "" })} onSave={handleModelSave} />
      <KeyModal open={keyModal.open} provider={keyModal.provider} unlocked={unlocked}
        onCancel={() => setKeyModal({ open: false, provider: "" })} onSave={handleKeySave} />
    </div>
  );
}
