"""XOR 演示入口。在 scratch/simple_nn 目录下运行: python main.py"""

from __future__ import annotations

from nn import train_xor, xor_dataset


def main() -> None:
    # 训练并在全部 4 个 XOR 样本上评估
    model = train_xor()
    x, y = xor_dataset()
    preds = model.predict(x)

    print("Simple NN — XOR（异或）")
    print("-" * 24)
    for i in range(len(x)):
        a, b = int(x[i, 0]), int(x[i, 1])
        print(f"  {a} XOR {b} = {int(preds[i, 0])}  (target {int(y[i, 0])})")

    ok = (preds == y).all()
    print("-" * 24)
    print("PASS" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
