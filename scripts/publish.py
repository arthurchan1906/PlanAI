from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path
import shutil


def step(message: str) -> None:
    print(f"==> {message}")


def run(command: list[str], cwd: Path) -> None:
    subprocess.run(command, cwd=cwd, check=True)


def main() -> int:
    parser = argparse.ArgumentParser(description="Build and publish aipm-cli")
    parser.add_argument(
        "--repository",
        choices=["pypi", "testpypi"],
        default="pypi",
        help="Upload target repository",
    )
    parser.add_argument(
        "--skip-upload",
        action="store_true",
        help="Build and check only; do not upload",
    )
    parser.add_argument(
        "--rebuild-ui",
        action="store_true",
        help="Run `npm run build` in ui/ before packaging",
    )
    args = parser.parse_args()

    repo_root = Path(__file__).resolve().parent.parent

    if args.rebuild_ui:
        step("Building UI")
        run(["npm.cmd", "run", "build"], cwd=repo_root / "ui")

    # step("Installing build tools")
    # run([sys.executable, "-m", "pip", "install", "--upgrade", "build", "twine"], cwd=repo_root)

    step("Cleaning old artifacts")
    shutil.rmtree(repo_root / "dist", ignore_errors=True)
    shutil.rmtree(repo_root / "src" / "pmai.egg-info", ignore_errors=True)

    step("Building package")
    run([sys.executable, "-m", "build"], cwd=repo_root)

    dist_files = sorted((repo_root / "dist").glob("*"))
    if not dist_files:
        raise SystemExit("No build artifacts found in dist/")

    step("Checking distributions")
    run([sys.executable, "-m", "twine", "check", *[str(path) for path in dist_files]], cwd=repo_root)

    if args.skip_upload:
        step("Build completed")
        print("Artifacts are in dist/")
        return 0

    step(f"Uploading to {args.repository}")
    upload_cmd = [sys.executable, "-m", "twine", "upload", "--verbose"]
    if args.repository == "testpypi":
        upload_cmd.extend(["--repository", "testpypi"])
    upload_cmd.extend(str(path) for path in dist_files)
    run(upload_cmd, cwd=repo_root)

    step("Publish completed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
