import { useState } from "react";
import { Button, Modal, Form, Input, Tag, Space, Popconfirm, Select, Typography, Table } from "antd";
import { PlusOutlined, EditOutlined, DeleteOutlined, KeyOutlined } from "@ant-design/icons";

const { Text } = Typography;

// ?? Provider Modal ??????????????????????????????????????????????????????????

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
          <Input placeholder="https://api.deepseek.com/v1" />
        </Form.Item>
        <Form.Item name="anthropic_url" label="Anthropic URL">
          <Input placeholder="https://api.deepseek.com/anthropic" />
        </Form.Item>
      </Form>
    </Modal>
  );
}

// ?? Model Modal ? Routes editor ?????????????????????????????????????????????

function ModelModal({ open, model, providers, onCancel, onSave }) {
  const [form] = Form.useForm();
  const isEdit = !!model;

  return (
    <Modal title={isEdit ? "Edit Model" : "Add Model"} open={open} width={640}
      onOk={() => form.submit()} onCancel={() => { form.resetFields(); onCancel(); }} destroyOnClose>
      <Form form={form} layout="vertical"
        initialValues={model ? {
          id: model.id,
          display_name: model.display_name || "",
          tags: (model.tags || []).join(", "),
          priority: model.priority || 0,
          routes: (model.routes || []).map((rt, i) => ({ ...rt, key: i })),
        } : { priority: 0, routes: [{ key: 0, provider: "", model_openai: "", model_anthropic: "" }] }}
        onFinish={(raw) => {
          const tags = raw.tags ? raw.tags.split(",").map(s => s.trim()).filter(Boolean) : [];
          const routes = (raw.routes || [])
            .filter(r => r.provider)
            .map(r => ({
              provider: r.provider,
              model_openai: r.model_openai || "",
              model_anthropic: r.model_anthropic || "",
            }));
          if (routes.length === 0) return; // require at least one route
          onSave({
            id: raw.id,
            display_name: raw.display_name || "",
            routes,
            tags,
            priority: parseInt(raw.priority, 10) || 0,
          });
        }}>
        <Form.Item name="id" label="Virtual Model ID" rules={[{ required: true }]}>
          <Input placeholder="deepseek-chat" disabled={isEdit} />
        </Form.Item>
        <Form.Item name="display_name" label="Display Name">
          <Input placeholder="DeepSeek Chat" />
        </Form.Item>
        <Form.Item name="tags" label="Tags">
          <Input placeholder="fast, reasoning" />
        </Form.Item>
        <Form.Item name="priority" label="Priority">
          <Input type="number" placeholder="0" />
        </Form.Item>

        {/* Routes editor */}
        <Form.List name="routes">
          {(fields, { add, remove }) => (
            <>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 4 }}>
                <Text strong style={{ fontSize: 12 }}>Provider Routes (priority order)</Text>
                <Button type="dashed" size="small" icon={<PlusOutlined />}
                  onClick={() => add({ provider: "", model_openai: "", model_anthropic: "" })}>Add route</Button>
              </div>
              {fields.map(({ key, name, ...rest }) => (
                <div key={key} style={{
                  border: "1px solid #f0f0f0", borderRadius: 6, padding: "8px 10px",
                  marginBottom: 6, background: "#fafafa", position: "relative"
                }}>
                  <Space size={4} style={{ position: "absolute", top: 6, right: 8 }}>
                    {fields.length > 1 && (
                      <Button type="link" size="small" danger onClick={() => remove(name)}
                        style={{ padding: 0, height: 20 }}>?</Button>
                    )}
                  </Space>
                  <Space size={6} wrap align="start">
                    <Form.Item {...rest} name={[name, "provider"]} rules={[{ required: true, message: "Provider" }]}
                      style={{ marginBottom: 4, width: 130 }}>
                      <Select placeholder="Provider" size="small" style={{ fontSize: 12 }}>
                        {providers.map(p => <Select.Option key={p.name} value={p.name}>{p.name}</Select.Option>)}
                      </Select>
                    </Form.Item>
                    <Form.Item {...rest} name={[name, "model_openai"]} style={{ marginBottom: 4, width: 170 }}>
                      <Input placeholder="OpenAI model name" size="small" style={{ fontSize: 12 }} />
                    </Form.Item>
                    <Form.Item {...rest} name={[name, "model_anthropic"]} style={{ marginBottom: 4, width: 170 }}>
                      <Input placeholder="Anthropic model name" size="small" style={{ fontSize: 12 }} />
                    </Form.Item>
                  </Space>
                </div>
              ))}
            </>
          )}
        </Form.List>
      </Form>
    </Modal>
  );
}

// ?? Key Modal ????????????????????????????????????????????????????????????????

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

// ?? Main ?????????????????????????????????????????????????????????????????????

export default function ModelRegistryEditor({ providers = [], models = [], keys = {}, unlocked = false, onKeyChange, onChange }) {
  const [provModal, setProvModal] = useState({ open: false, edit: null });
  const [modelModal, setModelModal] = useState({ open: false, edit: null });
  const [keyModal, setKeyModal] = useState({ open: false, provider: "" });

  function handleProvSave(v) {
    const list = [...providers]; const idx = list.findIndex(p => p.name === v.name);
    if (idx >= 0) list[idx] = { ...list[idx], ...v }; else list.push(v);
    onChange(list, models);
    setProvModal({ open: false, edit: null });
  }

  function handleProvDelete(name) {
    // Remove provider and any models that ONLY have routes to this provider
    const keepModels = models.filter(m => {
      const routes = m.routes || [];
      if (routes.length === 0) return false;
      return routes.some(rt => rt.provider !== name);
    });
    onChange(providers.filter(p => p.name !== name), keepModels);
  }

  function handleModelSave(raw) {
    // raw.routes is already an array from the Form.List
    let routes = raw.routes || [];
    // Backward compat: if sent as flat old format
    if (routes.length === 0 && raw.provider) {
      routes = [{ provider: raw.provider, model_openai: raw.openai || "", model_anthropic: raw.anthropic || "" }];
    }
    const m = {
      id: raw.id,
      display_name: raw.display_name || "",
      routes,
      tags: raw.tags || [],
      priority: raw.priority || 0,
    };
    const list = [...models]; const idx = list.findIndex(x => x.id === m.id);
    if (idx >= 0) list[idx] = { ...list[idx], ...m }; else list.push(m);
    onChange(providers, list);
    setModelModal({ open: false, edit: null });
  }

  function handleModelDelete(id) {
    onChange(providers, models.filter(m => m.id !== id));
  }

  function handleKeySave(v) {
    onKeyChange(keyModal.provider, v.key, v.password || "");
    setKeyModal({ open: false, provider: "" });
  }

  // Build model table data ? color routes by key availability.
  const modelData = models.map((m, i) => {
    const routes = m.routes || [];
    // Find first route whose provider has a key (same logic as proxy Resolve).
    const hitProvider = routes.find(rt => keys[rt.provider]);
    const anyAvailable = !!hitProvider;

    const routeTags = routes.map((rt, j) => {
      const hasKey = !!keys[rt.provider];
      return (
        <Tag key={j}
          color={hasKey ? "green" : "default"}
          style={{ fontSize: 10, margin: 1, opacity: hasKey ? 1 : 0.5 }}>
          {rt.provider}
        </Tag>
      );
    });

    const protoInfo = routes.map((rt, j) => {
      const parts = [];
      if (rt.model_openai) parts.push(`O:${rt.model_openai}`);
      if (rt.model_anthropic) parts.push(`A:${rt.model_anthropic}`);
      return parts.length > 0 ? (
        <div key={j} style={{ fontSize: 10, color: "#888", lineHeight: "14px" }}>
          {rt.provider}: {parts.join(" ")}
        </div>
      ) : null;
    }).filter(Boolean);

    return { ...m, key: m.id || i, routeTagsArr: routeTags, protoInfoArr: protoInfo, _available: anyAvailable };
  });

  const modelCols = [
    { title: "ID", dataIndex: "id", width: 160,
      render: (v, r) => (
        <div>
          <Text code style={{ fontSize: 12 }}>{v}</Text>
          {r.display_name && <div style={{ fontSize: 11, color: "#666" }}>{r.display_name}</div>}
        </div>
      ) },
    { title: "Routes", dataIndex: "routeTagsArr", width: 150, render: tags => <Space size={2} wrap>{tags}</Space> },
    { title: "Protocol Names", dataIndex: "protoInfoArr", render: info => <div style={{ lineHeight: "16px" }}>{info}</div> },
    { title: "Tags", dataIndex: "tags", width: 140, render: ts => ts?.length > 0
      ? <Space size={2} wrap>{ts.map(t => <Tag key={t} color="geekblue" style={{ fontSize: 10 }}>{t}</Tag>)}</Space>
      : "" },
    { title: "", width: 60, render: (_, r) => (
      <Space size={0}>
        <Button type="link" size="small" icon={<EditOutlined />}
          onClick={() => setModelModal({ open: true, edit: r })} />
        <Popconfirm title="Delete?" onConfirm={() => handleModelDelete(r.id)}>
          <Button type="link" size="small" danger icon={<DeleteOutlined />} />
        </Popconfirm>
      </Space>
    )},
  ];

  if (providers.length === 0) {
    return (
      <div>
        <Text type="secondary" style={{ fontSize: 12, display: "block", marginBottom: 8 }}>
          No providers configured. Add a provider first, then configure models with routes.
        </Text>
        <Button type="dashed" size="small" icon={<PlusOutlined />}
          onClick={() => setProvModal({ open: true, edit: null })}>Add Provider</Button>
        <ProviderModal open={provModal.open} provider={provModal.edit}
          onCancel={() => setProvModal({ open: false, edit: null })} onSave={handleProvSave} />
      </div>
    );
  }

  return (
    <div>
      {/* ?? Provider cards ?? */}
      <Text strong style={{ fontSize: 12, display: "block", marginBottom: 8 }}>Providers</Text>
      <div style={{ marginBottom: 16 }}>
        {providers.map(p => {
          const hasKey = keys[p.name];
          return (
            <Tag key={p.name} style={{ margin: 2, padding: "2px 8px", fontSize: 12, cursor: "default" }}>
              <Space size={4}>
                <Text strong style={{ fontSize: 12 }}>{p.name}</Text>
                {p.anthropic_url && <Tag color="blue" style={{ fontSize: 9, margin: 0, padding: "0 3px" }}>A</Tag>}
                {hasKey
                  ? <Tag color="green" style={{ fontSize: 9, margin: 0, padding: "0 3px", cursor: "pointer" }}
                    onClick={() => setKeyModal({ open: true, provider: p.name })}>key</Tag>
                  : <Button type="link" size="small" icon={<KeyOutlined />} style={{ fontSize: 10, padding: 0, height: 18 }}
                    onClick={() => setKeyModal({ open: true, provider: p.name })}>Set key</Button>}
                <Button type="link" size="small" icon={<EditOutlined />} style={{ fontSize: 10, padding: 0, height: 18 }}
                  onClick={() => setProvModal({ open: true, edit: p })} />
                <Button type="link" size="small" danger icon={<DeleteOutlined />} style={{ fontSize: 10, padding: 0, height: 18 }}
                  onClick={() => {
                    if (window.confirm(`Delete provider "${p.name}" and its models?`)) handleProvDelete(p.name);
                  }} />
              </Space>
            </Tag>
          );
        })}
        <Button type="dashed" size="small" icon={<PlusOutlined />} style={{ fontSize: 11, margin: 2 }}
          onClick={() => setProvModal({ open: true, edit: null })}>Provider</Button>
      </div>

      {/* ?? Model table ?? */}
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 8 }}>
        <Text strong style={{ fontSize: 12 }}>Virtual Models</Text>
        <Button type="dashed" size="small" icon={<PlusOutlined />} style={{ fontSize: 11 }}
          onClick={() => setModelModal({ open: true, edit: null })}>Add Model</Button>
      </div>
      {modelData.length > 0 ? (
        <Table dataSource={modelData} columns={modelCols} size="small" pagination={false}
          style={{ margin: 0 }}
          onRow={(record) => record._available ? {} : {
            style: { opacity: 0.45, cursor: "help" },
            title: "No API key configured for any of this model’s providers in the current profile"
          }}
          rowClassName={(record) => record._available ? "" : "model-unavailable"} />
      ) : (
        <Text type="secondary" style={{ fontSize: 12 }}>No models configured.</Text>
      )}

      <ProviderModal open={provModal.open} provider={provModal.edit}
        onCancel={() => setProvModal({ open: false, edit: null })} onSave={handleProvSave} />
      <ModelModal open={modelModal.open} model={modelModal.edit} providers={providers}
        onCancel={() => setModelModal({ open: false, edit: null })} onSave={handleModelSave} />
      <KeyModal open={keyModal.open} provider={keyModal.provider} unlocked={unlocked}
        onCancel={() => setKeyModal({ open: false, provider: "" })} onSave={handleKeySave} />
    </div>
  );
}
