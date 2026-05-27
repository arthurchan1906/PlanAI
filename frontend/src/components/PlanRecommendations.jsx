import { Button, Space, Tag, Typography } from "antd";

const { Text } = Typography;

export default function PlanRecommendations({ recommendations, busy, onAdvancePlan, planId }) {
  if (!recommendations?.length) {
    return null;
  }

  const first = recommendations[0];

  return (
    <Space direction="vertical" size={4} style={{ width: "100%" }}>
      <Space wrap>
        <Button
          size="small"
          type="primary"
          ghost
          loading={busy}
          disabled={!first?.auto_supported || !onAdvancePlan || !planId}
          onClick={() => onAdvancePlan(planId)}
        >
          Advance plan
        </Button>
        <Tag color={first?.auto_supported ? "green" : "gold"}>
          {first?.auto_supported ? "auto-advance ready" : "manager review required"}
        </Tag>
      </Space>
      {recommendations.map((item) => (
        <Space key={item.command} direction="vertical" size={2} style={{ width: "100%" }}>
          <Text strong>{item.title}</Text>
          <Text type="secondary">{item.reason}</Text>
          <Text code>{item.command}</Text>
        </Space>
      ))}
    </Space>
  );
}
