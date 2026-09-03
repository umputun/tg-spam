"""Text de-obfuscation helpers used before risk scoring."""

from __future__ import annotations

import re


HOMOGLYPH_MAP = str.maketrans(
    {
        # Cyrillic characters commonly used to imitate Latin letters.
        "А": "A",
        "В": "B",
        "Е": "E",
        "К": "K",
        "М": "M",
        "Н": "H",
        "О": "O",
        "Р": "P",
        "С": "C",
        "Т": "T",
        "У": "Y",
        "Х": "X",
        "а": "a",
        "е": "e",
        "к": "k",
        "м": "m",
        "н": "h",
        "о": "o",
        "р": "p",
        "с": "c",
        "т": "t",
        "у": "y",
        "х": "x",
        "і": "i",
        "ј": "j",
        # Greek lookalikes.
        "Α": "A",
        "Β": "B",
        "Ε": "E",
        "Ζ": "Z",
        "Η": "H",
        "Ι": "I",
        "Κ": "K",
        "Μ": "M",
        "Ν": "N",
        "Ο": "O",
        "Ρ": "P",
        "Τ": "T",
        "Υ": "Y",
        "Χ": "X",
        "α": "a",
        "β": "b",
        "ε": "e",
        "ι": "i",
        "κ": "k",
        "ο": "o",
        "ρ": "p",
        "τ": "t",
        "υ": "y",
        "χ": "x",
    }
)

SINGLE_LETTER_CHAIN_RE = re.compile(r"(?iu)(?<!\w)(?:[a-ząćęłńóśźż]\W+){2,}[a-ząćęłńóśźż](?!\w)")
IN_WORD_SPECIAL_RE = re.compile(r"(?u)(?<=[^\W_])(?:[^\w\s]|_)+(?=[^\W_])")
WHITESPACE_RE = re.compile(r"\s+")


def deobfuscate_text(text: str) -> str:
    """Normalize simple phishing obfuscation without changing message meaning."""

    normalized = text.translate(HOMOGLYPH_MAP)
    normalized = SINGLE_LETTER_CHAIN_RE.sub(_join_letter_chain, normalized)
    normalized = IN_WORD_SPECIAL_RE.sub("", normalized)
    normalized = WHITESPACE_RE.sub(" ", normalized)
    return normalized.strip()


def _join_letter_chain(match: re.Match[str]) -> str:
    return "".join(re.findall(r"(?iu)[a-ząćęłńóśźż]", match.group(0)))
