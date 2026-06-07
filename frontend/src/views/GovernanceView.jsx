import { useState } from "react";
import { Card, Table, Tag, Typography, Space, Button, Input, Form, Select, Descriptions, Tabs } from "antd";

const { Paragraph } = Typography;

export default function GovernanceView({
  decisions, principles, canon, visions, canonForm, setCanonForm,
  decisionForm, setDecisionForm, principleForm, setPrincipleForm,
  visionForm, setVisionForm,
  busy, loading,
  onCreateDecision, onCreatePrinciple, onCreateVision,
  onSubmitCanon,
}) {
  const [tab, setTab] = useState("decisions");
  const vLen = (visions || []).length;
  const pLen = (principles || []).length;
  const dLen = (decisions || []).length;

  return (
    <div>
      <Tabs activeKey={tab} onChange={setTab} items={[
        {
          key: "visions", label: `Visions (${vLen})`,
          children: (
            <div>
              {(visions || []).map(v => (
                <Card key={v.id} size="small" style={{ marginBottom: 8 }}>
                  <Paragraph strong>{v.title}</Paragraph>
                  <Paragraph type="secondary">{v.summary}</Paragraph>
                  <Space><Tag>{v.status}</Tag><Tag>{v.horizon}</Tag></Space>
                </Card>
              ))}
              <Card size="small" title="添加 Vision" style={{ marginTop: 8 }}>
                <Form layout="inline" onFinish={() => { onCreateVision?.({ ...visionForm, status: visionForm?.status || "active", horizon: visionForm?.horizon || "long_term" }); setVisionForm?.({ title: "", summary: "", status: "active", horizon: "long_term" }); }}>
                  <Form.Item><Input placeholder="标题" value={visionForm?.title || ""} onChange={e => setVisionForm?.({ ...visionForm, title: e.target.value })} /></Form.Item>
                  <Form.Item><Input placeholder="摘要" value={visionForm?.summary || ""} onChange={e => setVisionForm?.({ ...visionForm, summary: e.target.value })} /></Form.Item>
                  <Form.Item><Button type="primary" htmlType="submit" loading={busy}>添加</Button></Form.Item>
                </Form>
              </Card>
            </div>
          ),
        },
        {
          key: "canon", label: "Canon",
          children: (
            <div>
              {canon ? (
                <Descriptions column={1} size="small" bordered>
                  <Descriptions.Item label="产品目标">{canon.product_goal || "—"}</Descriptions.Item>
                  <Descriptions.Item label="工程重点">{canon.engineering_focus || "—"}</Descriptions.Item>
                  <Descriptions.Item label="架构方向">{canon.architecture || "—"}</Descriptions.Item>
                </Descriptions>
              ) : <Paragraph type="secondary">Canon 未设置</Paragraph>}
              <Card size="small" title="更新" style={{ marginTop: 8 }}>
                <Form layout="inline" onFinish={onSubmitCanon}>
                  <Form.Item><Input placeholder="产品目标" value={canonForm?.productGoal || ""} onChange={e => setCanonForm?.({ ...canonForm, productGoal: e.target.value })} /></Form.Item>
                  <Form.Item><Input placeholder="工程重点" value={canonForm?.engineeringFocus || ""} onChange={e => setCanonForm?.({ ...canonForm, engineeringFocus: e.target.value })} /></Form.Item>
                  <Form.Item><Input placeholder="架构" value={canonForm?.architecture || ""} onChange={e => setCanonForm?.({ ...canonForm, architecture: e.target.value })} /></Form.Item>
                  <Form.Item><Button type="primary" htmlType="submit" loading={busy}>更新</Button></Form.Item>
                </Form>
              </Card>
            </div>
          ),
        },
        {
          key: "principles", label: `Principles (${pLen})`,
          children: (
            <div>
              <Table dataSource={principles || []} rowKey="id" size="small" loading={loading} pagination={false}
                columns={[
                  { title: "标题", dataIndex: "title", ellipsis: true },
                  { title: "类型", dataIndex: "kind", width: 80, render: k => <Tag>{k}</Tag> },
                  { title: "状态", dataIndex: "status", width: 70, render: s => <Tag color={s === "active" ? "green" : "default"}>{s}</Tag> },
                  { title: "摘要", dataIndex: "summary", ellipsis: true },
                ]}
              />
              <Card size="small" title="添加原则" style={{ marginTop: 8 }}>
                <Form layout="inline" onFinish={() => { onCreatePrinciple?.(); setPrincipleForm?.({ title: "", summary: "", kind: "governance", status: "active" }); }}>
                  <Form.Item><Input placeholder="标题" value={principleForm?.title || ""} onChange={e => setPrincipleForm?.({ ...principleForm, title: e.target.value })} /></Form.Item>
                  <Form.Item><Input placeholder="摘要" value={principleForm?.summary || ""} onChange={e => setPrincipleForm?.({ ...principleForm, summary: e.target.value })} /></Form.Item>
                  <Form.Item><Select value={principleForm?.kind || "governance"} onChange={v => setPrincipleForm?.({ ...principleForm, kind: v })} options={[
                    { value: "governance", label: "治理" }, { value: "engineering", label: "工程" }, { value: "product", label: "产品" }, { value: "meta", label: "元规则" },
                  ]} /></Form.Item>
                  <Form.Item><Button type="primary" htmlType="submit" loading={busy}>添加</Button></Form.Item>
                </Form>
              </Card>
            </div>
          ),
        },
        {
          key: "decisions", label: `Decisions (${dLen})`,
          children: (
            <div>
              <Table dataSource={decisions || []} rowKey="id" size="small" loading={loading} pagination={false}
                columns={[
                  { title: "标题", dataIndex: "title", ellipsis: true },
                  { title: "日期", dataIndex: "date", width: 100 },
                  { title: "状态", dataIndex: "status", width: 80, render: s => <Tag color={s === "accepted" ? "green" : s === "proposed" ? "orange" : "default"}>{s}</Tag> },
                  { title: "背景", dataIndex: "background", ellipsis: true },
                ]}
              />
              <Card size="small" title="记录决策" style={{ marginTop: 8 }}>
                <Form layout="inline" onFinish={() => { onCreateDecision(); setDecisionForm({ title: "", background: "", decision: "" }); }}>
                  <Form.Item><Input placeholder="标题" value={decisionForm?.title || ""} onChange={e => setDecisionForm?.({ ...decisionForm, title: e.target.value })} /></Form.Item>
                  <Form.Item><Input placeholder="背景" value={decisionForm?.background || ""} onChange={e => setDecisionForm?.({ ...decisionForm, background: e.target.value })} /></Form.Item>
                  <Form.Item><Input placeholder="决策内容" value={decisionForm?.decision || ""} onChange={e => setDecisionForm?.({ ...decisionForm, decision: e.target.value })} /></Form.Item>
                  <Form.Item><Button type="primary" htmlType="submit" loading={busy}>记录</Button></Form.Item>
                </Form>
              </Card>
            </div>
          ),
        },
      ]} />
    </div>
  );
}
