# 发布脚本使用指南

## 📦 完整的发布流程

`scripts/publish.py` 脚本会自动完成以下步骤：

### 自动执行的步骤

1. ✅ **构建前端** - 运行 `npm run build`
2. ✅ **复制前端** - 将 `ui/dist` 复制到 `src/pmai/ui/dist`
3. ✅ **清理旧文件** - 删除 `dist/` 和 `src/pmai.egg-info/`
4. ✅ **构建 Python 包** - 运行 `python -m build`
5. ✅ **检查包质量** - 运行 `twine check`
6. ✅ **上传到 PyPI** - 运行 `twine upload`

## 🚀 常用命令

### 完整发布（推荐）

```bash
# 构建前后端并上传到 PyPI
python scripts/publish.py
```

### 测试发布

```bash
# 上传到 TestPyPI（测试用）
python scripts/publish.py --repository testpypi
```

### 只构建不上传

```bash
# 构建但不上传，产物在 dist/ 目录
python scripts/publish.py --skip-upload
```

### 跳过前端构建

```bash
# 使用已有的 ui/dist，不重新构建
python scripts/publish.py --skip-ui-build
```

### 跳过 Python 构建

```bash
# 使用已有的 dist/，不重新构建 Python 包
python scripts/publish.py --skip-python-build
```

## 📋 参数说明

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--repository` | 上传目标 (pypi/testpypi) | pypi |
| `--skip-upload` | 跳过上传步骤 | False |
| `--skip-ui-build` | 跳过前端构建 | False |
| `--skip-python-build` | 跳过 Python 包构建 | False |

## ⚠️ 注意事项

### 前置条件

```bash
# 需要安装 Node.js 和 npm
npm --version

# 需要安装 Python 构建工具
python -m pip install build twine
```

### 前端文件路径

- **源文件**: `ui/` - 前端源代码
- **构建输出**: `ui/dist/` - 前端构建产物
- **Python 包内**: `src/pmai/ui/dist/` - Web 服务器从这里加载

### 发布前检查清单

- [ ] 前端代码已提交
- [ ] 后端代码已提交
- [ ] 版本号已更新 (`pyproject.toml`)
- [ ] CHANGELOG 已更新
- [ ] 本地测试通过

## 🔍 故障排查

### npm 找不到

```bash
# Windows
npm.cmd --version

# 如果找不到，检查 Node.js 安装
node --version
```

### 前端构建失败

```bash
# 手动构建前端
cd ui
npm install
npm run build
```

### Python 构建失败

```bash
# 手动构建 Python 包
python -m pip install --upgrade build twine
python -m build
```

### 上传失败

```bash
# 检查 twine 配置
twine check dist/*

# 手动上传
twine upload dist/*
```

## 📝 示例输出

```
==> Starting build and publish process

==> Building frontend (npm run build)
    Running: npm.cmd run build
    ✓ Frontend built successfully (2993 files)

==> Copying frontend dist to Python package (src/pmai/ui/dist)
    ✓ Cleaned old src/pmai/ui/dist
    ✓ Copied 2993 files to src/pmai/ui/dist

==> Cleaning old build artifacts
    ✓ Cleaned old artifacts

==> Building Python package
    Running: python -m build
    ✓ Python package built successfully (2 files)
      - aipm-cli-0.1.9.tar.gz (0.45 MB)
      - aipm_cli-0.1.9-py3-none-any.whl (0.52 MB)

==> Checking distributions with twine
    Running: python -m twine check dist/*
    ✓ Distributions passed checks

==> Uploading to pypi
    Running: python -m twine upload dist/*
    ✓ Uploaded to pypi

==> Publish completed successfully ✓
```
