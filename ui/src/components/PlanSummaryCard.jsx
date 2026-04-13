import { List, Space, Tag, Typography } from "antd";
import PlanRecommendations from "./PlanRecommendations";
import { statusColor } from "../lib/planView";

const { Text } = Typography;

export default function PlanSummaryCard({ plan, busy, onAdvancePlan }) {
  return (
    <List.Item>
      <Space direction="vertical" size={2} style={{ width: "100%" }}>
        <Space wrap>
          <Tag color={statusColor(plan.status)}>{plan.status}</Tag>
          <Tag>{plan.source}</Tag>
          <Tag color={statusColor(plan.health?.state)}>{plan.health?.state || "draft"}</Tag>
        </Space>
        <Text strong>{plan.title}</Text>
        {!!plan.goal && <Text type="secondary">{plan.goal}</Text>}
        {!!plan.manager_summary?.next_manager_checkpoint && (
          <Text type="secondary">{plan.manager_summary.next_manager_checkpoint}</Text>
        )}
        {!!plan.task_count && (
          <Text type="secondary">
            {plan.manager_summary?.done_task_count || 0}/{plan.task_count} tasks complete
          </Text>
        )}
        {!!plan.health?.issues?.length && (
          <div className="tag-wrap">
            {plan.health.issues.map((issue) => (
              <Tag key={issue} color="gold">{issue}</Tag>
            ))}
          </div>
        )}
        <PlanRecommendations recommendations={plan.recommendations} busy={busy} onAdvancePlan={onAdvancePlan} planId={plan.id} />
        {!!plan.execution_packet?.prompt && (
          <details>
            <summary style={{ cursor: "pointer" }}>Execution packet</summary>
            <pre style={{ marginTop: 8, whiteSpace: "pre-wrap", fontSize: 12 }}>
              {plan.execution_packet.prompt}
            </pre>
          </details>
        )}
      </Space>
    </List.Item>
  );
}
