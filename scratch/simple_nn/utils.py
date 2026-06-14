"""scratch/simple_nn 小工具。"""

from __future__ import annotations

from corpus import DEFAULT_CORPUS


def corpus_stats(text: str = DEFAULT_CORPUS) -> dict[str, int]:
    """返回语料字符数、行数、非空行数。"""
    lines = text.splitlines()
    return {
        "chars": len(text),
        "lines": len(lines),
        "non_empty_lines": sum(1 for ln in lines if ln.strip()),
    }


def format_corpus_summary(text: str = DEFAULT_CORPUS) -> str:
    """人类可读统计摘要。"""
    s = corpus_stats(text)
    return f"{s['chars']} chars, {s['lines']} lines ({s['non_empty_lines']} non-empty)"


def count_non_empty_lines(text: str) -> int:
    """非空行数。"""
    return sum(1 for ln in text.splitlines() if ln.strip())


def one_line_summary(text: str = DEFAULT_CORPUS) -> str:
    """一行统计摘要。"""
    return format_corpus_summary(text)


def char_count(text: str) -> int:
    """字符总数。"""
    return len(text)


def line_count(text: str) -> int:
    """总行数（含空行）。"""
    return len(text.splitlines())
