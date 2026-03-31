# PlanAI

PlanAI 是一个给 AI 编码项目使用的本地项目管理工具。

特性：
- `planai init` 在当前项目创建 `.planai/` 隐藏目录
- JSON 数据库存放在 `.planai/data/planai.json`
- Web 配置存放在 `.planai/config.json`
- AI 使用说明可写入 `.planai/USAGE.md`
- CLI 可记录 canon、task、decision、commit、daily
- Web 直接用 Python 标准库 `http.server` 提供，无需 FastAPI
- 可通过 `pip install -e .` 安装

常用命令：

```bash
pip install -e .
planai init
planai help
planai info
planai canon show
planai-web
```

运行时目录：
- `.planai/data/planai.json`
- `.planai/config.json`
- `.planai/USAGE.md`

主要模块：
- `bootstrap.py`
- `cli_main.py`
- `usage_guide.py`
- `store.py`
- `web_server.py`
- `run_server.py`



