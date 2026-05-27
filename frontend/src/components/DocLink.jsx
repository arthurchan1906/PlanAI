import { useState } from "react";
import { Spin, message } from "antd";
import { api } from "../utils/api";

/**
 * 规范化路径（处理 ../ 和 ./）
 */
function normalizePath(path) {
  const parts = path.split('/').filter(p => p);
  const normalized = [];
  for (const part of parts) {
    if (part === '..') {
      normalized.pop();
    } else if (part !== '.') {
      normalized.push(part);
    }
  }
  return normalized.join('/');
}

/**
 * 计算可能的文档路径
 * 返回多个候选路径，按优先级排序
 */
function calculateCandidatePaths(href, currentPath) {
  const candidates = [];
  
  // 清理链接
  let cleanPath = href
    .replace(/^\.\//, '')
    .replace(/^\//, '')
    .replace(/#.*$/, '');

  // 候选 1: 精确路径（直接拼接）
  if (cleanPath.startsWith('doc/') || cleanPath.startsWith('docs/')) {
    candidates.push(cleanPath);
  } else if (currentPath) {
    // 基于当前文档目录
    const currentDir = currentPath.includes('/') 
      ? currentPath.substring(0, currentPath.lastIndexOf('/') + 1) 
      : '';
    candidates.push(normalizePath(currentDir + cleanPath));
  } else {
    // 默认在 doc/ 下
    candidates.push('doc/' + cleanPath);
    candidates.push(cleanPath);
  }

  // 候选 2: 假设在 doc/ 目录下
  if (!cleanPath.startsWith('doc/')) {
    candidates.push('doc/' + cleanPath);
  }

  // 候选 3: 提取纯文件名，用于模糊匹配
  const fileName = cleanPath.split('/').pop();
  if (fileName) {
    candidates.push(fileName); // 纯文件名
    candidates.push('doc/' + fileName); // doc/ 下同名文件
  }

  // 去重
  return [...new Set(candidates)].filter(Boolean);
}

/**
 * 从文档列表中模糊匹配
 */
function fuzzyMatch(docs, fileName, candidates) {
  if (!docs || docs.length === 0) return null;

  // 先尝试精确匹配候选路径
  for (const candidate of candidates) {
    const found = docs.find(d => d.path === candidate || d.path.endsWith('/' + candidate));
    if (found) return found.path;
  }

  // 模糊匹配：文件名包含
  const lowerFileName = fileName.toLowerCase().replace(/\.md$/, '');
  const matches = docs.filter(d => {
    const docName = d.path.split('/').pop().toLowerCase();
    return docName === lowerFileName || 
           docName === lowerFileName + '.md' ||
           docName.includes(lowerFileName);
  });

  if (matches.length === 1) {
    return matches[0].path;
  } else if (matches.length > 1) {
    // 多个匹配，优先返回与当前文档同目录的
    // 或者返回第一个
    return matches[0].path;
  }

  return null;
}

// 自定义链接组件
export default function DocLink({ href, children, onNavigate, currentPath, docList }) {
  const [loading, setLoading] = useState(false);

  const handleClick = async (e) => {
    e.preventDefault();

    // 判断是否是内部文档链接
    const isInternalDoc = href && (
      href.endsWith('.md') ||
      (href.startsWith('doc/') && !href.startsWith('http')) ||
      (!href.startsWith('http') && !href.startsWith('#') && !href.startsWith('mailto:'))
    );

    if (!isInternalDoc) {
      // 外部链接或锚点，正常打开
      if (href && !href.startsWith('#')) {
        window.open(href, '_blank', 'noopener,noreferrer');
      }
      return;
    }

    // 处理内部文档链接
    setLoading(true);
    try {
      // 计算候选路径
      const candidates = calculateCandidatePaths(href, currentPath);
      
      let targetPath = null;

      // 策略 1: 尝试精确路径（按优先级）
      for (const candidate of candidates) {
        try {
          const data = await api(`/pmai/docs/content?path=${encodeURIComponent(candidate)}`);
          if (data && data.content) {
            targetPath = candidate;
            onNavigate(targetPath, data.content);
            setLoading(false);
            return;
          }
        } catch (err) {
          // 404 或其他错误，继续尝试下一个
          continue;
        }
      }

      // 策略 2: 模糊匹配（从文档列表中查找）
      const fileName = href.split('/').pop().replace(/#.*$/, '');
      targetPath = fuzzyMatch(docList, fileName, candidates);

      if (targetPath) {
        try {
          const data = await api(`/pmai/docs/content?path=${encodeURIComponent(targetPath)}`);
          if (data && data.content) {
            onNavigate(targetPath, data.content);
            setLoading(false);
            return;
          }
        } catch (err) {
          console.warn(`模糊匹配找到路径 ${targetPath} 但加载失败:`, err);
        }
      }

      // 所有策略都失败
      message.error(`无法找到文档: ${href}`);
    } catch (error) {
      console.error('加载文档失败:', error);
      message.error(`加载文档失败: ${error.message || '未知错误'}`);
    } finally {
      setLoading(false);
    }
  };

  if (loading) {
    return (
      <span style={{ color: '#1890ff', cursor: 'pointer' }}>
        <Spin size="small" /> 加载中...
      </span>
    );
  }

  return (
    <a
      href={href}
      onClick={handleClick}
      className="doc-link"
      style={{
        color: '#3182ce',
        textDecoration: 'none',
        borderBottom: '1px dashed #3182ce',
        cursor: 'pointer',
      }}
    >
      {children}
    </a>
  );
}
