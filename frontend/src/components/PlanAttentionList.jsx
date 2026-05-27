import { Card, Empty, List, Space, Tag, Typography } from "antd";
import PlanRecommendations from "./PlanRecommendations";
import { planAttentionColor } from "../lib/planView";

const { Text } = Typography;

export default function PlanAttentionList({ items }) {
  return (
    <Card className="console-card" title="Plan Attention" bordered={false}>
      {items.length ? (
        <List
          dataSource={items}
          renderItem={(item) => (
            <List.Item>
              <Space direction="vertical" size={4} style={{ width: "100%" }}>
                <Space wrap>
                  <Tag color={planAttentionColor(item)}>{item.state}</Tag>
                  {item.auto_action_available && <Tag color="green">auto</Tag>}
                  {item.manager_review_required && <Tag color="gold">review</Tag>}
                  {(item.issues || []).map((issue) => (
                    <Tag key={issue}>{issue}</Tag>
                  ))}
                </Space>
                <Text strong>{item.title}</Text>
                <Text type="secondary">{item.next_manager_checkpoint}</Text>
                <PlanRecommendations recommendations={item.recommendations} />
              </Space>
            </List.Item>
          )}
        />
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No plan attention items" />
      )}
    </Card>
  );
}
