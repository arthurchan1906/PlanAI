import { useState, useEffect } from "react";
import {
  Card, Table, Tag, Typography, Empty, Spin, Alert, Descriptions, Space,
} from "antd";
import {
  CheckCircleOutlined, CloseCircleOutlined, MinusCircleOutlined, ThunderboltOutlined,
} from "@ant-design/icons";
import { api } from "../utils/api";

const { Title, Text } = Typography;

// 目标值来自 docs/EVALUATION.md + docs/METRICS_REGISTRY.md（8/27 口径统一后）。
const QUALITY_TARGETS = [
  { key: "summary_coverage", label: "summary_coverage (B1)", target: "≥85%", upGood: true },
  { key: "event_processed_rate", label: "event_processed_rate (D2 可行动)", target: "≥40%", upGood: true },
  { key: "workflow_completed_rate", label: "workflow_completed_rate", target: "参考", upGood: true },
  { key: "event_unconsumed", label: "event_unconsumed (注入池)", target: "↓", upGood: false },
  { key: "l2_nested_goal", label: "l2_nested_goal (B2)", target: "=0", upGood: false },
  { key: "correction_signals", label: "correction_signals", target: "↓ 辅助", upGood: false },
];

const INJECTION_TARGETS = [
  { key: "emerge_events_total", label: "emerge_events_total", target: "↓", upGood: false },
  { key: "action_items", label: "action_items", target: "≤10", upGood: false },
  { key: "inject_chars", label: "inject_chars 均值", target: "≤800", upGood: false },
  { key: "inject_reach_rate", label: "inject_reach_rate (到达率)", target: "↑ 对照", upGood: true },
];

function fmtRate(v) { return `${(v * 100).toFixed(1)}%`; }
function fmtTok(v) { return v >= 1e9 ? `${(v / 1e9).toFixed(2)}B` : v >= 1e6 ? `${(v / 1e6).toFixed(1)}M` : `${v}`; }

function StatusTag({ value, target, upGood }) {
  if (target === "参考" || target.startsWith("↓ 辅助") || target.startsWith("↑ 对照")) {
    return <Tag icon={<MinusCircleOutlined />} color="default">参考</Tag>;
  }
  let ok;
  if (target === "=0") ok = value === 0;
  else if (target === "↓") ok = false; // 无阈值，仅参考
  else if (target.startsWith("≤")) ok = value <= parseInt(target.replace("≤", ""), 10);
  else if (target.startsWith("≥")) ok = value >= parseFloat(target.replace("≥", "").replace("%", "")) / 100;
  else ok = null;
  if (ok === null) return <Tag color="default">—</Tag>;
  return ok
    ? <Tag icon={<CheckCircleOutlined />} color="success">达标</Tag>
    : <Tag icon={<CloseCircleOutlined />} color="error">未达标</Tag>;
}

function MetricRow({ m }) {
  const v = m.value;
  const shown = typeof v === "number" && (m.key.includes("rate") || m.key.includes("coverage"))
    ? fmtRate(v) : String(v ?? "—");
  return (
    <Space style={{ width: "100%", justifyContent: "space-between" }}>
      <Text>{m.label} <Text type="secondary">({m.target})</Text></Text>
      <Space>
        <Text strong>{shown}</Text>
        <StatusTag value={v} target={m.target} upGood={m.upGood} />
      </Space>
    </Space>
  );
}

export default function MetricsView() {
  const [snap, setSnap] = useState(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api("/pmai/web/snapshot").then(d => {
      if (d.ok && d.snapshot) setSnap(d.snapshot);
      else setError(d.error || "无快照");
    }).catch(e => setError(String(e))).finally(() => setLoading(false));
  }, []);

  if (loading) return <div style={{ textAlign: "center", padding: 60 }}><Spin /></div>;
  if (error) return <Empty description={<span>{error}<br /><Text type="secondary">先运行 <Text code>aipmc snapshot</Text> 生成快照</Text></span>} />;

  const { window: win, metrics, generated_at } = snap;
  const q = metrics.quality || {};
  const inj = metrics.injection || {};
  const cons = metrics.consumption || {};

  const qualityRows = QUALITY_TARGETS.map(t => ({ key: t.key, ...t, value: q[t.key] }));
  const injectionRows = INJECTION_TARGETS.map(t => ({ key: t.key, ...t, value: inj[t.key] }));

  const consRows = Object.entries(cons).map(([name, c]) => ({
    key: name,
    agent: name,
    calls: c.calls,
    in_tok: fmtTok(c.in_tok),
    out_tok: fmtTok(c.out_tok),
    avg_lat: c.avg_lat ? `${c.avg_lat.toFixed(1)}s` : "—",
    p95_lat: c.p95_lat ? `${c.p95_lat.toFixed(1)}s` : "—",
    cache_hit_rate: fmtRate(c.cache_hit_rate),
    injected_rate: fmtRate(c.injected_rate),
  }));

  const consCols = [
    { title: "agent", dataIndex: "agent" },
    { title: "calls", dataIndex: "calls", align: "right" },
    { title: "in_tok", dataIndex: "in_tok", align: "right" },
    { title: "out_tok", dataIndex: "out_tok", align: "right" },
    { title: "avg_lat", dataIndex: "avg_lat", align: "right" },
    { title: "p95_lat", dataIndex: "p95_lat", align: "right" },
    { title: "cache_hit", dataIndex: "cache_hit_rate", align: "right" },
    { title: "injected(尝试)", dataIndex: "injected_rate", align: "right" },
  ];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <Card size="small">
        <Space split={<Text type="secondary">·</Text>}>
          <Text type="secondary"><ThunderboltOutlined /> 反馈镜子快照（方案 A：落盘+只读）</Text>
          <Text type="secondary">生成: {generated_at || "—"}</Text>
          <Text type="secondary">窗口: {win?.since?.slice(0, 16)} ~ {win?.until?.slice(0, 16)}</Text>
          <Text type="secondary">schema v{snap.schema_version}</Text>
        </Space>
      </Card>

      <Card size="small" title="质量类（DB，与 aipmc metrics 同口径）">
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(320px, 1fr))", gap: 12 }}>
          {qualityRows.map(m => <MetricRow key={m.key} m={m} />)}
        </div>
      </Card>

      <Card size="small" title="消耗类（[LLM] 日志，窗口内）">
        <Table
          size="small"
          rowKey="agent"
          columns={consCols}
          dataSource={consRows}
          pagination={false}
        />
        <Alert
          style={{ marginTop: 8 }}
          type="info"
          showIcon
          message="injected_rate 是「请求带注入尝试的比例」，非到达率——对照 inject_reach_rate 看通道健康（8/27 口径标注）。"
        />
      </Card>

      <Card size="small" title="注入类（[INJECT] 日志，窗口内）">
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(320px, 1fr))", gap: 12 }}>
          {injectionRows.map(m => <MetricRow key={m.key} m={m} />)}
        </div>
        <Alert
          style={{ marginTop: 8 }}
          type="info"
          showIcon
          message={`cache_hit 字段覆盖率 ${(snap.notes?.cache_hit_coverage * 100).toFixed(1)}%（responses 路径缺口记录在 METRICS_SPEC 备注）`}
        />
      </Card>
    </div>
  );
}
