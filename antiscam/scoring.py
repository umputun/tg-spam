from typing import Tuple
from .config import settings


def score_keywords(text_low: str) -> Tuple[int, str | None]:
    score = 0

    for word, weight in settings.keyword_weights.items():
        occurrences = text_low.count(word)
        score += min(occurrences, 2) * weight

    score = min(score, 30)

    return score, f"Keyword score: {score}" if score > 0 else None


def score_safe_context(text_low: str) -> Tuple[int, str | None]:
    hits = sum(1 for w in settings.safe_signals if w in text_low)

    if hits >= 2:
        return -10, "Low-risk context detected"

    return 0, None


def score_intent(text_low: str) -> Tuple[int, str | None]:
    hits = sum(1 for w in settings.intent_signals if w in text_low)

    if hits > 0:
        return 10, "Scam intent detected"

    return 0, None


def score_blik(numbers: list[str], text_low: str) -> Tuple[int, str | None]:
    if not numbers:
        return 0, None

    context = sum(
        1 for k in ["blik", "kod", "przelew"]
        if k in text_low
    )

    if context >= 1:
        return 60, f"BLIK CONFIRMED: {numbers}"

    return 30, f"BLIK UNCERTAIN: {numbers}"