import { Button, Card, Col, Empty, List, Row, Space, Spin, Statistic, Tag, Typography } from "antd";
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
  const activeVision = visions.find((vision) => vision.status === "active");
  const activePrinciples = principles.filter((item) => item.status === "active").slice(0, 5);
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
  const readyIdeas = (ideas || [])
    .filter((idea) => ["ready_for_decision", "ready_for_task"].includes(idea.recommended_next_action))
    .slice(0, 5);
  const managerPriorityTitle = recommendedActions[0]?.title || closureBlockers[0]?.title || nextAction?.title || "No active checkpoint";
  const managerPriorityReason = recommendedActions[0]?.reason || nextAction?.reason || "No review blocker is currently highlighted.";
  const managerFocusTags = [
    inboxCounts.canon_followups ? `${inboxCounts.canon_followups} canon followups` : "",
    closureBlockers.length ? `${closureBlockers.length} closure blockers` : "",
    reviewQueue.length ? `${reviewQueue.length} evidence reviews` : "",
    planAttention.length ? `${planAttention.length} plan checkpoints` : "",
  ].filter(Boolean);

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
      return;
    }
    onOpenTasks?.();
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
          style={{ cursor: "pointer" }}
        >
          <Title level={4}>
            <CompassOutlined /> Active Vision: {activeVision.title}
          </Title>
          <Paragraph type="secondary">{activeVision.summary}</Paragraph>
          <div className="tag-wrap">
            <Tag color="blue">{activeVision.horizon}</Tag>
            <Text type="secondary">Updated {activeVision.updated_at}</Text>
          </div>
        </Card>
      )}

      <Card className="console-card" bordered={false}>
        <Space direction="vertical" size={4}>
          <Text strong>Human Review Workspace</Text>
          <Text type="secondary">
            This web UI is for human review and project management. AI coders should use `aipmc` in the terminal.
          </Text>
        </Space>
      </Card>

      <Row gutter={[16, 16]}>
        <Col xs={24} md={12} xl={6}>
          <Card className="console-card stat-card clickable-card" bordered={false} hoverable onClick={() => onOpenTasks?.()}>
            <Statistic title="In Progress Tasks" value={dashboard?.task_counts?.in_progress || 0} />
            <Text type="secondary">Total {dashboard?.task_counts?.total || 0}</Text>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={6}>
          <Card className="console-card stat-card clickable-card" bordered={false} hoverable onClick={() => onOpenDecisions?.()}>
            <Statistic title="Inbox Items" value={inboxCounts.total || 0} />
            <Text type="secondary">Use inbox as the review queue</Text>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={6}>
          <Card className="console-card stat-card clickable-card" bordered={false} hoverable onClick={() => onOpenDecisions?.()}>
            <Statistic title="Proposed Decisions" value={inboxCounts.proposed_decisions || 0} />
            <Text type="secondary">Review before execution continues</Text>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={6}>
          <Card className="console-card stat-card clickable-card" bordered={false} hoverable onClick={() => onOpenTasks?.()}>
            <Statistic title="Active Plans" value={dashboard?.plan_counts?.active || 0} />
            <Text type="secondary">
              Auto {dashboard?.plan_counts?.auto_advance_ready || 0} / Review {dashboard?.plan_counts?.manager_review_required || 0}
            </Text>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={6}>
          <Card className="console-card stat-card clickable-card" bordered={false} hoverable onClick={() => onOpenCanon?.()}>
            <Statistic title="Canon Followups" value={inboxCounts.canon_followups || 0} />
            <Text type="secondary">Linked decisions {canonMeta.related_decisions_count || 0}</Text>
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={12}>
          <Card className="console-card" title="Execution Focus" bordered={false}>
            <Space direction="vertical" size={8} style={{ width: "100%" }}>
              <div className="mainline-flow">
                <div className="mainline-step">
                  <Text type="secondary">Roadmap</Text>
                  <Text strong>{mainline?.roadmap?.title || "No active roadmap"}</Text>
                  {!!mainline?.roadmap?.status && <Tag color="blue">{mainline.roadmap.status}</Tag>}
                </div>
                <div className="mainline-flow__arrow">→</div>
                <div className="mainline-step">
                  <Text type="secondary">Plan</Text>
                  <Text strong>{mainline?.plan?.title || "No active plan"}</Text>
                  {!!mainline?.plan?.status && <Tag color="gold">{mainline.plan.status}</Tag>}
                </div>
                <div className="mainline-flow__arrow">→</div>
                <div className="mainline-step mainline-step--task">
                  <Text type="secondary">Task</Text>
                  <Text strong>{mainline?.task?.title || "No active task"}</Text>
                  {!!mainline?.task?.status && <Tag color="green">{mainline.task.status}</Tag>}
                </div>
              </div>
              {!!mainline?.task?.last_note && <Text type="secondary">{mainline.task.last_note}</Text>}
              {!!narrative?.why_now && <Paragraph type="secondary" style={{ marginBottom: 0 }}>{narrative.why_now}</Paragraph>}
              {!!narrative?.constraints_summary && <Text type="secondary">Constraints: {narrative.constraints_summary}</Text>}
              {!!narrative?.governance_focus && <Text type="secondary">Governance: {narrative.governance_focus}</Text>}
            </Space>
          </Card>
        </Col>
        <Col xs={24} xl={12}>
          <Card className="console-card" title="Manager Checkpoint" bordered={false}>
            <Space direction="vertical" size={8} style={{ width: "100%" }}>
              <Text strong>{managerPriorityTitle}</Text>
              <Text type="secondary">{managerPriorityReason}</Text>
              {!!managerFocusTags.length && (
                <div className="tag-wrap">
                  {managerFocusTags.map((item) => (
                    <Tag key={item} color="blue">{item}</Tag>
                  ))}
                </div>
              )}
              {!!nextAction?.command && <Text code>{nextAction.command}</Text>}
              {!!handoffNext.length && <Text type="secondary">Handoff: {handoffNext.join(" / ")}</Text>}
            </Space>
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={16}>
          <Card className="console-card" title="Recommended Actions" bordered={false}>
            {recommendedActions.length ? (
              <List
                dataSource={recommendedActions}
                renderItem={(action) => (
                  <List.Item
                    actions={[
                      <Button key="open" type="link" onClick={() => handleRecommendedAction(action)}>
                        Open
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
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No recommended actions" />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={8}>
          <Space direction="vertical" size={16} style={{ width: "100%" }}>
            <Card
              className="console-card"
              title="Ready Ideas"
              bordered={false}
              extra={<Button type="link" size="small" onClick={() => onOpenIdeas?.()}>Open ideas</Button>}
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
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No ready ideas" />
              )}
            </Card>
            <Card
              className="console-card"
              title="Active Principles"
              bordered={false}
              extra={<Button type="link" size="small" onClick={() => onOpenPrinciples?.()}>View all</Button>}
            >
              {activePrinciples.length ? (
                <List
                  dataSource={activePrinciples}
                  renderItem={(item) => (
                    <List.Item>
                      <Space direction="vertical" size={2}>
                        <Text strong><SafetyCertificateOutlined /> {item.title}</Text>
                        <Tag size="small">{item.kind}</Tag>
                      </Space>
                    </List.Item>
                  )}
                />
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No active principles" />
              )}
            </Card>
            <Card
              className="console-card"
              title="Evidence Review"
              bordered={false}
              extra={<Button type="link" size="small" onClick={() => onOpenCommitAttention?.("needs_review")}>Open queue</Button>}
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
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No review evidence" />
              )}
            </Card>
            <Card
              className="console-card"
              title="Closure Blockers"
              bordered={false}
              extra={<Button type="link" size="small" onClick={() => onOpenTasks?.()}>Open tasks</Button>}
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
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No closure blockers" />
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
          <Card className="console-card" title="Canon Snapshot" bordered={false}>
            {canon ? (
              <div className="canon-grid">
                <div>
                  <Text type="secondary">Engineering focus</Text>
                  <Paragraph ellipsis={{ rows: 2 }}>{canon.engineering_focus || "Not set"}</Paragraph>
                </div>
                <div>
                  <Text type="secondary">Version scope</Text>
                  <div className="tag-wrap">
                    {(canon.version_scope || []).map((item) => (
                      <Tag key={item}>{item}</Tag>
                    ))}
                  </div>
                </div>
              </div>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No canon data" />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={12}>
          <Space direction="vertical" size={16} style={{ width: "100%" }}>
            <Card className="console-card" title="Today Focus" bordered={false}>
              <List
                dataSource={dashboard?.today_focus || []}
                renderItem={(item) => (
                  <List.Item>
                    <Text strong>{item}</Text>
                  </List.Item>
                )}
                locale={{ emptyText: "No focus items" }}
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
                locale={{ emptyText: "No handoff risks" }}
              />
            </Card>
          </Space>
        </Col>
      </Row>
    </div>
  );
}
