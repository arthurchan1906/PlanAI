import { useState } from "react";
import { Button, Space, message } from "antd";
import { CopyOutlined } from "@ant-design/icons";
import MermaidRenderer from "./MermaidRenderer";

export default function CodeBlock({ className, children, inline, ...props }) {
  const [showMermaid, setShowMermaid] = useState(true);
  const [showSource, setShowSource] = useState(false);

  if (inline) {
    return (
      <code
        style={{
          fontFamily: "'JetBrains Mono', 'Fira Code', 'SF Mono', 'Cascadia Code', 'Consolas', monospace",
          fontSize: "0.9em",
          padding: "0.2em 0.4em",
          background: "#f7fafc",
          border: "1px solid #e2e8f0",
          borderRadius: "4px",
          color: "#e53e3e",
          fontWeight: 500,
        }}
        className={className}
        {...props}
      >
        {children}
      </code>
    );
  }

  const match = /language-([\w-]+)/.exec(className || "");
  const language = match ? match[1].toLowerCase() : "";
  const isMermaid = language === "mermaid";
  const codeText = String(children ?? "").replace(/\n$/, "");

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(codeText);
      message.success("Code copied");
    } catch (_) {
      message.error("Copy failed");
    }
  };

  if (isMermaid) {
    return (
      <div style={{ margin: "1.5em 0" }}>
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            gap: "8px",
            padding: "8px 16px",
            background: "#2d3748",
            borderRadius: "12px 12px 0 0",
            borderBottom: "1px solid #4a5568",
          }}
        >
          <span style={{ color: "#a0aec0", fontSize: "13px", fontWeight: 600 }}>Mermaid</span>
          <Space>
            <Button size="small" icon={<CopyOutlined />} onClick={handleCopy}>
              Copy
            </Button>
            <Button size="small" onClick={() => setShowSource((v) => !v)}>
              {showSource ? "Hide source" : "Show source"}
            </Button>
            <Button size="small" type="primary" onClick={() => setShowMermaid((v) => !v)}>
              {showMermaid ? "Hide diagram" : "Show diagram"}
            </Button>
          </Space>
        </div>
        {showMermaid && (
          <div
            style={{
              marginTop: 0,
              padding: "16px",
              background: "#f7fafc",
              borderRadius: showSource ? 0 : "0 0 12px 12px",
              border: "1px solid #e2e8f0",
              borderTop: "none",
            }}
          >
            <MermaidRenderer chart={codeText} />
          </div>
        )}
        {showSource && (
          <pre
            style={{
              margin: 0,
              padding: "1.2em",
              background: "linear-gradient(135deg, #1a202c 0%, #2d3748 100%)",
              borderRadius: "0 0 12px 12px",
              overflow: "auto",
            }}
          >
            <code className={className}>{children}</code>
          </pre>
        )}
      </div>
    );
  }

  if (className) {
    return (
      <div style={{ margin: "1.5em 0", position: "relative" }}>
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            padding: "6px 16px",
            background: "#2d3748",
            borderRadius: "12px 12px 0 0",
            borderBottom: "1px solid #4a5568",
          }}
        >
          <span style={{ color: "#a0aec0", fontSize: "12px", fontWeight: 600 }}>{language || "code"}</span>
          <Button size="small" icon={<CopyOutlined />} onClick={handleCopy}>
            Copy
          </Button>
        </div>
        <pre
          style={{
            margin: 0,
            padding: "1.5em",
            background: "linear-gradient(135deg, #1a202c 0%, #2d3748 100%)",
            borderRadius: "0 0 12px 12px",
            overflow: "auto",
          }}
        >
          <code className={className}>{children}</code>
        </pre>
      </div>
    );
  }

  return <code className={className}>{children}</code>;
}
