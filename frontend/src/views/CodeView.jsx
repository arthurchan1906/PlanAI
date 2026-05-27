import { Button, Card, Col, Divider, List, Row, Space, Spin, Statistic, Tag, Typography } from "antd";
import { BranchesOutlined } from "@ant-design/icons";

const { Text } = Typography;

export default function CodeView({ codeStatus, recentCommits, loading, onCommitFiles, onViewCommit }) {
  if (loading && !codeStatus) return <Spin />;

  const handleCommitFiles = (files) => {
    if (onCommitFiles) {
      onCommitFiles(files);
    }
  };

  const handleViewCommit = (commit) => {
    if (onViewCommit) {
      onViewCommit(commit);
    }
  };

  return (
    <div className="view-stack">
      <Card className="console-card" title="Git 工作区状态" bordered={false} extra={
        <Space>
          <Tag color="blue">{codeStatus?.branch}</Tag>
          {(codeStatus?.staged?.length > 0 || codeStatus?.unstaged?.length > 0) && (
            <Button size="small" type="primary" onClick={() => {
              const allFiles = [...(codeStatus?.staged || []), ...(codeStatus?.unstaged || [])];
              handleCommitFiles(allFiles);
            }}>登记提交</Button>
          )}
        </Space>
      }>
        <Row gutter={16}>
          <Col span={8}>
            <Statistic title="已暂存 (Staged)" value={codeStatus?.staged?.length || 0} />
            <List size="small" dataSource={codeStatus?.staged || []} renderItem={f => (
              <List.Item>
                <Text code>{f}</Text>
              </List.Item>
            )} />
          </Col>
          <Col span={8}>
            <Statistic title="未暂存 (Unstaged)" value={codeStatus?.unstaged?.length || 0} />
            <List size="small" dataSource={codeStatus?.unstaged || []} renderItem={f => (
              <List.Item>
                <Text code type="warning">{f}</Text>
              </List.Item>
            )} />
          </Col>
          <Col span={8}>
            <Statistic title="未追踪 (Untracked)" value={codeStatus?.untracked?.length || 0} />
            <List size="small" dataSource={codeStatus?.untracked || []} renderItem={f => (
              <List.Item>
                <Text code type="secondary">{f}</Text>
              </List.Item>
            )} />
          </Col>
        </Row>
        {(codeStatus?.staged?.length > 0 || codeStatus?.unstaged?.length > 0) && (
          <Divider orientation="left" plain>快速操作</Divider>
        )}
        {codeStatus?.staged?.length > 0 && (
          <Space wrap style={{ marginBottom: 8 }}>
            <Text type="secondary">已暂存文件:</Text>
            {codeStatus.staged.slice(0, 5).map(f => (
              <Tag key={f} closable onClose={() => {}} onClick={() => handleCommitFiles([f])}>{f}</Tag>
            ))}
            {codeStatus.staged.length > 5 && <Text type="secondary">等 {codeStatus.staged.length} 个文件</Text>}
          </Space>
        )}
      </Card>
      <Card className="console-card" title="Git 近期提交历史" bordered={false}>
        <List
          dataSource={recentCommits}
          renderItem={c => (
            <List.Item
              actions={[
                <Button key="view" size="small" type="link" onClick={() => handleViewCommit(c)}>
                  查看详情
                </Button>
              ]}
            >
              <List.Item.Meta
                avatar={<BranchesOutlined />}
                title={<Text strong>{c.title}</Text>}
                description={
                  <Space direction="vertical" size={2}>
                    <Text type="secondary">{c.author} · {c.timestamp}</Text>
                    <Text code>{c.commit_hash}</Text>
                    <div className="tag-wrap">
                      {(c.files || []).slice(0, 5).map(f => <Tag key={f} size="small">{f}</Tag>)}
                      {(c.files || []).length > 5 && <Text type="secondary">等 {(c.files || []).length} 个文件</Text>}
                    </div>
                  </Space>
                }
              />
            </List.Item>
          )}
        />
      </Card>
    </div>
  );
}
