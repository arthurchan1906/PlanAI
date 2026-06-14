"""Transformer LLM MVP 单元测试。"""

from __future__ import annotations

import numpy as np

from corpus import build_vocab, training_pairs
from transformer import CharTransformer, causal_mask, train_transformer_lm


def test_causal_mask():
    m = causal_mask(4)
    assert m[0, 1] < -1e8
    assert m[1, 0] == 0


def test_forward_shape():
    stoi, itos = build_vocab("abc")
    model = CharTransformer(len(itos), d_model=16, n_layer=1, n_head=1, ff_dim=32, max_len=8, seed=0)
    idx = np.array([0, 1, 2], dtype=int)
    logits = model.forward(idx)
    assert logits.shape == (3, len(itos))


def test_loss_decreases():
    text = "abcabcabcabc"
    stoi, itos = build_vocab(text)
    x, y = training_pairs(text, stoi)
    x = np.array(x, dtype=int)
    y = np.array(y, dtype=int)
    model = CharTransformer(len(itos), d_model=24, n_layer=1, n_head=1, ff_dim=48, max_len=32, seed=0)
    losses = [model.loss_and_backward(x, y, lr=0.05) for _ in range(80)]
    assert losses[-1] < losses[0] * 0.9


def test_generate_runs():
    text = "hello\nhello\n"
    model, stoi, itos = train_transformer_lm(
        text=text, d_model=24, n_layer=1, n_head=1, ff_dim=48, epochs=120, lr=0.03
    )
    out = model.generate("he", stoi, itos, max_new=10, temperature=1.0, seed=1)
    assert out.startswith("he")
    assert len(out) == 12


def test_train_transformer_lm():
    model, stoi, itos = train_transformer_lm(
        text="hi\nhi\n", d_model=16, n_layer=1, n_head=1, ff_dim=32, epochs=100, lr=0.05
    )
    assert model.vocab_size == len(itos)


def test_chinese_corpus_vocab():
    from corpus import DEFAULT_CORPUS

    model, _, itos = train_transformer_lm(text=DEFAULT_CORPUS, d_model=16, n_layer=1, n_head=1, ff_dim=32, epochs=50)
    assert model.vocab_size == len(itos)
    assert any("\u4e00" <= ch <= "\u9fff" for ch in itos)


if __name__ == "__main__":
    test_causal_mask()
    test_forward_shape()
    test_loss_decreases()
    test_generate_runs()
    test_train_transformer_lm()
    test_chinese_corpus_vocab()
    print("All Transformer tests passed.")
