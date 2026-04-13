// 自动拆分 App.jsx 的脚本
const fs = require('fs');
const path = require('path');

const srcDir = path.join(__dirname, '..', 'src');
const viewsDir = path.join(srcDir, 'views');
const appFile = path.join(srcDir, 'App.jsx');

// 读取原始 App.jsx
const content = fs.readFileSync(appFile, 'utf8');
const lines = content.split('\n');

console.log(`原始 App.jsx 有 ${lines.length} 行`);

// 找出所有视图函数
const viewFunctions = [];
let currentFunc = null;
let braceCount = 0;
let startLine = 0;

for (let i = 0; i < lines.length; i++) {
  const line = lines[i];
  
  // 检测视图函数开始
  const match = line.match(/^function (\w+)\s*\(/);
  if (match && !line.includes('ConsoleApp') && !line.includes('export default')) {
    if (currentFunc) {
      currentFunc.endLine = i - 1;
      viewFunctions.push(currentFunc);
    }
    currentFunc = {
      name: match[1],
      startLine: i,
      code: []
    };
    startLine = i;
    braceCount = 0;
  }
  
  if (currentFunc) {
    currentFunc.code.push(line);
    braceCount += (line.match(/{/g) || []).length;
    braceCount -= (line.match(/}/g) || []).length;
    
    if (braceCount === 0 && currentFunc.code.length > 1) {
      currentFunc.endLine = i;
      viewFunctions.push(currentFunc);
      currentFunc = null;
    }
  }
}

console.log(`找到 ${viewFunctions.length} 个视图函数`);
viewFunctions.forEach(v => {
  console.log(`  ${v.name}: ${v.startLine + 1}-${v.endLine + 1} (${v.endLine - v.startLine + 1} 行)`);
});

// 导出视图函数到独立文件
viewFunctions.forEach(func => {
  const fileName = `${func.name}.jsx`;
  const filePath = path.join(viewsDir, fileName);
  
  // 添加必要的导入
  const imports = `import { useMemo } from "react";
import { Badge, Button, Card, Col, Empty, Form, Input, List, Row, Select, Space, Table, Tag, Typography } from "antd";
import { statusColor, toTitleMap, buildCommitPayload } from "../utils/helpers";

const { Text } = Typography;
const { TextArea } = Input;
`;
  
  const fullCode = imports + '\n' + func.code.join('\n');
  fs.writeFileSync(filePath, fullCode, 'utf8');
  console.log(`✓ 已导出 ${fileName}`);
});

console.log('\n拆分完成！');
console.log(`视图文件保存在: ${viewsDir}`);
