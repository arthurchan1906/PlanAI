import { useState } from "react";
import { Alert, Button, Card, Col, Empty, Form, Input, Row, Select, Space, Statistic, Table, Tag, Typography } from "antd";
import {
  CheckCircleOutlined,
  DeleteOutlined,
  EyeOutlined,
  SyncOutlined,
  ToolOutlined,
  WarningOutlined,
} from "@ant-design/icons";
import DocumentDrawer from "../components/DocumentDrawer";
import { DOC_LAYERS, statusColor } from "../utils/helpers";

const { Text } = Typography;

function summarizeLinks(links, direction) {
  return (links || []).map((item) => {
    if (direction === "outgoing") {
      return `${item.relation} -> ${item.target_type}:${item.target_id}`;
    }
    return `${item.source_type}:${item.source_id} -> ${item.relation}`;
  });
}

export default function DocsView({
  docs,
  docAudit,
  docForm,
  setDocForm,
  onSubmitDoc,
  onSyncDocs,
  onRepairDocs,
  onPruneDocs,
  busy,
}) {
  const [readingDoc, setReadingDoc] = useState(null);
  const selectedDoc = docs.find((item) => item.path === (readingDoc || docForm.path)) || null;
  const outgoingLinks = summarizeLinks(selectedDoc?.links?.outgoing, "outgoing");
  const incomingLinks = summarizeLinks(selectedDoc?.links?.incoming, "incoming");
  const docIssues = selectedDoc?.issues || [];
  const blockingIssueCount =
    (docAudit?.invalid_truth_records?.length || 0) +
    (docAudit?.obsolete_without_replacement?.length || 0) +
    (docAudit?.missing_from_fs?.length || 0) +
    (docAudit?.path_not_normalized?.length || 0);

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
            setReadingDoc(record.path);
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
              <Space wrap>
                <Button icon={<SyncOutlined />} onClick={onSyncDocs} loading={busy}>
                  Sync
                </Button>
                <Button icon={<ToolOutlined />} onClick={onRepairDocs} loading={busy}>
                  Repair Records
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

      <DocumentDrawer docs={docs} path={readingDoc} open={!!readingDoc} onClose={() => setReadingDoc(null)} />
    </div>
  );
}
