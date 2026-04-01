# PMAI

PMAI 是一个给 AI 编码项目使用的本地项目管理工具。

PyPI 分发名：`pmai`

特性：
- `pmai init` 在当前项目创建 `.pmai/` 隐藏目录
- JSON 数据库存放在 `.pmai/data/pmai.json`
- Web 配置存放在 `.pmai/config.json`
- AI 使用说明可写入 `.pmai/USAGE.md`
- CLI 可记录 canon、task、decision、commit、daily
- Web 直接用 Python 标准库 `http.server` 提供，无需 FastAPI
- 可通过 PyPI 安装并直接使用

常用命令：

```bash
pip install pmai
pmai init
pmai help
pmai info
pmai canon show
pmai-web
```

运行时目录：
- `.pmai/data/pmai.json`
- `.pmai/config.json`
- `.pmai/USAGE.md`

主要模块：
- `src/pmai/bootstrap.py`
- `src/pmai/cli_main.py`
- `src/pmai/usage_guide.py`
- `src/pmai/store.py`
- `src/pmai/web_server.py`
- `src/pmai/run_server.py`
