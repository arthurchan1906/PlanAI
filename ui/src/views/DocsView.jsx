import { useState, useRef, useCallback } from "react";
import { Button, Card, Col, Empty, Form, Input, Row, Select, Space, Statistic, Table, Tag, Typography, Alert, Drawer, Spin, Breadcrumb } from "antd";
import { CheckCircleOutlined, SyncOutlined, DeleteOutlined, WarningOutlined, EyeOutlined, ArrowLeftOutlined, ArrowRightOutlined } from "@ant-design/icons";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import CodeBlock from "../components/CodeBlock";
import DocLink from "../components/DocLink";
import { statusColor, DOC_LAYERS } from "../utils/helpers";
import { api } from "../utils/api";

const { Text, Title, Paragraph } = Typography;

export default function DocsView({ docs, docAudit, docForm, setDocForm, onSubmitDoc, onSyncDocs, onPruneDocs, busy, decisions }) {
  const [readingDoc, setReadingDoc] = useState(null);
  const [content, setContent] = useState("");
  const [loadingContent, setLoadingContent] = useState(false);
  
  // 使用 ref 跟踪历史，避免闭包问题
  const docHistoryRef = useRef([]);
  const historyIndexRef = useRef(-1);
  
  // 使用 ref 存储当前文档和当前内容，避免闭包问题
  const currentDocPathRef = useRef(null);
  const currentDocContentRef = useRef("");
  
  // 同时使用 state 触发重渲染
  const [docHistory, setDocHistory] = useState([]);
  const [historyIndex, setHistoryIndex] = useState(-1);

  // 同步 ref 和 state
  const setDocAndSync = (path, newContent) => {
    currentDocPathRef.current = path;
    currentDocContentRef.current = newContent;
    setReadingDoc(path);
    setContent(newContent);
  };

  const handleRead = async (path) => {
    setLoadingContent(true);
    try {
      const data = await api(`/pmai/docs/content?path=${encodeURIComponent(path)}`);
      
      // 首次打开，清空历史并设置
      docHistoryRef.current = [{ path, content: data.content }];
      historyIndexRef.current = 0;
      setDocHistory(docHistoryRef.current);
      setHistoryIndex(0);
      currentDocPathRef.current = path;
      currentDocContentRef.current = data.content;
      
      setReadingDoc(path);
      setContent(data.content);
    } catch (error) {
      const errorMessage = `Error loading document: ${error.message}`;
      docHistoryRef.current = [{ path, content: errorMessage }];
      historyIndexRef.current = 0;
      setDocHistory(docHistoryRef.current);
      setHistoryIndex(0);
      currentDocPathRef.current = path;
      currentDocContentRef.current = errorMessage;
      
      setReadingDoc(path);
      setContent(errorMessage);
    } finally {
      setLoadingContent(false);
    }
  };

  // 导航到新文档
  const navigateToDoc = useCallback((path, newContent) => {
    const currentPath = currentDocPathRef.current;
    
    if (currentPath) {
      const truncatedHistory = docHistoryRef.current.slice(0, historyIndexRef.current + 1);
      const currentContent = currentDocContentRef.current;
      const newHistory = [...truncatedHistory, { path: currentPath, content: currentContent }];
      const finalHistory = [...newHistory, { path, content: newContent }];
      
      docHistoryRef.current = finalHistory;
      historyIndexRef.current = finalHistory.length - 1;
      setDocHistory(finalHistory);
      setHistoryIndex(finalHistory.length - 1);
    } else {
      docHistoryRef.current = [{ path, content: newContent }];
      historyIndexRef.current = 0;
      setDocHistory(docHistoryRef.current);
      setHistoryIndex(0);
    }
    
    setDocAndSync(path, newContent);
  }, []);

  // 后退
  const goBack = useCallback(() => {
    if (historyIndexRef.current > 0) {
      const newIndex = historyIndexRef.current - 1;
      const targetDoc = docHistoryRef.current[newIndex];
      
      if (!targetDoc) return;
      
      historyIndexRef.current = newIndex;
      currentDocPathRef.current = targetDoc.path;
      currentDocContentRef.current = targetDoc.content;
      setHistoryIndex(newIndex);
      setReadingDoc(targetDoc.path);
      setContent(targetDoc.content);
    }
  }, []);

  // 前进
  const goForward = useCallback(() => {
    const newIndex = historyIndexRef.current + 1;
    if (newIndex < docHistoryRef.current.length) {
      const targetDoc = docHistoryRef.current[newIndex];
      
      if (!targetDoc) return;
      
      historyIndexRef.current = newIndex;
      currentDocPathRef.current = targetDoc.path;
      currentDocContentRef.current = targetDoc.content;
      setHistoryIndex(newIndex);
      setReadingDoc(targetDoc.path);
      setContent(targetDoc.content);
    }
  }, []);

  // 关闭文档
  const closeDoc = () => {
    setReadingDoc(null);
    setContent("");
    currentDocPathRef.current = null;
    currentDocContentRef.current = "";
    docHistoryRef.current = [];
    historyIndexRef.current = -1;
    setDocHistory([]);
    setHistoryIndex(-1);
  };

  const columns = [
    { title: "路径", dataIndex: "path", key: "path" },
    { title: "层级", dataIndex: "layer", key: "layer" },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      render: (value) => <Tag color={statusColor(value)}>{value}</Tag>,
    },
    {
      title: "真相",
      dataIndex: "source_of_truth",
      key: "source_of_truth",
      render: (value) => (value ? <CheckCircleOutlined style={{ color: '#52c41a' }} /> : "-"),
    },
    {
      title: "操作",
      key: "actions",
      render: (_, record) => (
        <Button 
          type="link" 
          icon={<EyeOutlined />} 
          onClick={(e) => {
            e.stopPropagation();
            handleRead(record.path);
          }}
        >
          阅读
        </Button>
      )
    }
  ];

  const hasConflicts = docAudit && Object.keys(docAudit.sot_conflicts || {}).length > 0;
  const hasStale = docAudit && (docAudit.stale_active_records || []).length > 0;

  return (
    <div className="view-stack">
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={10}>
          <Card 
            className="console-card" 
            title="文档状态管理" 
            bordered={false}
            extra={
              <Space>
                <Button 
                  icon={<SyncOutlined />} 
                  onClick={onSyncDocs} 
                  loading={busy}
                >
                  同步目录
                </Button>
                <Button 
                  danger 
                  icon={<DeleteOutlined />} 
                  onClick={onPruneDocs} 
                  loading={busy}
                >
                  清理归档
                </Button>
              </Space>
            }
          >
            <Form layout="vertical" onFinish={onSubmitDoc}>
              <Form.Item label="文档路径 (相对 doc/)" required>
                <Input
                  placeholder="e.g. architecture/data-model.md"
                  value={docForm.path}
                  onChange={(event) => setDocForm((current) => ({ ...current, path: event.target.value }))}
                />
              </Form.Item>
              <Row gutter={16}>
                <Col span={12}>
                  <Form.Item label="层级">
                    <Select
                      value={docForm.layer}
                      onChange={(value) => setDocForm((current) => ({ ...current, layer: value }))}
                      options={DOC_LAYERS.map((layer) => ({ value: layer, label: layer }))}
                    />
                  </Form.Item>
                </Col>
                <Col span={12}>
                  <Form.Item label="状态">
                    <Select
                      value={docForm.status}
                      onChange={(value) => setDocForm((current) => ({ ...current, status: value }))}
                      options={[
                        { value: "draft", label: "Draft (草案)" },
                        { value: "active", label: "Active (活跃)" },
                        { value: "stale", label: "Stale (陈旧)" },
                        { value: "archived", label: "Archived (已归档)" },
                        { value: "obsolete", label: "Obsolete (废弃)" },
                      ]}
                    />
                  </Form.Item>
                </Col>
              </Row>
              <Form.Item label="关联决策">
                <Select
                  allowClear
                  value={docForm.relatedDecisionId || undefined}
                  placeholder="选择关联决策（可选）"
                  onChange={(value) =>
                    setDocForm((current) => ({ ...current, relatedDecisionId: value }))
                  }
                  options={(decisions || []).map((d) => ({ value: d.id, label: d.title }))}
                />
              </Form.Item>
              <Form.Item label="标记为单一事实来源 (SOT)">
                <Select
                  value={docForm.sourceOfTruth ? "yes" : "no"}
                  onChange={(value) =>
                    setDocForm((current) => ({ ...current, sourceOfTruth: value === "yes" }))
                  }
                  options={[{ value: "no", label: "否" }, { value: "yes", label: "是" }]}
                />
              </Form.Item>
              <Button type="primary" block htmlType="submit" loading={busy}>保存文档状态</Button>
            </Form>
          </Card>
        </Col>
        <Col xs={24} xl={14}>
          <Card className="console-card" title="文档治理审计" bordered={false}>
            {docAudit ? (
              <Space direction="vertical" style={{ width: '100%' }} size="large">
                <Row gutter={[16, 16]}>
                  <Col span={6}><Statistic title="管理总数" value={docAudit.total_managed_docs} /></Col>
                  <Col span={6}><Statistic title="活跃文档" value={docAudit.active_records} /></Col>
                  <Col span={6}><Statistic title="真相来源" value={docAudit.source_of_truth_records} valueStyle={{ color: '#3f8600' }} /></Col>
                  <Col span={6}><Statistic title="待修复" value={(docAudit.invalid_truth_records?.length || 0) + (docAudit.obsolete_without_replacement?.length || 0)} valueStyle={{ color: '#cf1322' }} /></Col>
                </Row>
                
                {hasConflicts && (
                  <Alert
                    message="SOT 冲突警告"
                    description={
                      <ul style={{ margin: 0, paddingLeft: 20 }}>
                        {Object.entries(docAudit.sot_conflicts).map(([layer, paths]) => (
                          <li key={layer}>
                            <Text strong>{layer}</Text> 层级存在多个真相来源: {paths.join(', ')}
                          </li>
                        ))}
                      </ul>
                    }
                    type="error"
                    showIcon
                    icon={<WarningOutlined />}
                  />
                )}

                {hasStale && (
                  <Alert
                    message="活跃文档已陈旧"
                    description={`以下文档被标记为 Active，但已有替代者：${docAudit.stale_active_records.join(', ')}`}
                    type="warning"
                    showIcon
                  />
                )}

                {!hasConflicts && !hasStale && docAudit.total_managed_docs > 0 && (
                  <Alert message="治理状态良好" type="success" showIcon />
                )}
              </Space>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />
            )}
          </Card>
        </Col>
      </Row>

      <Card className="console-card" title="文档治理清单 (doc/ 目录受管文件)" bordered={false}>
        <Table
          rowKey="path"
          columns={columns}
          dataSource={docs}
          pagination={{ pageSize: 10 }}
          onRow={(record) => ({
            onClick: () =>
              setDocForm({
                path: record.path || "",
                type: record.type || "",
                status: record.status || "draft",
                layer: record.layer || "exploration",
                sourceOfTruth: !!record.source_of_truth,
                relatedDecisionId: record.related_decision_id || undefined,
              }),
          })}
        />
      </Card>

      <Drawer
        title={
          <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
            <Space size="small">
              <Button
                type="text"
                size="small"
                icon={<ArrowLeftOutlined />}
                onClick={goBack}
                disabled={historyIndex <= 0}
                title="后退"
              />
              <Button
                type="text"
                size="small"
                icon={<ArrowRightOutlined />}
                onClick={goForward}
                disabled={historyIndex >= docHistory.length - 1}
                title="前进"
              />
            </Space>
            <div style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {readingDoc}
            </div>
          </div>
        }
        placement="right"
        width="60%"
        onClose={closeDoc}
        open={!!readingDoc}
        extra={
          <Button type="text" onClick={closeDoc} title="关闭">
            ✕
          </Button>
        }
      >
        {loadingContent ? (
          <div style={{ textAlign: 'center', padding: '50px' }}><Spin size="large" /></div>
        ) : (
          <div className="markdown-reader" style={{ background: '#fff', padding: '24px', borderRadius: '8px', minHeight: '100%', overflow: 'auto' }}>
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              rehypePlugins={[rehypeHighlight]}
              components={{
                code({ className, children, inline, ...props }) {
                  return (
                    <CodeBlock className={className} inline={inline} {...props}>
                      {children}
                    </CodeBlock>
                  );
                },
                a({ href, children, ...props }) {
                  return (
                    <DocLink href={href} currentPath={readingDoc} docList={docs} onNavigate={navigateToDoc}>
                      {children}
                    </DocLink>
                  );
                },
              }}
            >
              {content}
            </ReactMarkdown>
          </div>
        )}
      </Drawer>
    </div>
  );
}
