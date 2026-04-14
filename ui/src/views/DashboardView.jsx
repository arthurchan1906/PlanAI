import { Badge, Button, Card, Col, Empty, List, Row, Space, Spin, Statistic, Tag, Typography } from "antd";
import { CompassOutlined, SafetyCertificateOutlined } from "@ant-design/icons";
import PlanAttentionList from "../components/PlanAttentionList";

const { Title, Paragraph, Text } = Typography;

export default function DashboardView({
  dashboard,
  aiContext,
  nextPacket,
  handoff,
  inbox,
  canon,
  visions,
  principles,
  ideas,
  loading,
  onOpenCanon,
  onOpenDecisions,
  onOpenTasks,
  onOpenCommits,
  onOpenCommitAttention,
  onOpenIdeas,
  onOpenDocs,
  onOpenDaily,
  onOpenPrinciples,
}) {
  const activeVision = visions.find(v => v.status === 'active');
  const activePrinciples = principles.filter(p => p.status === 'active').slice(0, 5);
  const recommendedActions = inbox?.recommended_actions || [];
  const inboxCounts = inbox?.counts || {};
  const canonMeta = inbox?.canon || {};
  const planAttention = dashboard?.plan_attention || [];
  const reviewQueue = dashboard?.review_queue || [];
  const closureBlockers = dashboard?.closure_blockers || [];
  const mainline = aiContext?.mainline || {};
  const narrative = aiContext?.narrative || {};
  const nextAction = nextPacket?.next_action || null;
  const handoffNext = handoff?.next || [];
  const handoffRisks = handoff?.risks || [];
  const readyIdeas = (ideas || []).filter((idea) =>
    ["ready_for_decision", "ready_for_task"].includes(idea.recommended_next_action)
  ).slice(0, 5);

  function handleRecommendedAction(action) {
    if (action.kind === "decision_review") {
      onOpenDecisions?.();
      return;
    }
    if (action.kind === "canon_followup") {
      onOpenCanon?.(action.target_id);
      return;
    }
    if (action.kind === "commit_review") {
      onOpenCommitAttention?.("needs_review");
      return;
    }
    if (action.kind === "verification_gap") {
      onOpenCommitAttention?.("needs_verification");
      return;
    }
    if (action.kind === "task_closure_blocker") {
      onOpenTasks?.();
    }
  }

  if (loading && !dashboard) {
    return (
      <div className="page-loading">
        <Spin size="large" />
      </div>
    );
  }

  return (
    <div className="view-stack">
      {activeVision && (
        <Card 
          className="console-card vision-banner" 
          bordered={false}
          hoverable
          onClick={() => onOpenPrinciples?.()}
          style={{ cursor: 'pointer' }}
        >
          <Title level={4}><CompassOutlined /> 当前愿景: {activeVision.title}</Title>
          <Paragraph type="secondary">{activeVision.summary}</Paragraph>
          <div className="tag-wrap">
            <Tag color="blue">{activeVision.horizon}</Tag>
            <Text type="secondary">更新于 {activeVision.updated_at}</Text>
          </div>
        </Card>
      )}

      <Row gutter={[16, 16]}>
        <Col xs={24} md={12} xl={6}>
          <Card 
            className="console-card stat-card clickable-card" 
            bordered={false}
            hoverable
            onClick={(e) => {
              e.preventDefault();
              onOpenTasks?.();
            }}
          >
            <Statistic title="进行中任务" value={dashboard?.task_counts?.in_progress || 0} />
            <Text type="secondary">总数 {dashboard?.task_counts?.total || 0}</Text>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={6}>
          <Card 
            className="console-card stat-card clickable-card" 
            bordered={false}
            hoverable
            onClick={(e) => {
              e.preventDefault();
              onOpenDecisions?.();
            }}
          >
            <Statistic title="待审批事项" value={inboxCounts.total || 0} />
            <Text type="secondary">优先先看 inbox</Text>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={6}>
          <Card 
            className="console-card stat-card clickable-card" 
            bordered={false}
            hoverable
            onClick={(e) => {
              e.preventDefault();
              onOpenDecisions?.();
            }}
          >
            <Statistic title="待决策" value={inboxCounts.proposed_decisions || 0} />
            <Text type="secondary">显式 review 后再推进</Text>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={6}>
          <Card 
            className="console-card stat-card clickable-card" 
            bordered={false}
            hoverable
            onClick={(e) => {
              e.preventDefault();
              onOpenTasks?.();
            }}
          >
            <Statistic title="Active Plans" value={dashboard?.plan_counts?.active || 0} />
            <Text type="secondary">
              Auto {dashboard?.plan_counts?.auto_advance_ready || 0} / Review {dashboard?.plan_counts?.manager_review_required || 0}
            </Text>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={6}>
          <Card 
            className="console-card stat-card clickable-card" 
            bordered={false}
            hoverable
            onClick={(e) => {
              e.preventDefault();
              onOpenCanon?.();
            }}
          >
            <Statistic title="规范同步" value={inboxCounts.canon_followups || 0} />
            <Text type="secondary">已关联 {canonMeta.related_decisions_count || 0}</Text>
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={12}>
          <Card className="console-card" title="AI Context" bordered={false}>
            <Space direction="vertical" size={8} style={{ width: "100%" }}>
              <Text strong>{mainline?.roadmap?.title || "暂无主线 Roadmap"}</Text>
              <Text type="secondary">{mainline?.plan?.title || "暂无激活 Plan"}</Text>
              <Text>{mainline?.task?.title || "暂无当前任务"}</Text>
              {!!mainline?.task?.last_note && <Text type="secondary">{mainline.task.last_note}</Text>}
              {!!narrative?.why_now && <Paragraph type="secondary" style={{ marginBottom: 0 }}>{narrative.why_now}</Paragraph>}
              {!!narrative?.constraints_summary && <Text type="secondary">约束: {narrative.constraints_summary}</Text>}
              {!!narrative?.governance_focus && <Text type="secondary">治理焦点: {narrative.governance_focus}</Text>}
            </Space>
          </Card>
        </Col>
        <Col xs={24} xl={12}>
          <Card className="console-card" title="Next / Handoff" bordered={false}>
            <Space direction="vertical" size={8} style={{ width: "100%" }}>
              <Text strong>{nextAction?.title || "暂无明确下一步"}</Text>
              <Text type="secondary">{nextAction?.reason || "当前没有生成推荐动作。"}</Text>
              {!!nextAction?.command && <Text code>{nextAction.command}</Text>}
              {!!handoffNext.length && <Text type="secondary">交接建议：{handoffNext.join(" / ")}</Text>}
            </Space>
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={16}>
          <Card className="console-card" title="推荐动作" bordered={false}>
            {recommendedActions.length ? (
              <List
                dataSource={recommendedActions}
                renderItem={(action) => (
                  <List.Item
                    actions={[
                      <Button key="open" type="link" onClick={() => handleRecommendedAction(action)}>
                        打开处理
                      </Button>,
                    ]}
                  >
                    <Space direction="vertical" size={4} style={{ width: "100%" }}>
                      <Space wrap>
                        <Tag color={action.priority === "high" ? "red" : "gold"}>{action.priority}</Tag>
                        <Tag>{action.kind}</Tag>
                      </Space>
                      <Text strong>{action.title}</Text>
                      <Text type="secondary">{action.reason}</Text>
                      <Text code>{action.command}</Text>
                    </Space>
                  </List.Item>
                )}
              />
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无推荐动作" />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={8}>
          <Space direction="vertical" size={16} style={{ width: "100%" }}>
            <Card
              className="console-card"
              title="Ready Ideas"
              bordered={false}
              extra={
                <Button type="link" size="small" onClick={() => onOpenIdeas?.()}>
                  打开想法池
                </Button>
              }
            >
              {readyIdeas.length ? (
                <List
                  dataSource={readyIdeas}
                  renderItem={(idea) => (
                    <List.Item>
                      <Space direction="vertical" size={2}>
                        <Text strong>{idea.title}</Text>
                        <Text type="secondary">{idea.current_summary || idea.summary}</Text>
                        <Tag color={idea.recommended_next_action === "ready_for_decision" ? "gold" : "blue"}>
                          {idea.recommended_next_action}
                        </Tag>
                      </Space>
                    </List.Item>
                  )}
                />
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无 ready ideas" />
              )}
            </Card>
            <Card
              className="console-card"
              title="活跃原则"
              bordered={false}
              extra={
                <Button type="link" size="small" onClick={() => onOpenPrinciples?.()}>
                  查看全部
                </Button>
              }
            >
              {activePrinciples.length ? (
                <List
                  dataSource={activePrinciples}
                  renderItem={(p) => (
                    <List.Item>
                      <Space direction="vertical" size={2}>
                        <Text strong><SafetyCertificateOutlined /> {p.title}</Text>
                        <Tag size="small">{p.kind}</Tag>
                      </Space>
                    </List.Item>
                  )}
                />
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无活跃原则" />
              )}
            </Card>
            <Card
              className="console-card"
              title="Evidence Review"
              bordered={false}
              extra={
                <Button type="link" size="small" onClick={() => onOpenCommitAttention?.("needs_review")}>
                  打开待审
                </Button>
              }
            >
              {reviewQueue.length ? (
                <List
                  dataSource={reviewQueue}
                  renderItem={(item) => (
                    <List.Item>
                      <Space direction="vertical" size={2}>
                        <Text strong>{item.title}</Text>
                        <Space wrap>
                          <Tag color={item.attention === "needs_review" ? "gold" : "cyan"}>{item.attention}</Tag>
                          <Tag>{item.review_status}</Tag>
                          <Tag>{item.test_status}</Tag>
                        </Space>
                      </Space>
                    </List.Item>
                  )}
                />
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无待审 evidence" />
              )}
            </Card>
            <Card
              className="console-card"
              title="Closure Blockers"
              bordered={false}
              extra={
                <Button type="link" size="small" onClick={() => onOpenTasks?.()}>
                  打开任务
                </Button>
              }
            >
              {closureBlockers.length ? (
                <List
                  dataSource={closureBlockers}
                  renderItem={(item) => (
                    <List.Item>
                      <Space direction="vertical" size={2}>
                        <Text strong>{item.title}</Text>
                        <div className="tag-wrap">
                          {(item.reasons || []).map((reason) => (
                            <Tag key={`${item.id}-${reason}`} color="red">{reason}</Tag>
                          ))}
                        </div>
                      </Space>
                    </List.Item>
                  )}
                />
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无收口阻塞" />
              )}
            </Card>
          </Space>
        </Col>
      </Row>

      <Row gutter={[16, 16]}>
        <Col xs={24}>
          <PlanAttentionList items={planAttention} />
        </Col>
      </Row>

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={12}>
          <Card className="console-card" title="基线快照" bordered={false}>
            {canon ? (
              <div className="canon-grid">
                <div>
                  <Text type="secondary">工程重点</Text>
                  <Paragraph ellipsis={{ rows: 2 }}>{canon.engineering_focus || '未定义'}</Paragraph>
                </div>
                <div>
                  <Text type="secondary">版本范围</Text>
                  <div className="tag-wrap">
                    {(canon.version_scope || []).map((item) => (
                      <Tag key={item}>{item}</Tag>
                    ))}
                  </div>
                </div>
              </div>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无规范数据" />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={12}>
          <Space direction="vertical" size={16} style={{ width: "100%" }}>
            <Card className="console-card" title="今日关注" bordered={false}>
              <List
                dataSource={dashboard?.today_focus || []}
                renderItem={(item) => (
                  <List.Item>
                    <Text strong>{item}</Text>
                  </List.Item>
                )}
                locale={{ emptyText: "暂无关注项" }}
              />
            </Card>
            <Card className="console-card" title="Handoff Risks" bordered={false}>
              <List
                dataSource={handoffRisks}
                renderItem={(item) => (
                  <List.Item>
                    <Text type="secondary">{item}</Text>
                  </List.Item>
                )}
                locale={{ emptyText: "暂无交接风险" }}
              />
            </Card>
          </Space>
        </Col>
      </Row>
    </div>
  );
}
