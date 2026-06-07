import { useMemo, useState } from "react";
import { Card, Empty, Space, Tag, Typography, Tooltip, Segmented, Collapse, Button, Badge, Spin } from "antd";
import { PlusOutlined, PauseCircleOutlined, NodeIndexOutlined, PartitionOutlined } from "@ant-design/icons";
import ThreadsCanvasView from "./ThreadsCanvasView";

const { Text, Paragraph } = Typography;

const ITEM_COLORS = {
  task: "#2f6fec",
  commit: "#52c41a",
  decision: "#faad14",
  idea: "#eb2f96",
  plan: "#722ed1",
  bug: "#ff4d4f",
  roadmap: "#13c2c2",
};

const STATUS_COLORS = {
  done: "#52c41a",
  completed: "#52c41a",
  committed: "#52c41a",
  merged: "#52c41a",
  active: "#2f6fec",
  in_progress: "#2f6fec",
  todo: "#8c8c8c",
  draft: "#8c8c8c",
  blocked: "#ff4d4f",
  cancelled: "#d9d9d9",
  paused: "#faad14",
  proposed: "#faad14",
  accepted: "#2f6fec",
};

function statusColor(s) { return STATUS_COLORS[s] || "#8c8c8c"; }
function itemColor(t) { return ITEM_COLORS[t] || "#8c8c8c"; }

function parseDate(d) {
  if (!d) return 0;
  const t = new Date(d);
  return isNaN(t.getTime()) ? 0 : t.getTime();
}

function fmtDate(d) {
  if (!d) return "";
  return d.split("T")[0] || d.slice(0, 10);
}

function shortId(id) {
  if (!id) return "";
  return id.includes("-") ? id.split("-").pop().slice(0, 6) : id.slice(0, 8);
}

function ItemDot({ item, onClick }) {
  const color = itemColor(item.entity_type);
  const bg = statusColor(item.status);
  return (
    <Tooltip
      title={
        <div style={{ maxWidth: 260 }}>
          <div style={{ fontWeight: 600, marginBottom: 4 }}>{item.title || item.entity_id}</div>
          <Space size={4}>
            <Tag color={color} style={{ fontSize: 10, lineHeight: "16px" }}>{item.entity_type}</Tag>
            <Tag color={bg} style={{ fontSize: 10, lineHeight: "16px" }}>{item.status}</Tag>
          </Space>
          {item.note && <div style={{ fontSize: 11, marginTop: 4, color: "#bbb" }}>{item.note}</div>}
          <div style={{ fontSize: 10, color: "#999", marginTop: 2 }}>{fmtDate(item.added_at)}</div>
        </div>
      }
    >
      <span
        className="thread-dot"
        style={{ background: bg, borderColor: color }}
        onClick={() => onClick?.(item)}
      />
    </Tooltip>
  );
}

function ThreadSwimlane({ thread, timeRange, onItemClick }) {
  const items = thread.items || [];
  if (!items.length) return null;

  const [minT, maxT] = timeRange;
  const range = maxT - minT || 1;
  const itemCount = items.length;

  const getX = (dateStr) => ((parseDate(dateStr) - minT) / range) * 100;

  return (
    <div className="thread-swimlane">
      <div className="swimlane-header">
        <Space size={4}>
          <NodeIndexOutlined style={{ color: "#2f6fec", fontSize: 14 }} />
          <Text strong style={{ fontSize: 13 }}>{thread.title}</Text>
          <Tag style={{ fontSize: 10 }}>{itemCount} items</Tag>
          {thread.status !== "active" && (
            <Tag color="default" style={{ fontSize: 10 }}>{thread.status}</Tag>
          )}
        </Space>
        {thread.summary && (
          <Paragraph
            type="secondary"
            ellipsis={{ rows: 1 }}
            style={{ fontSize: 11, margin: "2px 0 0 22px", maxWidth: 360 }}
          >
            {thread.summary}
          </Paragraph>
        )}
      </div>
      <div className="swimlane-track">
        {items.map((item, i) => (
          <ItemDot
            key={`${item.entity_type}-${item.entity_id}-${i}`}
            item={item}
            onClick={onItemClick}
          />
        ))}
      </div>
      <div className="swimlane-footer">
        <Text type="secondary" style={{ fontSize: 10 }}>
          {fmtDate(thread.created_at)} → {fmtDate(thread.updated_at)}
        </Text>
      </div>
    </div>
  );
}

function ThreadSuggestCard({ suggestion, onCreateThread }) {
  const items = suggestion.source_entities || [];
  return (
    <Card
      size="small"
      className="thread-suggest-card"
      title={
        <Space size={4}>
          <Badge status="processing" />
          <Text strong style={{ fontSize: 13 }}>{suggestion.suggested_title}</Text>
          <Tag style={{ fontSize: 10 }}>score: {Math.round(suggestion.score * 100)}%</Tag>
        </Space>
      }
      extra={
        <Button
          size="small"
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => onCreateThread(suggestion)}
        >
          确认
        </Button>
      }
    >
      <Paragraph type="secondary" style={{ fontSize: 11, marginBottom: 8 }}>
        {suggestion.rationale}
      </Paragraph>
      <Space size={4} wrap>
        {items.slice(0, 5).map((e, i) => (
          <Tag key={i} color={itemColor(e.entity_type)} style={{ fontSize: 10 }}>
            {e.title?.slice(0, 30) || e.entity_id?.slice(0, 12)}
          </Tag>
        ))}
        {items.length > 5 && (
          <Text type="secondary" style={{ fontSize: 10 }}>+{items.length - 5} more</Text>
        )}
      </Space>
    </Card>
  );
}

export default function ThreadsView({
  threads,
  threadSuggestions,
  threadStatus,
  tasks,
  commits,
  decisions,
  plans,
  busy,
  loading,
  onCreateThread,
  onAddToThread,
}) {
  const [filter, setFilter] = useState("all");
  const [viewMode, setViewMode] = useState(threads?.length ? "canvas" : "list");

  const timeRange = useMemo(() => {
    let minT = Infinity, maxT = -Infinity;
    for (const t of threads) {
      for (const item of t.items || []) {
        const d = parseDate(item.added_at);
        if (d && d > 0) {
          if (d < minT) minT = d;
          if (d > maxT) maxT = d;
        }
      }
    }
    if (!isFinite(minT)) {
      minT = Date.now() - 30 * 86400000;
      maxT = Date.now();
    }
    const pad = (maxT - minT) * 0.05 || 86400000;
    return [minT - pad, maxT + pad];
  }, [threads]);

  const filtered = useMemo(() => {
    if (filter === "all") return threads;
    if (filter === "active") return threads.filter(t => t.status === "active");
    if (filter === "paused") {
      const pausedIds = new Set(threadStatus.filter(s => s.paused).map(s => s.thread_id));
      return threads.filter(t => pausedIds.has(t.id));
    }
    return threads;
  }, [threads, threadStatus, filter]);

  const pausedCount = threadStatus.filter(s => s.paused).length;

  function handleItemClick(item) {
    if (item.entity_type === "task" || item.entity_type === "plan") {
      window.location.hash = "planning";
    }
    // Could be extended to navigate to specific entity
  }

  return (
    <div className="threads-view">
      <div className="threads-toolbar">
        <Space>
          <Segmented
            size="small"
            value={viewMode}
            onChange={setViewMode}
            options={[
              { label: "画布", value: "canvas", icon: <PartitionOutlined /> },
              { label: "列表", value: "list", icon: <NodeIndexOutlined /> },
            ]}
          />
          {viewMode === "list" && (
            <Segmented
              size="small"
              value={filter}
              onChange={setFilter}
              options={[
                { label: "全部", value: "all" },
                { label: "活跃", value: "active" },
                { label: pausedCount > 0 ? `暂停 (${pausedCount})` : "暂停", value: "paused", disabled: pausedCount === 0 },
              ]}
            />
          )}
          {pausedCount > 0 && (
            <Tag icon={<PauseCircleOutlined />} color="warning" style={{ fontSize: 11 }}>
              {pausedCount} 条线索超过 7 天无活动
            </Tag>
          )}
        </Space>
      </div>

      {viewMode === "canvas" && threads?.length > 0 && (
        <ThreadsCanvasView
          threads={threads}
          plans={plans}
          tasks={tasks}
          commits={commits}
          decisions={decisions}
          threadSuggestions={threadSuggestions}
          threadStatus={threadStatus}
          loading={loading}
        />
      )}

      {viewMode === "list" && (
        <>
          {threadSuggestions.length > 0 && (
            <Collapse
              size="small"
              ghost
              className="thread-suggest-collapse"
              items={[{
                key: "suggestions",
                label: <Text strong style={{ fontSize: 13 }}>建议的线索 ({threadSuggestions.length})</Text>,
                children: (
                  <div className="thread-suggest-list">
                    {threadSuggestions.map((s, i) => (
                      <ThreadSuggestCard
                        key={i}
                        suggestion={s}
                        onCreateThread={(sug) => onCreateThread?.(sug)}
                      />
                    ))}
                  </div>
                ),
              }]}
            />
          )}

          {filtered.length === 0 ? (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={
                <span>
                  暂无{filter === "active" ? "活跃" : filter === "paused" ? "暂停的" : ""}线索
                  <br />
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    线索从 commit 历史中自动识别，在每日结束时由 AI Agent 建议
                  </Text>
                </span>
              }
            />
          ) : (
            <div className="swimlane-list">
              {filtered.map((thread) => (
                <ThreadSwimlane
                  key={thread.id}
                  thread={thread}
                  timeRange={timeRange}
                  onItemClick={handleItemClick}
                />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}
