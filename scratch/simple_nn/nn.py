"""纯 NumPy 实现的两层 MLP（XOR 教学 demo，与 PlanAI 主项目无关）。"""

from __future__ import annotations

import numpy as np


def sigmoid(x: np.ndarray) -> np.ndarray:
    """Sigmoid 激活：把实数映射到 (0, 1)，用作隐藏层和输出层。"""
    return 1.0 / (1.0 + np.exp(-x))


def sigmoid_deriv(y: np.ndarray) -> np.ndarray:
    """Sigmoid 导数。传入已激活的值 y=σ(z)，避免重复计算 exp。"""
    return y * (1.0 - y)


class SimpleNN:
    """两层全连接网络：input → hidden(sigmoid) → output(sigmoid)。

    权重形状约定（批量输入 x 形状为 (m, input_size)）：
      w1: (input_size, hidden_size)
      w2: (hidden_size, output_size)
    """

    def __init__(self, input_size: int, hidden_size: int, output_size: int, seed: int = 42):
        rng = np.random.default_rng(seed)
        # 小随机权重打破对称性；偏置初始化为 0
        self.w1 = rng.normal(0, 0.5, size=(input_size, hidden_size))
        self.b1 = np.zeros((1, hidden_size))
        self.w2 = rng.normal(0, 0.5, size=(hidden_size, output_size))
        self.b2 = np.zeros((1, output_size))

    def forward(self, x: np.ndarray) -> tuple[np.ndarray, np.ndarray]:
        """前向传播，并缓存中间结果供反向传播使用。"""
        self.z1 = x @ self.w1 + self.b1   # 隐藏层线性变换
        self.a1 = sigmoid(self.z1)        # 隐藏层激活
        self.z2 = self.a1 @ self.w2 + self.b2
        self.a2 = sigmoid(self.z2)         # 输出层激活（XOR 任务中为 0~1 概率）
        return self.a1, self.a2

    def backward(self, x: np.ndarray, y: np.ndarray, lr: float = 0.5) -> float:
        """反向传播 + 梯度下降一步，返回当前 batch 的 MSE 损失。"""
        m = x.shape[0]  # batch 大小（XOR 固定为 4）
        _, a2 = self.forward(x)
        loss = float(np.mean((a2 - y) ** 2))

        # 输出层梯度（MSE + sigmoid 链式法则）
        dz2 = (a2 - y) * sigmoid_deriv(a2)
        dw2 = (self.a1.T @ dz2) / m
        db2 = np.sum(dz2, axis=0, keepdims=True) / m

        # 隐藏层梯度
        da1 = dz2 @ self.w2.T
        dz1 = da1 * sigmoid_deriv(self.a1)
        dw1 = (x.T @ dz1) / m
        db1 = np.sum(dz1, axis=0, keepdims=True) / m

        # 更新参数
        self.w2 -= lr * dw2
        self.b2 -= lr * db2
        self.w1 -= lr * dw1
        self.b1 -= lr * db1
        return loss

    def predict(self, x: np.ndarray) -> np.ndarray:
        """输出 ≥0.5 判为 1，否则为 0。"""
        return (self.forward(x)[1] >= 0.5).astype(int)


def xor_dataset() -> tuple[np.ndarray, np.ndarray]:
    """XOR 真值表。经典非线性问题，单层感知机无法解决，需要隐藏层。"""
    x = np.array([[0, 0], [0, 1], [1, 0], [1, 1]], dtype=float)
    y = np.array([[0], [1], [1], [0]], dtype=float)
    return x, y


def train_xor(epochs: int = 5000, hidden: int = 4, lr: float = 0.5) -> SimpleNN:
    """在 XOR 数据集上训练，默认 5000 轮通常足够收敛。"""
    x, y = xor_dataset()
    model = SimpleNN(2, hidden, 1)
    for _ in range(epochs):
        model.backward(x, y, lr=lr)
    return model
