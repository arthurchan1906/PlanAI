"""LLM MVP 演示：训练字符级 RNN 并采样生成。"""

from __future__ import annotations

import argparse

from corpus import DEFAULT_CORPUS
from llm import train_char_lm


def main() -> None:
    parser = argparse.ArgumentParser(description="Char-RNN LLM MVP (NumPy)")
    parser.add_argument("--prompt", default="hello ", help="生成起始文本")
    parser.add_argument("--epochs", type=int, default=600, help="训练轮数（小语料可 200~800）")
    parser.add_argument("--hidden", type=int, default=64, help="隐层维度")
    parser.add_argument("--max-new", type=int, default=80, help="最多生成字符数")
    parser.add_argument("--temperature", type=float, default=0.75, help="采样温度")
    parser.add_argument("--lr", type=float, default=0.1, help="学习率")
    parser.add_argument("--seed", type=int, default=42, help="随机种子")
    args = parser.parse_args()

    print("Char-RNN LLM MVP")
    print("-" * 40)
    print(f"corpus chars: {len(DEFAULT_CORPUS)}")
    print(f"training epochs={args.epochs}, hidden={args.hidden}, lr={args.lr}")

    model, stoi, itos = train_char_lm(
        text=DEFAULT_CORPUS,
        hidden=args.hidden,
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
        seed=42,
    )

    print("-" * 40)
    print(f"prompt:   {args.prompt!r}")
    print(f"sample:\n{sample}")


if __name__ == "__main__":
    main()
