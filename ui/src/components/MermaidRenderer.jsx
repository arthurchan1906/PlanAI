import { useEffect, useState } from "react";
import { Spin } from "antd";
import mermaid from "mermaid";

// 初始化 mermaid
mermaid.initialize({
  startOnLoad: false,
  theme: "default",
  securityLevel: "loose",
  fontFamily: '"PingFang SC", "Microsoft YaHei", sans-serif',
});

export default function MermaidRenderer({ chart }) {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [svg, setSvg] = useState(null);

  useEffect(() => {
    let mounted = true;

    async function renderMermaid() {
      if (!chart || !String(chart).trim()) {
        if (mounted) {
          setSvg(null);
          setError(null);
          setLoading(false);
        }
        return;
      }

      setLoading(true);
      setError(null);

      try {
        // 验证图表语法
        const isValid = await mermaid.parse(chart);
        if (!isValid) {
          throw new Error("Mermaid 图表语法无效");
        }

        // 生成唯一 ID
        const id = `mermaid-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;

        // 渲染 SVG
        const { svg: renderedSvg } = await mermaid.render(id, chart);
        
        if (mounted) {
          setSvg(renderedSvg);
          setLoading(false);
        }
      } catch (err) {
        if (mounted) {
          setError(err.message || "Mermaid 图表渲染失败");
          setLoading(false);
        }
      }
    }

    renderMermaid();

    return () => {
      mounted = false;
    };
  }, [chart]);

  if (loading) {
    return (
      <div style={{ textAlign: "center", padding: "20px" }}>
        <Spin tip="渲染图表..." />
      </div>
    );
  }

  if (error) {
    return (
      <div
        style={{
          padding: "16px",
          background: "#fff2f0",
          border: "1px solid #ffccc7",
          borderRadius: "8px",
          color: "#cf1322",
        }}
      >
        <strong>⚠️ Mermaid 渲染错误：</strong>
        <pre style={{ margin: "8px 0 0", fontSize: "13px", whiteSpace: "pre-wrap" }}>
          {error}
        </pre>
      </div>
    );
  }

  return (
    <div
      className="mermaid-diagram"
      style={{
        margin: "1.5em 0",
        padding: "20px",
        background: "linear-gradient(135deg, #f7fafc 0%, #edf2f7 100%)",
        borderRadius: "12px",
        border: "1px solid #e2e8f0",
        textAlign: "center",
        boxShadow: "0 2px 8px rgba(0, 0, 0, 0.05)",
      }}
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  );
}
