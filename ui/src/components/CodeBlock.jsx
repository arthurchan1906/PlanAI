import { useState } from "react";
import { Button, Space, message } from "antd";
import { CopyOutlined } from "@ant-design/icons";
import MermaidRenderer from "./MermaidRenderer";

// 自定义代码块组件
export default function CodeBlock({ className, children, inline, ...props }) {
  const [showMermaid, setShowMermaid] = useState(false);

  // 如果是行内代码，直接返回简单样式
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

  // 提取语言类型
  const match = /language-(\w+)/.exec(className || "");
  const language = match ? match[1] : "";
  const isMermaid = language === "mermaid";

  // 获取代码文本
  const codeText = String(children).replace(/\n$/, "");

  // 复制代码功能
  const handleCopy = () => {
    navigator.clipboard.writeText(codeText);
    message.success("代码已复制到剪贴板");
  };

  // 如果是 mermaid 代码，提供渲染选项
  if (isMermaid) {
    return (
      <div style={{ margin: "1.5em 0" }}>
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            padding: "8px 16px",
            background: "#2d3748",
            borderRadius: "12px 12px 0 0",
            borderBottom: "1px solid #4a5568",
          }}
        >
          <span style={{ color: "#a0aec0", fontSize: "13px", fontWeight: "600" }}>
            📊 Mermaid 图表
          </span>
          <Space>
            <Button
              size="small"
              icon={<CopyOutlined />}
              onClick={handleCopy}
              style={{ background: "#4a5568", color: "#e2e8f0", border: "none" }}
            >
              复制代码
            </Button>
            <Button
              size="small"
              type="primary"
              onClick={() => setShowMermaid(!showMermaid)}
            >
              {showMermaid ? "隐藏图表" : "渲染图表"}
            </Button>
          </Space>
        </div>
        <pre
          style={{
            margin: 0,
            padding: "1.5em",
            background: "linear-gradient(135deg, #1a202c 0%, #2d3748 100%)",
            borderRadius: showMermaid ? "0 0 12px 12px" : "0 0 12px 12px",
            overflow: "auto",
          }}
        >
          <code className={className}>{children}</code>
        </pre>
        {showMermaid && (
          <div
            style={{
              marginTop: "12px",
              padding: "16px",
              background: "#f7fafc",
              borderRadius: "12px",
              border: "1px solid #e2e8f0",
            }}
          >
            <MermaidRenderer chart={codeText} />
          </div>
        )}
      </div>
    );
  }

  // 普通代码块（有多行内容时）
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
          <span style={{ color: "#a0aec0", fontSize: "12px", fontWeight: "600" }}>
            {language || "code"}
          </span>
          <Button
            size="small"
            icon={<CopyOutlined />}
            onClick={handleCopy}
            style={{ background: "#4a5568", color: "#e2e8f0", border: "none" }}
          >
            复制
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

  // 如果没有 className，可能是普通文本，直接返回
  return <code className={className}>{children}</code>;
}
