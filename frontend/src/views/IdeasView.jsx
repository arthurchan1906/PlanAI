import { useEffect, useMemo, useState } from "react";
import { Button, Card, Col, Empty, Form, Input, List, Row, Select, Space, Tag, Typography } from "antd";
import { api } from "../utils/api";
import { statusColor, IDEA_STATUSES } from "../utils/helpers";
import IdeaDetailPanel from "../components/IdeaDetailPanel";

const { Text, Paragraph } = Typography;
const { TextArea } = Input;

function makeDraft(idea) {
  return {
    current_summary: idea?.current_summary || idea?.summary || "",
    main_question: idea?.main_question || "",
    recommended_next_action: idea?.recommended_next_action || "continue_discussion",
  };
}

export default function IdeasView({
  ideas,
  ideaSearch,
  ideaStatusFilter,
  setIdeaSearch,
  setIdeaStatusFilter,
  ideaForm,
  setIdeaForm,
  onCreateIdea,
  onUpdateIdea,
  onCommentIdea,
  onConvertToTask,
  onConvertToDecision,
  focusedIdeaId,
  busy,
}) {
  const [selectedIdeaId, setSelectedIdeaId] = useState("");
  const [selectedIdea, setSelectedIdea] = useState(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [draft, setDraft] = useState(makeDraft(null));
  const [commentDraft, setCommentDraft] = useState("");

  const filteredIdeas = useMemo(() => {
    return ideas.filter((idea) => {
      const query = `${idea.title} ${idea.summary} ${idea.current_summary || ""} ${idea.main_question || ""} ${idea.impact || ""} ${idea.source || ""}`.toLowerCase();
      return (!ideaStatusFilter || idea.status === ideaStatusFilter) && (!ideaSearch || query.includes(ideaSearch.toLowerCase()));
    });
  }, [ideas, ideaSearch, ideaStatusFilter]);

  useEffect(() => {
    if (!filteredIdeas.length) {
      setSelectedIdeaId("");
      setSelectedIdea(null);
      setDraft(makeDraft(null));
      return;
    }
    if (!selectedIdeaId || !filteredIdeas.some((idea) => idea.id === selectedIdeaId)) {
      setSelectedIdeaId(filteredIdeas[0].id);
    }
  }, [filteredIdeas, selectedIdeaId]);

  useEffect(() => {
    if (focusedIdeaId && filteredIdeas.some((idea) => idea.id === focusedIdeaId)) {
      setSelectedIdeaId(focusedIdeaId);
    }
  }, [focusedIdeaId, filteredIdeas]);

  useEffect(() => {
    if (!selectedIdeaId) return;
    let cancelled = false;

    async function loadIdeaDetail() {
      setDetailLoading(true);
      try {
        const payload = await api(`/pmai/ideas/${selectedIdeaId}`);
        if (cancelled) return;
        setSelectedIdea(payload);
        setDraft(makeDraft(payload));
      } finally {
        if (!cancelled) {
          setDetailLoading(false);
        }
      }
    }

    loadIdeaDetail();
    return () => {
      cancelled = true;
    };
  }, [selectedIdeaId, ideas]);

  async function refreshSelectedIdea() {
    if (!selectedIdeaId) return;
    const payload = await api(`/pmai/ideas/${selectedIdeaId}`);
    setSelectedIdea(payload);
    setDraft(makeDraft(payload));
  }

  return (
    <div className="view-stack">
      <Card className="console-card" title="记录新想法" bordered={false}>
        <Form layout="vertical" onFinish={onCreateIdea}>
          <Form.Item label="标题" required>
            <Input
              value={ideaForm.title}
              onChange={(event) => setIdeaForm((current) => ({ ...current, title: event.target.value }))}
            />
          </Form.Item>
          <Form.Item label="Summary" required>
            <TextArea
              rows={4}
              value={ideaForm.summary}
              onChange={(event) => setIdeaForm((current) => ({ ...current, summary: event.target.value }))}
            />
          </Form.Item>
          <Form.Item label="Impact">
            <Input
              value={ideaForm.impact}
              onChange={(event) => setIdeaForm((current) => ({ ...current, impact: event.target.value }))}
            />
          </Form.Item>
          <Form.Item label="Current Summary">
            <TextArea
              rows={3}
              value={ideaForm.current_summary}
              onChange={(event) => setIdeaForm((current) => ({ ...current, current_summary: event.target.value }))}
            />
          </Form.Item>
          <Form.Item label="Main Question">
            <Input
              value={ideaForm.main_question}
              onChange={(event) => setIdeaForm((current) => ({ ...current, main_question: event.target.value }))}
            />
          </Form.Item>
          <Form.Item label="Recommended Next Action">
            <Select
              value={ideaForm.recommended_next_action}
              onChange={(value) => setIdeaForm((current) => ({ ...current, recommended_next_action: value }))}
              options={[
                { value: "continue_discussion", label: "continue_discussion" },
                { value: "ready_for_decision", label: "ready_for_decision" },
                { value: "ready_for_task", label: "ready_for_task" },
                { value: "hold", label: "hold" },
              ]}
            />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>
            记录想法
          </Button>
        </Form>
      </Card>

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={10}>
          <Card
            className="console-card"
            title="想法池"
            bordered={false}
            extra={
              <Space wrap>
                <Input
                  value={ideaSearch}
                  onChange={(event) => setIdeaSearch(event.target.value)}
                  placeholder="搜索想法"
                  style={{ width: 180 }}
                />
                <Select
                  value={ideaStatusFilter || undefined}
                  allowClear
                  placeholder="全部状态"
                  style={{ width: 160 }}
                  onChange={(value) => setIdeaStatusFilter(value || "")}
                  options={IDEA_STATUSES.map((status) => ({ value: status, label: status }))}
                />
              </Space>
            }
          >
            {filteredIdeas.length ? (
              <List
                itemLayout="vertical"
                dataSource={filteredIdeas}
                renderItem={(idea) => (
                  <List.Item onClick={() => setSelectedIdeaId(idea.id)} style={{ cursor: "pointer" }}>
                    <Card
                      className="inner-card"
                      bordered={false}
                      style={selectedIdeaId === idea.id ? { borderColor: "#2f6fec", boxShadow: "inset 0 0 0 1px #2f6fec" } : undefined}
                    >
                      <Space direction="vertical" size={8} style={{ width: "100%" }}>
                        <Space wrap>
                          <Tag color={statusColor(idea.status)}>{idea.status}</Tag>
                          {!!idea.recommended_next_action && <Tag color="purple">{idea.recommended_next_action}</Tag>}
                        </Space>
                        <Text strong>{idea.title}</Text>
                        <Paragraph ellipsis={{ rows: 2 }} style={{ marginBottom: 0 }}>
                          {idea.current_summary || idea.summary}
                        </Paragraph>
                        <Space wrap>
                          {!!idea.main_question && <Text type="secondary">问题: {idea.main_question}</Text>}
                          {!!idea.comment_count && <Text type="secondary">评论 {idea.comment_count}</Text>}
                          {!!idea.converted_to && (
                            <Tag color={idea.converted_to_type === "task" ? "blue" : "gold"}>
                              {idea.converted_to_type}: {idea.converted_to_title || idea.converted_to}
                            </Tag>
                          )}
                        </Space>
                      </Space>
                    </Card>
                  </List.Item>
                )}
              />
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无想法" />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={14}>
          <IdeaDetailPanel
            idea={selectedIdea}
            draft={draft}
            commentDraft={commentDraft}
            busy={busy || detailLoading}
            onDraftChange={(patch) => setDraft((current) => ({ ...current, ...patch }))}
            onCommentDraftChange={setCommentDraft}
            onSaveSummary={async () => {
              if (!selectedIdea) return;
              await onUpdateIdea(selectedIdea.id, {
                status: selectedIdea.status,
                current_summary: draft.current_summary,
                main_question: draft.main_question,
                recommended_next_action: draft.recommended_next_action,
              });
              await refreshSelectedIdea();
            }}
            onAddComment={async () => {
              if (!selectedIdea || !commentDraft.trim()) return;
              await onCommentIdea(selectedIdea.id, {
                content: commentDraft.trim(),
                kind: "comment",
                author_type: "human",
                author_name: "web",
              });
              setCommentDraft("");
              await refreshSelectedIdea();
            }}
            onSetStatus={async (status) => {
              if (!selectedIdea) return;
              await onUpdateIdea(selectedIdea.id, { status });
              await refreshSelectedIdea();
            }}
            onConvertToTask={onConvertToTask}
            onConvertToDecision={onConvertToDecision}
          />
        </Col>
      </Row>
    </div>
  );
}
