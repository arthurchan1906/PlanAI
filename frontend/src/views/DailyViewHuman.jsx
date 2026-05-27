import { Button, Card, Col, Empty, Form, Input, List, Row, Space, Tag, Typography } from "antd";
import { todayString } from "../utils/helpers";

const { Text } = Typography;

export default function DailyViewHuman({ daily, dailyForm, setDailyForm, onAppendDaily, onReplaceDaily, tasks, commits, onCreateTaskFromDaily, busy }) {
  const todayCommits = (commits || []).filter(c => c.created_at && c.created_at.startsWith(dailyForm.noteDate || todayString()));

  return (
    <div className="view-stack">
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={10}>
          <Card className="console-card" title="Daily Update" bordered={false}>
            <Form layout="vertical" onFinish={onAppendDaily}>
              <Form.Item label="Date">
                <Input type="date" value={dailyForm.noteDate} onChange={e => setDailyForm({...dailyForm, noteDate: e.target.value})} />
              </Form.Item>
              <Form.Item label="Completed">
                <Input value={dailyForm.completed} placeholder="item 1 | item 2" onChange={e => setDailyForm({...dailyForm, completed: e.target.value})} />
              </Form.Item>
              <Form.Item label="Problems">
                <Input value={dailyForm.problems} placeholder="item 1 | item 2" onChange={e => setDailyForm({...dailyForm, problems: e.target.value})} />
              </Form.Item>
              <Form.Item label="Risks">
                <Input value={dailyForm.risks} placeholder="item 1 | item 2" onChange={e => setDailyForm({...dailyForm, risks: e.target.value})} />
              </Form.Item>
              <Form.Item label="Next">
                <Input value={dailyForm.next} placeholder="item 1 | item 2" onChange={e => setDailyForm({...dailyForm, next: e.target.value})} />
              </Form.Item>
              <Space>
                <Button type="primary" htmlType="submit" loading={busy}>Append</Button>
                <Button onClick={onReplaceDaily} loading={busy}>Replace</Button>
              </Space>
            </Form>
          </Card>
          {todayCommits.length > 0 && (
            <Card className="console-card" title="今日提交" bordered={false} style={{ marginTop: 16 }}>
              <List
                size="small"
                dataSource={todayCommits}
                renderItem={c => (
                  <List.Item>
                    <Space direction="vertical" size={2}>
                      <Text strong>{c.title}</Text>
                      <Text type="secondary" size="small">{c.short_hash || c.commit_hash?.slice(0, 8)}</Text>
                      {c.task_id && <Tag color="blue">关联任务</Tag>}
                    </Space>
                  </List.Item>
                )}
              />
            </Card>
          )}
        </Col>
        <Col xs={24} xl={14}>
          <Card className="console-card" title={`Daily Note (${daily?.note_date || todayString()})`} bordered={false}>
            {daily ? (
              <Row gutter={[16, 16]}>
                <Col span={12}>
                  <Card size="small" title="Completed" bordered={false}>
                    <List size="small" dataSource={daily.completed || []} renderItem={i => <List.Item>{i}</List.Item>} />
                  </Card>
                </Col>
                <Col span={12}>
                  <Card size="small" title="Next" bordered={false}>
                    <List size="small" dataSource={daily.next || []} renderItem={i => (
                      <List.Item>
                        {i}
                        <Button size="small" type="link" onClick={() => onCreateTaskFromDaily?.(i)}>转任务</Button>
                      </List.Item>
                    )} />
                  </Card>
                </Col>
                <Col span={12}>
                  <Card size="small" title="Problems" bordered={false}>
                    <List size="small" dataSource={daily.problems || []} renderItem={i => (
                      <List.Item>
                        {i}
                        <Button size="small" type="link" onClick={() => onCreateTaskFromDaily?.(i)}>转任务</Button>
                      </List.Item>
                    )} />
                  </Card>
                </Col>
                <Col span={12}>
                  <Card size="small" title="Risks" bordered={false}>
                    <List size="small" dataSource={daily.risks || []} renderItem={i => <List.Item>{i}</List.Item>} />
                  </Card>
                </Col>
              </Row>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />
            )}
          </Card>
        </Col>
      </Row>
    </div>
  );
}
