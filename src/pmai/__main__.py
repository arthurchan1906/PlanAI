from __future__ import annotations

import sys

from .cli_main import main as cli_main
from .run_server import main as server_main


def main() -> None:
    if len(sys.argv) > 1 and sys.argv[1] == "web":
        sys.argv.pop(1)
        server_main()
        return
    cli_main()


if __name__ == "__main__":
    main()
