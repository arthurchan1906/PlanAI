"""纯 NumPy Decoder-only Transformer 语言模型 MVP（因果自注意力 + 下一字符预测）。"""

from __future__ import annotations

import math

import numpy as np

from corpus import DEFAULT_CORPUS, build_vocab, training_pairs


def softmax(x: np.ndarray, axis: int = -1) -> np.ndarray:
    z = x - np.max(x, axis=axis, keepdims=True)
    e = np.exp(z)
    return e / np.sum(e, axis=axis, keepdims=True)


def softmax_backward(dout: np.ndarray, probs: np.ndarray, axis: int = -1) -> np.ndarray:
    dot = np.sum(dout * probs, axis=axis, keepdims=True)
    return probs * (dout - dot)


def cross_entropy_loss(logits: np.ndarray, targets: np.ndarray) -> float:
    probs = softmax(logits, axis=1)
    t = targets.astype(int)
    return float(-np.mean(np.log(probs[np.arange(len(t)), t] + 1e-9)))


def causal_mask(T: int) -> np.ndarray:
    return np.triu(np.full((T, T), -1e9, dtype=np.float64), k=1)


def layer_norm_forward(x: np.ndarray, gamma: np.ndarray, beta: np.ndarray, eps: float = 1e-5):
    mean = x.mean(axis=-1, keepdims=True)
    var = x.var(axis=-1, keepdims=True)
    std = np.sqrt(var + eps)
    x_hat = (x - mean) / std
    out = gamma * x_hat + beta
    return out, (x, x_hat, mean, std, gamma, eps)


def layer_norm_backward(dout: np.ndarray, cache):
    x, x_hat, mean, std, gamma, _eps = cache
    N = x.shape[-1]
    dbeta = dout.sum(axis=0)
    dgamma = (dout * x_hat).sum(axis=0)
    dx_hat = dout * gamma
    dvar = (dx_hat * (x - mean)).sum(axis=-1, keepdims=True) * (-0.5) * (std ** -3)
    dmean = (dx_hat * (-1.0 / std)).sum(axis=-1, keepdims=True) + dvar * (
        -2.0 * (x - mean) / N
    )
    dx = dx_hat / std + dvar * 2.0 * (x - mean) / N + dmean / N
    return dx, dgamma, dbeta


class TransformerBlock:
    """Pre-LN: x + MHA(LN(x)); x + FFN(LN(x))."""

    def __init__(self, d_model: int, n_head: int, ff_dim: int, rng: np.random.Generator):
        if d_model % n_head != 0:
            raise ValueError("d_model must be divisible by n_head")
        self.n_head = n_head
        self.head_dim = d_model // n_head
        s = 0.02

        self.ln1_g = np.ones(d_model)
        self.ln1_b = np.zeros(d_model)
        self.ln2_g = np.ones(d_model)
        self.ln2_b = np.zeros(d_model)
        self.w_qkv = rng.normal(0, s, (d_model, 3 * d_model))
        self.b_qkv = np.zeros(3 * d_model)
        self.w_o = rng.normal(0, s, (d_model, d_model))
        self.b_o = np.zeros(d_model)
        self.w1 = rng.normal(0, s, (d_model, ff_dim))
        self.b1 = np.zeros(ff_dim)
        self.w2 = rng.normal(0, s, (ff_dim, d_model))
        self.b2 = np.zeros(d_model)
        self.grads: dict[str, np.ndarray] = {}
        self._cache: dict = {}

    def forward(self, x: np.ndarray, attn_mask: np.ndarray) -> np.ndarray:
        ln1, c_ln1 = layer_norm_forward(x, self.ln1_g, self.ln1_b)
        attn_out, c_attn = self._mha_forward(ln1, attn_mask)
        x = x + attn_out

        ln2, c_ln2 = layer_norm_forward(x, self.ln2_g, self.ln2_b)
        h_pre = ln2 @ self.w1 + self.b1
        h = np.maximum(h_pre, 0.0)
        ff = h @ self.w2 + self.b2
        x = x + ff

        self._cache = {"ln1": c_ln1, "ln1_x": ln1, "c_attn": c_attn, "ln2": c_ln2, "ln2_x": ln2, "h_pre": h_pre, "h": h}
        return x

    def _mha_forward(self, x: np.ndarray, attn_mask: np.ndarray):
        T, C = x.shape
        nh, hs = self.n_head, self.head_dim
        qkv = x @ self.w_qkv + self.b_qkv
        q, k, v = np.split(qkv, 3, axis=1)
        q = q.reshape(T, nh, hs).transpose(1, 0, 2)
        k = k.reshape(T, nh, hs).transpose(1, 0, 2)
        v = v.reshape(T, nh, hs).transpose(1, 0, 2)
        scores = (q @ k.transpose(0, 2, 1)) / math.sqrt(hs) + attn_mask[np.newaxis, :, :]
        weights = softmax(scores, axis=-1)
        ctx = (weights @ v).transpose(1, 0, 2).reshape(T, C)
        out = ctx @ self.w_o + self.b_o
        return out, (x, q, k, v, weights, ctx)

    def _mha_backward(self, dout: np.ndarray, cache):
        x, q, k, v, weights, ctx = cache
        T, C = x.shape
        nh, hs = self.n_head, self.head_dim

        self.grads["w_o"] = ctx.T @ dout
        self.grads["b_o"] = dout.sum(axis=0)
        dctx = dout @ self.w_o.T
        dctx = dctx.reshape(nh, T, hs)

        dv = weights.transpose(0, 2, 1) @ dctx
        dweights = dctx @ v.transpose(0, 2, 1)
        dscores = softmax_backward(dweights, weights, axis=-1) / math.sqrt(hs)
        dq = dscores @ k
        dk = dscores.transpose(0, 2, 1) @ q

        dq = dq.transpose(1, 0, 2).reshape(T, C)
        dk = dk.transpose(1, 0, 2).reshape(T, C)
        dv = dv.transpose(1, 0, 2).reshape(T, C)
        dqkv = np.concatenate([dq, dk, dv], axis=1)
        self.grads["w_qkv"] = x.T @ dqkv
        self.grads["b_qkv"] = dqkv.sum(axis=0)
        return dqkv @ self.w_qkv.T

    def backward(self, dout: np.ndarray) -> np.ndarray:
        c = self._cache
        dx = dout.copy()

        d_h = dout @ self.w2.T
        self.grads["w2"] = c["h"].T @ dout
        self.grads["b2"] = dout.sum(axis=0)
        d_ln2 = (d_h * (c["h_pre"] > 0)) @ self.w1.T
        self.grads["w1"] = c["ln2_x"].T @ (d_h * (c["h_pre"] > 0))
        self.grads["b1"] = (d_h * (c["h_pre"] > 0)).sum(axis=0)
        d_ln2_out, self.grads["ln2_g"], self.grads["ln2_b"] = layer_norm_backward(d_ln2, c["ln2"])
        dx += d_ln2_out

        d_ln1_out = self._mha_backward(dx, c["c_attn"])
        d_ln1_in, self.grads["ln1_g"], self.grads["ln1_b"] = layer_norm_backward(d_ln1_out, c["ln1"])
        dx += d_ln1_in
        return dx

    def step(self, lr: float) -> None:
        for attr, gkey in [
            ("w_qkv", "w_qkv"), ("b_qkv", "b_qkv"), ("w_o", "w_o"), ("b_o", "b_o"),
            ("w1", "w1"), ("b1", "b1"), ("w2", "w2"), ("b2", "b2"),
            ("ln1_g", "ln1_g"), ("ln1_b", "ln1_b"), ("ln2_g", "ln2_g"), ("ln2_b", "ln2_b"),
        ]:
            g = self.grads.get(gkey)
            if g is not None:
                setattr(self, attr, getattr(self, attr) - lr * g)


class CharTransformer:
    """GPT 风格字符级 Decoder-only Transformer。"""

    def __init__(
        self,
        vocab_size: int,
        d_model: int = 64,
        n_layer: int = 2,
        n_head: int = 2,
        ff_dim: int = 128,
        max_len: int = 256,
        seed: int = 42,
    ):
        self.vocab_size = vocab_size
        self.d_model = d_model
        self.max_len = max_len
        rng = np.random.default_rng(seed)
        s = 0.02
        self.wte = rng.normal(0, s, (vocab_size, d_model))
        self.wpe = rng.normal(0, s, (max_len, d_model))
        self.lm_head = rng.normal(0, s, (d_model, vocab_size))
        self.blocks = [TransformerBlock(d_model, n_head, ff_dim, rng) for _ in range(n_layer)]
        self.ln_f_g = np.ones(d_model)
        self.ln_f_b = np.zeros(d_model)
        self.grads: dict[str, np.ndarray] = {}
        self._cache: dict = {}

    def forward(self, idx: np.ndarray) -> np.ndarray:
        T = len(idx)
        if T > self.max_len:
            raise ValueError(f"sequence length {T} exceeds max_len {self.max_len}")
        x = self.wte[idx] + self.wpe[:T]
        mask = causal_mask(T)
        for block in self.blocks:
            x = block.forward(x, mask)
        x, c_ln = layer_norm_forward(x, self.ln_f_g, self.ln_f_b)
        logits = x @ self.lm_head
        self._cache = {"idx": idx, "T": T, "x0": self.wte[idx] + self.wpe[:T], "ln_f": c_ln, "x_final": x}
        return logits

    def backward(self, targets: np.ndarray) -> None:
        logits = self._cache.get("logits")
        if logits is None:
            raise RuntimeError("call forward before backward")
        T = self._cache["T"]
        probs = softmax(logits, axis=1)
        dlogits = probs.copy()
        t = targets.astype(int)
        dlogits[np.arange(T), t] -= 1.0
        dlogits /= T

        self.grads["lm_head"] = self._cache["x_final"].T @ dlogits
        dx, self.grads["ln_f_g"], self.grads["ln_f_b"] = layer_norm_backward(
            dlogits @ self.lm_head.T, self._cache["ln_f"]
        )

        for block in reversed(self.blocks):
            dx = block.backward(dx)

        d_wte = np.zeros_like(self.wte)
        idx = self._cache["idx"]
        for t, tok in enumerate(idx):
            d_wte[tok] += dx[t]
        self.grads["wte"] = d_wte
        self.grads["wpe"] = np.zeros_like(self.wpe)
        self.grads["wpe"][:T] = dx

    def loss_and_backward(self, x_ids: np.ndarray, y_ids: np.ndarray, lr: float = 0.01) -> float:
        logits = self.forward(x_ids)
        self._cache["logits"] = logits
        loss = cross_entropy_loss(logits, y_ids)
        self.backward(y_ids)
        self.step(lr)
        return loss

    def step(self, lr: float) -> None:
        if "wte" in self.grads:
            self.wte -= lr * self.grads["wte"]
        if "wpe" in self.grads:
            self.wpe -= lr * self.grads["wpe"]
        if "lm_head" in self.grads:
            self.lm_head -= lr * self.grads["lm_head"]
        if "ln_f_g" in self.grads:
            self.ln_f_g -= lr * self.grads["ln_f_g"]
        if "ln_f_b" in self.grads:
            self.ln_f_b -= lr * self.grads["ln_f_b"]
        for block in self.blocks:
            block.step(lr)

    def generate(
        self,
        prompt: str,
        stoi: dict[str, int],
        itos: list[str],
        max_new: int = 80,
        temperature: float = 0.8,
        seed: int | None = None,
    ) -> str:
        rng = np.random.default_rng(seed)
        fallback = stoi.get(" ", 0)
        ids = [stoi.get(ch, fallback) for ch in prompt]
        out = list(prompt)

        for _ in range(max_new):
            idx = np.array(ids, dtype=int)
            logits = self.forward(idx)
            next_logits = logits[-1] / max(temperature, 1e-6)
            probs = softmax(next_logits.reshape(1, -1), axis=1)[0]
            nid = int(rng.choice(self.vocab_size, p=probs))
            ids.append(nid)
            out.append(itos[nid])

        return "".join(out)


def train_transformer_lm(
    text: str = DEFAULT_CORPUS,
    d_model: int = 64,
    n_layer: int = 2,
    n_head: int = 2,
    ff_dim: int = 128,
    epochs: int = 300,
    lr: float = 0.01,
    seed: int = 42,
) -> tuple[CharTransformer, dict[str, int], list[str]]:
    stoi, itos = build_vocab(text)
    x_ids, y_ids = training_pairs(text, stoi)
    x = np.array(x_ids, dtype=int)
    y = np.array(y_ids, dtype=int)
    model = CharTransformer(
        len(itos), d_model=d_model, n_layer=n_layer, n_head=n_head, ff_dim=ff_dim,
        max_len=max(len(x) + 1, 64), seed=seed,
    )
    for _ in range(epochs):
        model.loss_and_backward(x, y, lr=lr)
    return model, stoi, itos
