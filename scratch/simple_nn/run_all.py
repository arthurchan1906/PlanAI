"""一键运行 scratch/simple_nn 全部演示与测试。"""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent

DEMOS = [
    ("XOR MLP", [sys.executable, "main.py"]),
    ("Char-RNN", [sys.executable, "llm_main.py", "--epochs", "200", "--max-new", "40"]),
    ("Transformer", [sys.executable, "transformer_main.py", "--epochs", "200", "--max-new", "40"]),
]

TESTS = [
    "test_nn.py",
    "test_llm.py",
    "test_transformer.py",
]


def run(cmd: list[str], label: str) -> bool:
    print(f"\n{'=' * 50}")
    print(f"  {label}")
    print("=" * 50)
    result = subprocess.run(cmd, cwd=ROOT)
    return result.returncode == 0


def main() -> None:
    print("simple_nn — all demos")
    ok = True
    quick = "--quick" in sys.argv
    verbose = "--verbose" in sys.argv
    dry_run = "--dry-run" in sys.argv
    demos = DEMOS[:1] if quick else DEMOS
    for label, cmd in demos:
        if verbose:
            print(f"[run] {label}: {' '.join(cmd)}")
        if dry_run:
            print(f"[dry-run] skip {label}")
            continue
        ok = run(cmd, label) and ok
    for test_file in TESTS:
        ok = run([sys.executable, test_file], f"test {test_file}") and ok
    print("\n" + ("ALL PASS" if ok else "SOME FAILED"))
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
