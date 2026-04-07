from __future__ import annotations

import os

from .store import DEFAULT_WEB_HOST, DEFAULT_WEB_PORT, load_runtime_config
from .web_server import create_server


def main() -> None:
    config = load_runtime_config()
    host = (
        os.getenv("PMAI_HOST")
        or os.getenv("PLANAI_HOST")
        or os.getenv("PROJECT_OS_HOST")
        or config.get("web_host", DEFAULT_WEB_HOST)
    )
    port = int(
        os.getenv("PMAI_PORT")
        or os.getenv("PLANAI_PORT")
        or os.getenv("PROJECT_OS_PORT")
        or config.get("web_port", DEFAULT_WEB_PORT)
    )
    server = create_server(host, port)
    print(f"AIPM web server listening on http://{host}:{port}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
