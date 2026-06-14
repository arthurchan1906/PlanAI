"""单元测试。"""

import numpy as np

from nn import SimpleNN, train_xor, xor_dataset


def test_xor_learns():
    """训练后应完全拟合 XOR 真值表。"""
    model = train_xor(epochs=8000, hidden=8, lr=0.6)
    x, y = xor_dataset()
    preds = model.predict(x)
    assert preds.shape == y.shape
    assert np.array_equal(preds, y), f"expected {y.T}, got {preds.T}"


def test_forward_shape():
    """前向传播输出形状应为 (batch, output_size)。"""
    model = SimpleNN(3, 5, 2)
    x = np.random.randn(4, 3)
    _, out = model.forward(x)
    assert out.shape == (4, 2)


def test_loss_decreases():
    """连续训练后 MSE 应下降，说明梯度方向正确。"""
    x, y = xor_dataset()
    model = SimpleNN(2, 6, 1, seed=0)
    losses = [model.backward(x, y, lr=0.5) for _ in range(200)]
    assert losses[-1] < losses[0]


if __name__ == "__main__":
    test_xor_learns()
    test_forward_shape()
    test_loss_decreases()
    print("All tests passed.")
