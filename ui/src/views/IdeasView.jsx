import { useMemo } from "react";
import { Button, Card, Empty, Form, Input, List, Select, Space, Tag, Typography } from "antd";
import { statusColor, IDEA_STATUSES } from "../utils/helpers";

const { Text, Paragraph } = Typography;
const { TextArea } = Input;

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
  onConvertToTask,
  onConvertToDecision,
  busy,
}) {
  const filteredIdeas = useMemo(() => {
    return ideas.filter((idea) => {
      const query = `${idea.title} ${idea.summary} ${idea.impact || ""} ${idea.source || ""}`.toLowerCase();
      return (!ideaStatusFilter || idea.status === ideaStatusFilter) && (!ideaSearch || query.includes(ideaSearch.toLowerCase()));
    });
  }, [ideas, ideaSearch, ideaStatusFilter]);

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
          <Button type="primary" htmlType="submit" loading={busy}>
            记录想法
          </Button>
        </Form>
      </Card>

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
              style={{ width: 220 }}
            />
            <Select
              value={ideaStatusFilter || undefined}
              allowClear
              placeholder="全部状态"
              style={{ width: 180 }}
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
              <List.Item>
                <Card className="inner-card" bordered={false}>
                  <Space direction="vertical" size={8} style={{ width: "100%" }}>
                    <Space wrap>
                      <Tag color={statusColor(idea.status)}>{idea.status}</Tag>
                      <Text type="secondary">{idea.source}</Text>
                      <Text type="secondary">{idea.created_at}</Text>
                    </Space>
                    <Text strong>{idea.title}</Text>
                    <Paragraph>{idea.summary}</Paragraph>
                    {!!idea.impact && <Text type="secondary">影响: {idea.impact}</Text>}
                    {idea.converted_to && (
                      <Space wrap>
                        <Tag color="green">已转化</Tag>
                        {idea.converted_to_type === 'task' && (
                          <Tag color="blue">任务: {idea.converted_to_title}</Tag>
                        )}
                        {idea.converted_to_type === 'decision' && (
                          <Tag color="gold">决策: {idea.converted_to_title}</Tag>
                        )}
                      </Space>
                    )}
                    <Space wrap>
                      {idea.status === 'accepted' && !idea.converted_to && (
                        <>
                          <Button size="small" type="primary" onClick={() => onConvertToTask?.(idea)}>转化为任务</Button>
                          <Button size="small" onClick={() => onConvertToDecision?.(idea)}>转化为决策</Button>
                        </>
                      )}
                      {idea.status === 'inbox' && (
                        <Button size="small" onClick={() => onUpdateIdea(idea.id, "under_review")}>开始评审</Button>
                      )}
                      <Button size="small" onClick={() => onUpdateIdea(idea.id, "accepted")}>Accept</Button>
                      <Button size="small" danger onClick={() => onUpdateIdea(idea.id, "rejected")}>Reject</Button>
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
    </div>
  );
}
