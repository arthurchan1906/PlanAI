"""极小语料与字符词表，供 LLM MVP 训练/测试。"""

from __future__ import annotations

# 重复模式 + 常见英文片段，便于小模型快速过拟合并肉眼检查生成效果
DEFAULT_CORPUS = """
hello world
hello llm
the cat sat on the mat
the dog ran fast
ai learns patterns
llm mvp demo
hello world again
神经网络入门
字符级语言模型
纯 numpy 手写反向传播
""".strip()


def build_vocab(text: str) -> tuple[dict[str, int], list[str]]:
    """从语料构建字符级词表。保留换行，空格也作为 token。"""
    chars = sorted(set(text))
    stoi = {ch: i for i, ch in enumerate(chars)}
    itos = chars
    return stoi, itos


def encode(text: str, stoi: dict[str, int]) -> list[int]:
    return [stoi[ch] for ch in text]


def decode(ids: list[int], itos: list[str]) -> str:
    return "".join(itos[i] for i in ids)


def training_pairs(text: str, stoi: dict[str, int]) -> tuple[list[int], list[int]]:
    """下一字符预测：输入 seq[:-1]，目标 seq[1:]。

    支持中英文混合语料；词表按字符粒度构建。
    """
    ids = encode(text, stoi)
    return ids[:-1], ids[1:]
