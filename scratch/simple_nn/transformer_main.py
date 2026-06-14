"""Transformer LLM MVP 演示：训练 Decoder-only Transformer 并采样生成。"""

from __future__ import annotations

import argparse

from corpus import DEFAULT_CORPUS
from transformer import train_transformer_lm


def main() -> None:
    parser = argparse.ArgumentParser(description="Char Transformer LLM MVP (NumPy)")
    parser.add_argument("--prompt", default="hello ", help="生成起始文本")
    parser.add_argument("--epochs", type=int, default=400, help="训练轮数（默认 400）")
    parser.add_argument("--d-model", type=int, default=64, help="模型维度")
    parser.add_argument("--n-layer", type=int, default=2, help="Transformer 层数")
    parser.add_argument("--n-head", type=int, default=2, help="注意力头数")
    parser.add_argument("--ff-dim", type=int, default=128, help="FFN 隐层维度")
    parser.add_argument("--max-new", type=int, default=80, help="最多生成字符数")
    parser.add_argument("--temperature", type=float, default=0.75, help="采样温度")
    parser.add_argument("--lr", type=float, default=0.01, help="学习率")
    parser.add_argument("--seed", type=int, default=42, help="随机种子")
    args = parser.parse_args()

    print("Char Transformer LLM MVP")
    print("-" * 40)
    print(f"corpus chars: {len(DEFAULT_CORPUS)}")
    print(
        f"train: epochs={args.epochs}, d_model={args.d_model}, "
        f"layers={args.n_layer}, heads={args.n_head}, lr={args.lr}"
    )

    model, stoi, itos = train_transformer_lm(
        text=DEFAULT_CORPUS,
        d_model=args.d_model,
        n_layer=args.n_layer,
        n_head=args.n_head,
        ff_dim=args.ff_dim,
        epochs=args.epochs,
        lr=args.lr,
        seed=args.seed,
    )

    sample = model.generate(
        args.prompt,
        stoi,
        itos,
        max_new=args.max_new,
        temperature=args.temperature,
        seed=args.seed,
    )

    print("-" * 40)
    print(f"prompt:   {args.prompt!r}")
    print(f"sample:\n{sample}")


if __name__ == "__main__":
    main()
