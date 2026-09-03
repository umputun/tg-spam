import re
from typing import Dict
from .logger import get_logger
from .config import settings
from .links import analyze_links_detailed
from .ml import classify_message
from .normalization import deobfuscate_text
from .scoring import score_keywords, score_safe_context, score_intent, score_blik

logger = get_logger()


def detect_blik(text: str) -> list[str]:
    return re.findall(settings.blik_pattern, text)


def calculate_risk(text: str) -> Dict:
    logger.info("scan_started")

    risk_score = 0.0
    reasons = []

    normalized_text = deobfuscate_text(text)
    text_low = normalized_text.lower()

    # ML BASELINE
    ml_assessment = classify_message(text_low)
    risk_score = float(ml_assessment.score)
    reasons.append(
        f"ML intent score: {ml_assessment.score} "
        f"({ml_assessment.label}, p={ml_assessment.scam_probability:.2f})"
    )

    # BLIK
    numbers = detect_blik(normalized_text)
    s, r = score_blik(numbers, text_low)
    if r:
        risk_score *= 1.6 if s >= 60 else 1.25
        risk_score = max(risk_score, s)
        reasons.append(r)

    # LINKS
    link_analysis = analyze_links_detailed(text)
    safe_links = link_analysis.safe_links
    risky_links = link_analysis.risky_links

    if risky_links:
        risk_score *= 1.3
        risk_score += min(20, len(risky_links) * 5)
        reasons.append(f"Risky links: {risky_links}")

    if link_analysis.typosquatting_links:
        risk_score *= 2
        risk_score = max(risk_score, 90)
        reasons.append(f"Typosquatting links: {link_analysis.typosquatting_links}")

    if safe_links:
        risk_score *= 0.85
        reasons.append(f"Trusted links: {safe_links}")

    # KEYWORDS
    kw_score, kw_reason = score_keywords(text_low)
    risk_score += kw_score * 0.5
    if kw_reason:
        reasons.append(kw_reason)

    # SAFE CONTEXT
    safe_score, safe_reason = score_safe_context(text_low)
    risk_score += safe_score
    if safe_reason:
        reasons.append(safe_reason)

    # INTENT
    intent_score, intent_reason = score_intent(text_low)
    risk_score += intent_score * 0.5
    if intent_reason:
        reasons.append(intent_reason)

    # NORMALIZATION
    risk_score = max(0, min(int(risk_score), 100))

    # CLASSIFICATION
    if risk_score >= 80:
        status = "HIGH RISK"
    elif risk_score >= 50:
        status = "MEDIUM RISK"
    else:
        status = "LOW RISK"

    logger.info("scan_finished")

    return {
        "status": status,
        "risk_score": risk_score,
        "reasons": reasons,
        "safe_links": safe_links,
        "risky_links": risky_links
    }