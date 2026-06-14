# Hook 演示说明

本目录可用于 **PlanAI Cursor Hook** 联调：Agent 在此做多文件读写、Shell、Grep 等操作时，Discussions 应出现对应记录。

## 预期 hook 事件

| 操作类型 | 示例 | Discussions 展示 |
|----------|------|------------------|
| 用户消息 | 在 Agent 里发中文 prompt | `user` / cursor |
| 读文件 | Read `corpus.py` | 👁 文件路径 |
| 编辑 | StrReplace / Write | 📝 或 🆕 文件路径 + diff |
| Shell | `python test_nn.py` | 🔧 命令 |
| Grep | 搜索 `train_char_lm` | 🔍 pattern @ path |

## 快速自检

```bash
cd scratch/simple_nn
python run_all.py
```

## 语料

`corpus.py` 的 `DEFAULT_CORPUS` 含中文短句，便于观察 hook 对 **中英混合文本** 的处理。

## 检查项

- 每个文件编辑在 DB 中应只有 **一条** `afterFileEdit` 记录（无 postToolUse Write 重复）
- content 应含 `- old` / `+ new` diff 预览
- 新建文件应显示 🆕 而非 📝
