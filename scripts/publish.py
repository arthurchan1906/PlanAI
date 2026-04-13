from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path
import shutil


def step(message: str) -> None:
    print(f"\n==> {message}")


def run(command: list[str], cwd: Path) -> None:
    print(f"    Running: {' '.join(command)}")
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
        "--skip-ui-build",
        action="store_true",
        help="Skip UI build step (use existing ui/dist)",
    )
    parser.add_argument(
        "--skip-python-build",
        action="store_true",
        help="Skip Python package build (use existing dist)",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Show what would be done without executing",
    )
    args = parser.parse_args()

    repo_root = Path(__file__).resolve().parent.parent
    ui_dir = repo_root / "ui"
    ui_dist = ui_dir / "dist"
    pmai_ui_dir = repo_root / "src" / "pmai" / "ui"
    pmai_ui_dist = pmai_ui_dir / "dist"

    step("Starting build and publish process")

    # Step 1: Build frontend
    if not args.skip_ui_build:
        step("Building frontend (npm run build)")
        try:
            # 尝试不同的 npm 命令
            if sys.platform == "win32":
                run(["npm.cmd", "run", "build"], cwd=ui_dir)
            else:
                run(["npm", "run", "build"], cwd=ui_dir)
            
            # 验证前端构建成功
            if not ui_dist.exists():
                raise SystemExit(f"UI build failed: {ui_dist} not found")
            
            ui_files = list(ui_dist.rglob("*"))
            if not ui_files:
                raise SystemExit(f"UI build failed: {ui_dist} is empty")
            
            print(f"    ✓ Frontend built successfully ({len(ui_files)} files)")
        except FileNotFoundError:
            raise SystemExit("npm not found. Please install Node.js and npm first.")
        except subprocess.CalledProcessError as e:
            raise SystemExit(f"UI build failed: {e}")
    else:
        step("Skipping frontend build (using existing ui/dist)")
        if not ui_dist.exists():
            raise SystemExit(f"ui/dist not found. Build frontend first or remove --skip-ui-build flag.")

    # Step 2: Copy frontend to Python package
    step("Copying frontend dist to Python package (src/pmai/ui/dist)")
    if pmai_ui_dist.exists():
        shutil.rmtree(pmai_ui_dist)
        print(f"    ✓ Cleaned old {pmai_ui_dist}")
    
    shutil.copytree(ui_dist, pmai_ui_dist)
    pmai_ui_files = list(pmai_ui_dist.rglob("*"))
    print(f"    ✓ Copied {len(pmai_ui_files)} files to {pmai_ui_dist.relative_to(repo_root)}")

    # Step 3: Build Python package
    if not args.skip_python_build:
        step("Cleaning old build artifacts")
        shutil.rmtree(repo_root / "dist", ignore_errors=True)
        shutil.rmtree(repo_root / "src" / "pmai.egg-info", ignore_errors=True)
        print(f"    ✓ Cleaned old artifacts")

        step("Building Python package")
        try:
            run([sys.executable, "-m", "build"], cwd=repo_root)
            
            dist_files = sorted((repo_root / "dist").glob("*"))
            if not dist_files:
                raise SystemExit("No build artifacts found in dist/")
            
            print(f"    ✓ Python package built successfully ({len(dist_files)} files)")
            for f in dist_files:
                size_mb = f.stat().st_size / 1024 / 1024
                print(f"      - {f.name} ({size_mb:.2f} MB)")
        except subprocess.CalledProcessError as e:
            raise SystemExit(f"Python build failed: {e}")
    else:
        step("Skipping Python package build (using existing dist)")
        dist_files = sorted((repo_root / "dist").glob("*"))
        if not dist_files:
            raise SystemExit("No build artifacts found in dist/. Build first or remove --skip-python-build flag.")

    # Step 4: Check distributions
    step("Checking distributions with twine")
    try:
        dist_files = sorted((repo_root / "dist").glob("*"))
        run([sys.executable, "-m", "twine", "check", *[str(path) for path in dist_files]], cwd=repo_root)
        print(f"    ✓ Distributions passed checks")
    except subprocess.CalledProcessError as e:
        raise SystemExit(f"Twine check failed: {e}")

    # Step 5: Upload
    if args.skip_upload:
        step("Build completed (skipping upload)")
        print(f"\n✓ Artifacts are in {repo_root / 'dist'}")
        return 0

    step(f"Uploading to {args.repository}")
    try:
        dist_files = sorted((repo_root / "dist").glob("*"))
        upload_cmd = [sys.executable, "-m", "twine", "upload"]
        if args.repository == "testpypi":
            upload_cmd.extend(["--repository", "testpypi"])
        upload_cmd.extend(str(path) for path in dist_files)
        run(upload_cmd, cwd=repo_root)
        print(f"    ✓ Uploaded to {args.repository}")
    except subprocess.CalledProcessError as e:
        raise SystemExit(f"Upload failed: {e}")

    step("Publish completed successfully ✓")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
