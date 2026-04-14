import { Button, Card, Empty, Input, Select, Space, Tag, Typography } from "antd";

const { Text, Paragraph } = Typography;

export default function IdeaDetailPanel({
  idea,
  draft,
  commentDraft,
  busy,
  onDraftChange,
  onCommentDraftChange,
  onSaveSummary,
  onAddComment,
  onSetStatus,
  onConvertToTask,
  onConvertToDecision,
}) {
  if (!idea) {
    return (
      <Card className="console-card" title="想法详情" bordered={false}>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="选择一条想法查看详情" />
      </Card>
    );
  }

  return (
    <Card className="console-card" title="想法详情" bordered={false}>
      <Space direction="vertical" size={12} style={{ width: "100%" }}>
        <Space wrap>
          <Tag>{idea.status}</Tag>
          {!!idea.recommended_next_action && <Tag color="purple">{idea.recommended_next_action}</Tag>}
          {!!idea.converted_to && (
            <Tag color={idea.converted_to_type === "task" ? "blue" : "gold"}>
              {idea.converted_to_type}: {idea.converted_to_title || idea.converted_to}
            </Tag>
          )}
          <Text type="secondary">{idea.updated_at || idea.created_at}</Text>
        </Space>

        <div>
          <Text strong>{idea.title}</Text>
          <Paragraph type="secondary" style={{ marginTop: 8 }}>
            {idea.summary}
          </Paragraph>
        </div>

        {!!idea.impact && <Text type="secondary">影响: {idea.impact}</Text>}

        <Input.TextArea
          rows={4}
          value={draft.current_summary}
          placeholder="当前收敛摘要"
          onChange={(event) => onDraftChange({ current_summary: event.target.value })}
        />
        <Input
          value={draft.main_question}
          placeholder="当前主问题"
          onChange={(event) => onDraftChange({ main_question: event.target.value })}
        />
        <Select
          value={draft.recommended_next_action}
          onChange={(value) => onDraftChange({ recommended_next_action: value })}
          options={[
            { value: "continue_discussion", label: "continue_discussion" },
            { value: "ready_for_decision", label: "ready_for_decision" },
            { value: "ready_for_task", label: "ready_for_task" },
            { value: "hold", label: "hold" },
          ]}
        />

        <Space wrap>
          <Button size="small" onClick={onSaveSummary} loading={busy}>
            同步当前摘要
          </Button>
          {idea.status === "inbox" && (
            <Button size="small" onClick={() => onSetStatus("under_review")} loading={busy}>
              开始评审
            </Button>
          )}
          <Button size="small" onClick={() => onSetStatus("accepted")} loading={busy}>
            Accept
          </Button>
          <Button size="small" danger onClick={() => onSetStatus("rejected")} loading={busy}>
            Reject
          </Button>
        </Space>

        {idea.status === "accepted" && !idea.converted_to && (
          <Space wrap>
            <Button size="small" type="primary" onClick={() => onConvertToTask?.(idea)}>
              转化为任务
            </Button>
            <Button size="small" onClick={() => onConvertToDecision?.(idea)}>
              转化为决策
            </Button>
          </Space>
        )}

        <Card className="inner-card" bordered={false}>
          <Space direction="vertical" size={10} style={{ width: "100%" }}>
            <Input.TextArea
              rows={3}
              value={commentDraft}
              placeholder="追加一条讨论评论"
              onChange={(event) => onCommentDraftChange(event.target.value)}
            />
            <Button
              size="small"
              type="primary"
              ghost
              disabled={!commentDraft.trim()}
              onClick={onAddComment}
              loading={busy}
            >
              追加评论
            </Button>
          </Space>
        </Card>

        <Card className="inner-card" bordered={false}>
          <Space direction="vertical" size={8} style={{ width: "100%" }}>
            <Text strong>评论流</Text>
            {(idea.comments || []).length ? (
              (idea.comments || []).map((comment) => (
                <Card key={comment.id} className="inner-card" bordered={false}>
                  <Space direction="vertical" size={4} style={{ width: "100%" }}>
                    <Space wrap>
                      <Tag>{comment.kind}</Tag>
                      <Tag color="blue">{comment.author_type}</Tag>
                      <Text type="secondary">{comment.created_at}</Text>
                    </Space>
                    <Text>{comment.content}</Text>
                  </Space>
                </Card>
              ))
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无评论" />
            )}
          </Space>
        </Card>
      </Space>
    </Card>
  );
}
