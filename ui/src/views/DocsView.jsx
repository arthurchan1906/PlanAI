import { useCallback, useRef, useState } from "react";
import { Button, Card, Col, Empty, Form, Input, Row, Select, Space, Statistic, Table, Tag, Typography, Alert, Drawer, Spin } from "antd";
import { CheckCircleOutlined, SyncOutlined, DeleteOutlined, WarningOutlined, EyeOutlined, ArrowLeftOutlined, ArrowRightOutlined } from "@ant-design/icons";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import CodeBlock from "../components/CodeBlock";
import DocLink from "../components/DocLink";
import { statusColor, DOC_LAYERS } from "../utils/helpers";
import { api } from "../utils/api";

const { Text } = Typography;

function summarizeLinks(links, direction) {
  return (links || []).map((item) => {
    if (direction === "outgoing") {
      return `${item.relation} -> ${item.target_type}:${item.target_id}`;
    }
    return `${item.source_type}:${item.source_id} -> ${item.relation}`;
  });
}

export default function DocsView({ docs, docAudit, docForm, setDocForm, onSubmitDoc, onSyncDocs, onPruneDocs, busy }) {
  const [readingDoc, setReadingDoc] = useState(null);
  const [content, setContent] = useState("");
  const [loadingContent, setLoadingContent] = useState(false);
  const docHistoryRef = useRef([]);
  const historyIndexRef = useRef(-1);
  const currentDocPathRef = useRef(null);
  const currentDocContentRef = useRef("");
  const [docHistory, setDocHistory] = useState([]);
  const [historyIndex, setHistoryIndex] = useState(-1);

  const selectedDoc = docs.find((item) => item.path === (readingDoc || docForm.path)) || null;
  const outgoingLinks = summarizeLinks(selectedDoc?.links?.outgoing, "outgoing");
  const incomingLinks = summarizeLinks(selectedDoc?.links?.incoming, "incoming");
  const docIssues = selectedDoc?.issues || [];
  const blockingIssueCount =
    (docAudit?.invalid_truth_records?.length || 0) +
    (docAudit?.obsolete_without_replacement?.length || 0) +
    (docAudit?.missing_from_fs?.length || 0) +
    (docAudit?.path_not_normalized?.length || 0);

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

  const navigateToDoc = useCallback((path, newContent) => {
    const currentPath = currentDocPathRef.current;
    if (currentPath) {
      const truncatedHistory = docHistoryRef.current.slice(0, historyIndexRef.current + 1);
      const currentContent = currentDocContentRef.current;
      const newHistory = [...truncatedHistory, { path: currentPath, content: currentContent }, { path, content: newContent }];
      docHistoryRef.current = newHistory;
      historyIndexRef.current = newHistory.length - 1;
      setDocHistory(newHistory);
      setHistoryIndex(newHistory.length - 1);
    } else {
      docHistoryRef.current = [{ path, content: newContent }];
      historyIndexRef.current = 0;
      setDocHistory(docHistoryRef.current);
      setHistoryIndex(0);
    }
    setDocAndSync(path, newContent);
  }, []);

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
    { title: "Path", dataIndex: "path", key: "path" },
    { title: "Layer", dataIndex: "layer", key: "layer", width: 120 },
    {
      title: "Status",
      dataIndex: "status",
      key: "status",
      width: 120,
      render: (value) => <Tag color={statusColor(value)}>{value}</Tag>,
    },
    {
      title: "SOT",
      dataIndex: "source_of_truth",
      key: "source_of_truth",
      width: 80,
      render: (value) => (value ? <CheckCircleOutlined style={{ color: "#52c41a" }} /> : "-"),
    },
    {
      title: "Issues",
      key: "issues",
      width: 220,
      render: (_, record) =>
        record.issues?.length ? record.issues.map((item) => <Tag key={item} color="red">{item}</Tag>) : <Text type="secondary">None</Text>,
    },
    {
      title: "Links",
      key: "links",
      width: 100,
      render: (_, record) => {
        const totalLinks = (record.links?.incoming?.length || 0) + (record.links?.outgoing?.length || 0);
        return totalLinks ? <Tag color="blue">{totalLinks}</Tag> : <Text type="secondary">0</Text>;
      },
    },
    {
      title: "Actions",
      key: "actions",
      width: 100,
      render: (_, record) => (
        <Button
          type="link"
          icon={<EyeOutlined />}
          onClick={(event) => {
            event.stopPropagation();
            handleRead(record.path);
          }}
        >
          Read
        </Button>
      ),
    },
  ];

  const hasConflicts = !!Object.keys(docAudit?.sot_conflicts || {}).length;
  const hasStale = !!(docAudit?.stale_active_records || []).length;
  const hasMissing = !!(docAudit?.missing_from_fs || []).length;
  const hasUntracked = !!(docAudit?.untracked_in_fs || []).length;
  const hasNonNormalized = !!(docAudit?.path_not_normalized || []).length;

  return (
    <div className="view-stack">
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={10}>
          <Card
            className="console-card"
            title="Document State"
            bordered={false}
            extra={
              <Space>
                <Button icon={<SyncOutlined />} onClick={onSyncDocs} loading={busy}>
                  Sync
                </Button>
                <Button danger icon={<DeleteOutlined />} onClick={onPruneDocs} loading={busy}>
                  Prune Archived
                </Button>
              </Space>
            }
          >
            <Form layout="vertical" onFinish={onSubmitDoc}>
              <Form.Item label="Document Path" required>
                <Input
                  placeholder="README.md or docs/rebuild-status.md"
                  value={docForm.path}
                  onChange={(event) => setDocForm((current) => ({ ...current, path: event.target.value }))}
                />
              </Form.Item>
              <Form.Item label="Document Type">
                <Input
                  placeholder="baseline, governance, spec"
                  value={docForm.type}
                  onChange={(event) => setDocForm((current) => ({ ...current, type: event.target.value }))}
                />
              </Form.Item>
              <Row gutter={16}>
                <Col span={12}>
                  <Form.Item label="Layer">
                    <Select
                      value={docForm.layer}
                      onChange={(value) => setDocForm((current) => ({ ...current, layer: value }))}
                      options={DOC_LAYERS.map((layer) => ({ value: layer, label: layer }))}
                    />
                  </Form.Item>
                </Col>
                <Col span={12}>
                  <Form.Item label="Status">
                    <Select
                      value={docForm.status}
                      onChange={(value) => setDocForm((current) => ({ ...current, status: value }))}
                      options={[
                        { value: "draft", label: "Draft" },
                        { value: "active", label: "Active" },
                        { value: "stale", label: "Stale" },
                        { value: "archived", label: "Archived" },
                        { value: "obsolete", label: "Obsolete" },
                      ]}
                    />
                  </Form.Item>
                </Col>
              </Row>
              <Form.Item label="Source Of Truth">
                <Select
                  value={docForm.sourceOfTruth ? "yes" : "no"}
                  onChange={(value) => setDocForm((current) => ({ ...current, sourceOfTruth: value === "yes" }))}
                  options={[{ value: "no", label: "No" }, { value: "yes", label: "Yes" }]}
                />
              </Form.Item>
              <Button type="primary" block htmlType="submit" loading={busy}>
                Save Document State
              </Button>
            </Form>
          </Card>
        </Col>
        <Col xs={24} xl={14}>
          <Card className="console-card" title="Document Governance Audit" bordered={false}>
            {docAudit ? (
              <Space direction="vertical" style={{ width: "100%" }} size="large">
                <Row gutter={[16, 16]}>
                  <Col span={6}><Statistic title="Managed" value={docAudit.total_managed_docs} /></Col>
                  <Col span={6}><Statistic title="Tracked In FS" value={docAudit.tracked_files_in_fs || 0} /></Col>
                  <Col span={6}><Statistic title="Active" value={docAudit.active_records} /></Col>
                  <Col span={6}><Statistic title="Needs Fix" value={blockingIssueCount} valueStyle={{ color: blockingIssueCount ? "#cf1322" : "#3f8600" }} /></Col>
                </Row>

                {hasConflicts && (
                  <Alert
                    message="Source-of-truth conflicts"
                    description={Object.entries(docAudit.sot_conflicts).map(([layer, paths]) => `${layer}: ${paths.join(", ")}`).join(" | ")}
                    type="error"
                    showIcon
                    icon={<WarningOutlined />}
                  />
                )}

                {hasMissing && (
                  <Alert
                    message="Tracked records missing from filesystem"
                    description={docAudit.missing_from_fs.join(", ")}
                    type="error"
                    showIcon
                  />
                )}

                {hasUntracked && (
                  <Alert
                    message="Filesystem documents are not yet governed"
                    description={docAudit.untracked_in_fs.join(", ")}
                    type="warning"
                    showIcon
                  />
                )}

                {hasNonNormalized && (
                  <Alert
                    message="Stored paths need normalization"
                    description={docAudit.path_not_normalized.join(", ")}
                    type="warning"
                    showIcon
                  />
                )}

                {hasStale && (
                  <Alert
                    message="Active records are already superseded"
                    description={docAudit.stale_active_records.join(", ")}
                    type="warning"
                    showIcon
                  />
                )}

                {!hasConflicts && !hasMissing && !hasUntracked && !hasNonNormalized && !hasStale && docAudit.total_managed_docs > 0 && (
                  <Alert message="Governance state looks clean" type="success" showIcon />
                )}
              </Space>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />
            )}
          </Card>
        </Col>
      </Row>

      <Card className="console-card" title="Managed Documents" bordered={false}>
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
              }),
          })}
        />
      </Card>

      {!!selectedDoc && (
        <Card className="console-card" title={`Document Relations: ${selectedDoc.path}`} bordered={false}>
          <Space direction="vertical" style={{ width: "100%" }} size="middle">
            <div>
              <Text strong>Issues:</Text>{" "}
              {docIssues.length ? docIssues.map((item) => <Tag key={item} color="red">{item}</Tag>) : <Text type="secondary">None</Text>}
            </div>
            <div>
              <Text strong>Outgoing Links:</Text>{" "}
              {outgoingLinks.length ? outgoingLinks.map((item) => <Tag key={item} color="blue">{item}</Tag>) : <Text type="secondary">None</Text>}
            </div>
            <div>
              <Text strong>Incoming Links:</Text>{" "}
              {incomingLinks.length ? incomingLinks.map((item) => <Tag key={item} color="cyan">{item}</Tag>) : <Text type="secondary">None</Text>}
            </div>
          </Space>
        </Card>
      )}

      <Drawer
        title={
          <div style={{ display: "flex", alignItems: "center", gap: "12px" }}>
            <Space size="small">
              <Button type="text" size="small" icon={<ArrowLeftOutlined />} onClick={goBack} disabled={historyIndex <= 0} title="Back" />
              <Button type="text" size="small" icon={<ArrowRightOutlined />} onClick={goForward} disabled={historyIndex >= docHistory.length - 1} title="Forward" />
            </Space>
            <div style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{readingDoc}</div>
          </div>
        }
        placement="right"
        width="60%"
        onClose={closeDoc}
        open={!!readingDoc}
        extra={<Button type="text" onClick={closeDoc} title="Close">Close</Button>}
      >
        {loadingContent ? (
          <div style={{ textAlign: "center", padding: "50px" }}><Spin size="large" /></div>
        ) : (
          <div className="markdown-reader" style={{ background: "#fff", padding: "24px", borderRadius: "8px", minHeight: "100%", overflow: "auto" }}>
            {!!selectedDoc && (
              <Space direction="vertical" style={{ width: "100%", marginBottom: 16 }}>
                <div>
                  {docIssues.length ? docIssues.map((item) => <Tag key={item} color="red">{item}</Tag>) : <Tag>no issues</Tag>}
                </div>
                <div>
                  {outgoingLinks.map((item) => <Tag key={item} color="blue">{item}</Tag>)}
                  {incomingLinks.map((item) => <Tag key={item} color="cyan">{item}</Tag>)}
                </div>
              </Space>
            )}
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              rehypePlugins={[rehypeHighlight]}
              components={{
                code({ className, children }) {
                  return <CodeBlock className={className}>{children}</CodeBlock>;
                },
                a({ href, children }) {
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
