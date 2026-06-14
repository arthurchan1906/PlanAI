"""纯 NumPy 字符级 RNN 语言模型 MVP（下一 token 预测 + 采样生成）。"""

from __future__ import annotations

import numpy as np

from corpus import DEFAULT_CORPUS, build_vocab, decode, encode, training_pairs


def softmax(logits: np.ndarray) -> np.ndarray:
    z = logits - np.max(logits, axis=-1, keepdims=True)
    e = np.exp(z)
    return e / np.sum(e, axis=-1, keepdims=True)


def cross_entropy(logits: np.ndarray, target_id: int) -> float:
    probs = softmax(logits)
    return float(-np.log(probs[0, target_id] + 1e-9))


class CharRNN:
    """单层 tanh RNN：embed → hidden → vocab logits。"""

    def __init__(self, vocab_size: int, hidden_size: int = 64, seed: int = 42):
        self.vocab_size = vocab_size
        self.hidden_size = hidden_size
        rng = np.random.default_rng(seed)
        scale = 0.08
        self.W_embed = rng.normal(0, scale, size=(vocab_size, hidden_size))
        self.W_hh = rng.normal(0, scale, size=(hidden_size, hidden_size))
        self.b_h = np.zeros((1, hidden_size))
        self.W_hy = rng.normal(0, scale, size=(hidden_size, vocab_size))
        self.b_y = np.zeros((1, vocab_size))

    def forward(self, x_ids: np.ndarray) -> tuple[list[np.ndarray], list[np.ndarray], list[np.ndarray]]:
        """对长度为 T 的序列做前向，返回每步 logits / hidden / pre-activation。"""
        h = np.zeros((1, self.hidden_size))
        logits_list: list[np.ndarray] = []
        hs: list[np.ndarray] = []
        zs: list[np.ndarray] = []
        for tid in x_ids:
            x_emb = self.W_embed[tid : tid + 1]
            z = x_emb + h @ self.W_hh + self.b_h
            h = np.tanh(z)
            logits = h @ self.W_hy + self.b_y
            zs.append(z)
            hs.append(h.copy())
            logits_list.append(logits)
        return logits_list, hs, zs

    def loss_and_backward(
        self, x_ids: np.ndarray, y_ids: np.ndarray, lr: float = 0.1
    ) -> float:
        """单条序列的前向 + BPTT 反向，返回平均交叉熵损失。"""
        T = len(x_ids)
        logits_list, hs, zs = self.forward(x_ids)

        dW_embed = np.zeros_like(self.W_embed)
        dW_hh = np.zeros_like(self.W_hh)
        db_h = np.zeros_like(self.b_h)
        dW_hy = np.zeros_like(self.W_hy)
        db_y = np.zeros_like(self.b_y)

        dh_next = np.zeros((1, self.hidden_size))
        total_loss = 0.0

        for t in reversed(range(T)):
            logits = logits_list[t]
            target = int(y_ids[t])
            total_loss += cross_entropy(logits, target)

            probs = softmax(logits)
            dlogits = probs.copy()
            dlogits[0, target] -= 1.0

            dW_hy += hs[t].T @ dlogits
            db_y += dlogits
            dh = dlogits @ self.W_hy.T + dh_next
            dz = dh * (1.0 - hs[t] ** 2)

            dW_hh += (hs[t - 1].T @ dz) if t > 0 else 0.0
            db_h += dz
            dW_embed[x_ids[t]] += dz[0]
            dh_next = dz @ self.W_hh.T

        self.W_hy -= lr * dW_hy / T
        self.b_y -= lr * db_y / T
        self.W_hh -= lr * dW_hh / T
        self.b_h -= lr * db_h / T
        self.W_embed -= lr * dW_embed / T

        return total_loss / T

    @classmethod
    def from_vocab(cls, vocab_size: int, hidden: int = 64, seed: int = 42) -> CharRNN:
        return cls(vocab_size, hidden, seed=seed)

    def generate(
        self,
        prompt: str,
        stoi: dict[str, int],
        itos: list[str],
        max_new: int = 80,
        temperature: float = 0.8,
        seed: int | None = None,
    ) -> str:
        """自回归采样生成。temperature 越小越保守。"""
        rng = np.random.default_rng(seed)
        # 未知字符映射为空格，避免 OOV 崩溃
        fallback = stoi.get(" ", 0)
        ids = [stoi.get(ch, fallback) for ch in prompt]
        h = np.zeros((1, self.hidden_size))

        # 先跑一遍 prompt，建立 hidden state
        for tid in ids:
            x_emb = self.W_embed[tid : tid + 1]
            z = x_emb + h @ self.W_hh + self.b_h
            h = np.tanh(z)

        out = list(prompt)
        last_id = ids[-1] if ids else fallback

        for _ in range(max_new):
            x_emb = self.W_embed[last_id : last_id + 1]
            z = x_emb + h @ self.W_hh + self.b_h
            h = np.tanh(z)
            logits = (h @ self.W_hy + self.b_y) / max(temperature, 1e-6)
            probs = softmax(logits)[0]
            last_id = int(rng.choice(self.vocab_size, p=probs))
            out.append(itos[last_id])

        return "".join(out)


def train_char_lm(
    text: str = DEFAULT_CORPUS,
    hidden: int = 64,
    epochs: int = 400,
    lr: float = 0.15,
    seed: int = 42,
) -> tuple[CharRNN, dict[str, int], list[str]]:
    """在整段语料上训练字符级 RNN，返回模型与词表。"""
    stoi, itos = build_vocab(text)
    x_ids, y_ids = training_pairs(text, stoi)
    x = np.array(x_ids, dtype=int)
    y = np.array(y_ids, dtype=int)

    model = CharRNN(len(itos), hidden_size=hidden, seed=seed)
    for _ in range(epochs):
        model.loss_and_backward(x, y, lr=lr)
    return model, stoi, itos


def demo_generate(
    prompt: str = "hello ",
    text: str = DEFAULT_CORPUS,
    epochs: int = 500,
    max_new: int = 60,
) -> str:
    """训练后生成一段文本，供 main / 测试调用。"""
    model, stoi, itos = train_char_lm(text=text, epochs=epochs)
    return model.generate(prompt, stoi, itos, max_new=max_new, temperature=0.7, seed=0)
