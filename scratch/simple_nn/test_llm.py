"""LLM MVP 单元测试。"""

from __future__ import annotations

import numpy as np

from corpus import build_vocab, decode, encode, training_pairs
from llm import CharRNN, demo_generate, train_char_lm


def test_vocab_roundtrip():
    text = "ab\nc"
    stoi, itos = build_vocab(text)
    ids = encode(text, stoi)
    assert decode(ids, itos) == text


def test_loss_decreases_on_tiny_corpus():
    """重复短串上 loss 应下降，验证 BPTT 方向正确。"""
    text = "abcabcabcabc"
    stoi, itos = build_vocab(text)
    x_ids, y_ids = training_pairs(text, stoi)
    x = np.array(x_ids, dtype=int)
    y = np.array(y_ids, dtype=int)

    model = CharRNN(len(itos), hidden_size=16, seed=0)
    losses = [model.loss_and_backward(x, y, lr=0.2) for _ in range(120)]
    assert losses[-1] < losses[0] * 0.85


def test_generate_runs():
    text = "hello\nhello\n"
    model, stoi, itos = train_char_lm(text=text, hidden=24, epochs=200, lr=0.2)
    out = model.generate("he", stoi, itos, max_new=10, temperature=1.0, seed=1)
    assert out.startswith("he")
    assert len(out) == 12


def test_demo_generate():
    out = demo_generate(prompt="h", text="hello world\n" * 3, epochs=300, max_new=20)
    assert len(out) >= 5


def test_corpus_vocab_has_chinese():
    """扩充语料后词表应包含汉字。"""
    from corpus import DEFAULT_CORPUS, build_vocab

    _, itos = build_vocab(DEFAULT_CORPUS)
    assert any("\u4e00" <= ch <= "\u9fff" for ch in itos)


if __name__ == "__main__":
    test_vocab_roundtrip()
    test_loss_decreases_on_tiny_corpus()
    test_generate_runs()
    test_demo_generate()
    test_corpus_vocab_has_chinese()
    print("All LLM tests passed.")
