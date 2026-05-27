import { useCallback, useEffect, useRef, useState } from "react";
import { Button, Drawer, Space, Spin, Tag } from "antd";
import { ArrowLeftOutlined, ArrowRightOutlined } from "@ant-design/icons";
import ReactMarkdown from "react-markdown";
import rehypeHighlight from "rehype-highlight";
import remarkGfm from "remark-gfm";
import CodeBlock from "./CodeBlock";
import DocLink from "./DocLink";
import { api } from "../utils/api";

function summarizeLinks(links, direction) {
  return (links || []).map((item) => {
    if (direction === "outgoing") {
      return `${item.relation} -> ${item.target_type}:${item.target_id}`;
    }
    return `${item.source_type}:${item.source_id} -> ${item.relation}`;
  });
}

export default function DocumentDrawer({ docs = [], path, open, onClose }) {
  const [readingDoc, setReadingDoc] = useState(null);
  const [content, setContent] = useState("");
  const [loadingContent, setLoadingContent] = useState(false);
  const docHistoryRef = useRef([]);
  const historyIndexRef = useRef(-1);
  const currentDocPathRef = useRef(null);
  const currentDocContentRef = useRef("");
  const [docHistory, setDocHistory] = useState([]);
  const [historyIndex, setHistoryIndex] = useState(-1);

  const selectedDoc = docs.find((item) => item.path === readingDoc) || null;
  const outgoingLinks = summarizeLinks(selectedDoc?.links?.outgoing, "outgoing");
  const incomingLinks = summarizeLinks(selectedDoc?.links?.incoming, "incoming");
  const docIssues = selectedDoc?.issues || [];
  const hasMeta = !!(docIssues.length || outgoingLinks.length || incomingLinks.length);

  const setDocAndSync = useCallback((nextPath, newContent) => {
    currentDocPathRef.current = nextPath;
    currentDocContentRef.current = newContent;
    setReadingDoc(nextPath);
    setContent(newContent);
  }, []);

  const resetState = useCallback(() => {
    setReadingDoc(null);
    setContent("");
    currentDocPathRef.current = null;
    currentDocContentRef.current = "";
    docHistoryRef.current = [];
    historyIndexRef.current = -1;
    setDocHistory([]);
    setHistoryIndex(-1);
  }, []);

  const handleRead = useCallback(async (nextPath) => {
    setLoadingContent(true);
    try {
      const data = await api(`/pmai/docs/content?path=${encodeURIComponent(nextPath)}`);
      docHistoryRef.current = [{ path: nextPath, content: data.content }];
      historyIndexRef.current = 0;
      setDocHistory(docHistoryRef.current);
      setHistoryIndex(0);
      setDocAndSync(nextPath, data.content);
    } catch (error) {
      const errorMessage = `Error loading document: ${error.message}`;
      docHistoryRef.current = [{ path: nextPath, content: errorMessage }];
      historyIndexRef.current = 0;
      setDocHistory(docHistoryRef.current);
      setHistoryIndex(0);
      setDocAndSync(nextPath, errorMessage);
    } finally {
      setLoadingContent(false);
    }
  }, [setDocAndSync]);

  useEffect(() => {
    if (!open || !path) {
      if (!open) {
        resetState();
      }
      return;
    }
    if (path !== currentDocPathRef.current) {
      handleRead(path);
    }
  }, [handleRead, open, path, resetState]);

  const navigateToDoc = useCallback((nextPath, newContent) => {
    const currentPath = currentDocPathRef.current;
    if (currentPath) {
      const truncatedHistory = docHistoryRef.current.slice(0, historyIndexRef.current + 1);
      const currentContent = currentDocContentRef.current;
      const newHistory = [...truncatedHistory, { path: currentPath, content: currentContent }, { path: nextPath, content: newContent }];
      docHistoryRef.current = newHistory;
      historyIndexRef.current = newHistory.length - 1;
      setDocHistory(newHistory);
      setHistoryIndex(newHistory.length - 1);
    } else {
      docHistoryRef.current = [{ path: nextPath, content: newContent }];
      historyIndexRef.current = 0;
      setDocHistory(docHistoryRef.current);
      setHistoryIndex(0);
    }
    setDocAndSync(nextPath, newContent);
  }, [setDocAndSync]);

  const goBack = useCallback(() => {
    if (historyIndexRef.current > 0) {
      const newIndex = historyIndexRef.current - 1;
      const targetDoc = docHistoryRef.current[newIndex];
      if (!targetDoc) {
        return;
      }
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
      if (!targetDoc) {
        return;
      }
      historyIndexRef.current = newIndex;
      currentDocPathRef.current = targetDoc.path;
      currentDocContentRef.current = targetDoc.content;
      setHistoryIndex(newIndex);
      setReadingDoc(targetDoc.path);
      setContent(targetDoc.content);
    }
  }, []);

  const closeDrawer = useCallback(() => {
    resetState();
    onClose?.();
  }, [onClose, resetState]);

  return (
    <Drawer
      title={(
        <div style={{ display: "flex", alignItems: "center", gap: "12px" }}>
          <Space size="small">
            <Button type="text" size="small" icon={<ArrowLeftOutlined />} onClick={goBack} disabled={historyIndex <= 0} title="Back" />
            <Button type="text" size="small" icon={<ArrowRightOutlined />} onClick={goForward} disabled={historyIndex >= docHistory.length - 1} title="Forward" />
          </Space>
          <div style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{readingDoc || path}</div>
        </div>
      )}
      placement="right"
      width="60%"
      onClose={closeDrawer}
      open={open}
      extra={<Button type="text" onClick={closeDrawer} title="Close">Close</Button>}
    >
      {loadingContent ? (
        <div style={{ textAlign: "center", padding: "50px" }}><Spin size="large" /></div>
      ) : (
        <div className="markdown-reader" style={{ background: "#fff", padding: "24px", borderRadius: "8px", minHeight: "100%", overflow: "auto" }}>
          {!!selectedDoc && hasMeta && (
            <Space direction="vertical" style={{ width: "100%", marginBottom: 16 }}>
              {!!docIssues.length && (
                <div>
                  {docIssues.map((item) => <Tag key={item} color="red">{item}</Tag>)}
                </div>
              )}
              {!!(outgoingLinks.length || incomingLinks.length) && (
                <div>
                  {outgoingLinks.map((item) => <Tag key={item} color="blue">{item}</Tag>)}
                  {incomingLinks.map((item) => <Tag key={item} color="cyan">{item}</Tag>)}
                </div>
              )}
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
  );
}
