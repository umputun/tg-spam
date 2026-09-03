"""Educational AI/NLP helpers for the AntiScam project.

The module intentionally uses only the Python standard library so the examples
remain easy to inspect during labs. It is not meant to replace production ML
libraries; it provides small, deterministic building blocks for syllabus demos.
"""

from __future__ import annotations

import math
import re
import base64
import json
import sys
from collections import Counter, defaultdict
from dataclasses import dataclass
from typing import Iterable

from .engine import calculate_risk


TOKEN_PATTERN = re.compile(r"[a-zA-ZąćęłńóśźżĄĆĘŁŃÓŚŹŻ0-9]+")
NAMED_ENTITY_PATTERN = re.compile(
    r"\b(?:[A-ZĄĆĘŁŃÓŚŹŻ][a-ząćęłńóśźżA-ZĄĆĘŁŃÓŚŹŻ]+)(?:\s+[A-ZĄĆĘŁŃÓŚŹŻ][a-ząćęłńóśźżA-ZĄĆĘŁŃÓŚŹŻ]+)*\b"
)
ENTITY_STOPWORDS = {"boj", "boję", "boje", "proszę", "prosze", "dziękuję", "dziekuje"}


def tokenize(text: str) -> list[str]:
    """Lowercase and tokenize Polish/English text."""

    return [match.group(0).lower() for match in TOKEN_PATTERN.finditer(text)]


def bag_of_words(text: str) -> Counter[str]:
    return Counter(tokenize(text))


def cosine_similarity(left: Counter[str], right: Counter[str]) -> float:
    shared = set(left) & set(right)
    numerator = sum(left[token] * right[token] for token in shared)
    left_norm = math.sqrt(sum(value * value for value in left.values()))
    right_norm = math.sqrt(sum(value * value for value in right.values()))
    if left_norm == 0 or right_norm == 0:
        return 0.0
    return numerator / (left_norm * right_norm)


@dataclass(frozen=True)
class Classification:
    label: str
    scores: dict[str, float]


class NaiveBayesTextClassifier:
    """Tiny multinomial Naive Bayes classifier for lab-sized text datasets."""

    def __init__(self) -> None:
        self._label_counts: Counter[str] = Counter()
        self._token_counts: dict[str, Counter[str]] = defaultdict(Counter)
        self._total_tokens: Counter[str] = Counter()
        self._vocabulary: set[str] = set()

    def train(self, samples: Iterable[tuple[str, str]]) -> None:
        for text, label in samples:
            tokens = tokenize(text)
            self._label_counts[label] += 1
            self._token_counts[label].update(tokens)
            self._total_tokens[label] += len(tokens)
            self._vocabulary.update(tokens)

    def predict(self, text: str) -> Classification:
        if not self._label_counts:
            raise ValueError("Classifier has not been trained.")

        tokens = tokenize(text)
        total_documents = sum(self._label_counts.values())
        vocabulary_size = max(1, len(self._vocabulary))
        scores: dict[str, float] = {}

        for label, label_count in self._label_counts.items():
            score = math.log(label_count / total_documents)
            denominator = self._total_tokens[label] + vocabulary_size
            for token in tokens:
                score += math.log((self._token_counts[label][token] + 1) / denominator)
            scores[label] = score

        best_label = max(scores, key=scores.get)
        return Classification(best_label, scores)


class NGramLanguageModel:
    """Simple add-one smoothed n-gram language model."""

    def __init__(self, n: int = 2) -> None:
        if n < 1:
            raise ValueError("n must be at least 1.")
        self.n = n
        self._counts: Counter[tuple[str, ...]] = Counter()
        self._contexts: Counter[tuple[str, ...]] = Counter()
        self._vocabulary: set[str] = set()

    def train(self, texts: Iterable[str]) -> None:
        for text in texts:
            tokens = ["<s>"] * (self.n - 1) + tokenize(text) + ["</s>"]
            self._vocabulary.update(tokens)
            for index in range(len(tokens) - self.n + 1):
                ngram = tuple(tokens[index : index + self.n])
                context = ngram[:-1]
                self._counts[ngram] += 1
                self._contexts[context] += 1

    def probability(self, context: Iterable[str], token: str) -> float:
        context_tuple = tuple(context)[-(self.n - 1) :] if self.n > 1 else tuple()
        ngram = context_tuple + (token.lower(),)
        vocabulary_size = max(1, len(self._vocabulary))
        return (self._counts[ngram] + 1) / (self._contexts[context_tuple] + vocabulary_size)


def extract_terms(text: str, min_length: int = 4) -> list[str]:
    counts = Counter(token for token in tokenize(text) if len(token) >= min_length)
    return [term for term, _ in counts.most_common()]


def extract_named_entities(text: str) -> list[str]:
    entities = []
    for match in NAMED_ENTITY_PATTERN.finditer(text):
        entity = match.group(0)
        if entity.lower() not in ENTITY_STOPWORDS:
            entities.append(entity)
    return entities


class TranslationMemory:
    def __init__(self) -> None:
        self._entries: list[tuple[str, str, Counter[str]]] = []

    def add(self, source: str, target: str) -> None:
        self._entries.append((source, target, bag_of_words(source)))

    def suggest(self, source: str) -> tuple[str, float] | None:
        if not self._entries:
            return None
        vector = bag_of_words(source)
        source_text, target_text, score = max(
            (
                (entry_source, entry_target, cosine_similarity(vector, entry_vector))
                for entry_source, entry_target, entry_vector in self._entries
            ),
            key=lambda item: item[2],
        )
        _ = source_text
        return target_text, score


@dataclass(frozen=True)
class DialogResponse:
    intent: str
    message: str
    emotion: str


@dataclass(frozen=True)
class AiAssistanceReport:
    purpose: str
    intent: str
    emotion: str
    suggested_action: str
    scan_status: str
    risk_score: int
    blocked_after_scan: bool
    block_explanation: str
    scan_reasons: list[str]
    extracted_terms: list[str]
    named_entities: list[str]
    scam_similarity: float
    what_ai_makes_easier: list[str]


@dataclass(frozen=True)
class SendingBlockExplanation:
    source: str
    explanation: str
    recommended_action: str


class AntiScamDialogBot:
    def respond(self, message: str) -> DialogResponse:
        text = message.lower()
        emotion = detect_emotion(message)

        if "blik" in text or "kod" in text:
            return DialogResponse(
                "report_scam",
                "Nie podawaj kodu. Zweryfikuj prosbe innym kanalem i zachowaj zrzut ekranu.",
                emotion,
            )
        if "link" in text or "http" in text:
            return DialogResponse(
                "check_link",
                "Sprawdz domene, literowki i kontekst. Nie loguj sie przez podejrzany link.",
                emotion,
            )
        return DialogResponse(
            "education",
            "Opisz wiadomosc, a pomoge ocenic ryzyko i dobrac bezpieczna reakcje.",
            emotion,
        )


def detect_emotion(text: str) -> str:
    lowered = text.lower()
    if any(word in lowered for word in ["boje", "boję", "strach", "panika", "pilnie"]):
        return "anxiety"
    if any(word in lowered for word in ["dziekuje", "dziękuję", "super", "ok"]):
        return "positive"
    if any(word in lowered for word in ["zly", "zły", "wkurzony", "oszust"]):
        return "anger"
    return "neutral"


def explain_ai_assistance(text: str) -> AiAssistanceReport:
    """Explain what the AI/NLP layer helps with for a single message."""

    bot_response = AntiScamDialogBot().respond(text)
    scan_result = calculate_risk(text)
    scan_status = scan_result["status"]
    risk_score = scan_result["risk_score"]
    scan_reasons = scan_result["reasons"]
    blocked_after_scan = scan_status != "LOW RISK"
    scam_pattern = bag_of_words("pilny kod blik konto zablokowane kliknij link natychmiast")
    message_vector = bag_of_words(text)
    similarity = cosine_similarity(message_vector, scam_pattern)

    return AiAssistanceReport(
        purpose="AI explains the scan decision so the user knows why sending was allowed or blocked.",
        intent=bot_response.intent,
        emotion=bot_response.emotion,
        suggested_action=bot_response.message,
        scan_status=scan_status,
        risk_score=risk_score,
        blocked_after_scan=blocked_after_scan,
        block_explanation=explain_scan_block(scan_status, risk_score, scan_reasons),
        scan_reasons=scan_reasons,
        extracted_terms=extract_terms(text)[:8],
        named_entities=extract_named_entities(text),
        scam_similarity=round(similarity, 4),
        what_ai_makes_easier=[
            "recognizes the user's likely intent",
            "detects emotional tone for calmer support",
            "extracts important terms and named entities",
            "compares the message with known scam-like wording",
            "explains why the scan blocked sending instead of only returning a score",
        ],
    )


def explain_scan_block(scan_status: str, risk_score: int, scan_reasons: list[str]) -> str:
    if scan_status == "LOW RISK":
        return (
            f"Sending was not blocked because /scan classified the message as {scan_status} "
            f"with {risk_score}/100 risk."
        )

    reason_text = "; ".join(scan_reasons) if scan_reasons else "the risk score crossed the blocking threshold"
    return (
        f"Sending was blocked because /scan classified the message as {scan_status} "
        f"with {risk_score}/100 risk. Signals: {reason_text}."
    )


def explain_sending_block(text: str, scan_status: str, risk_score: int, scan_reasons: list[str]) -> SendingBlockExplanation:
    """Create the user-facing explanation used when sending/publishing is blocked."""

    bot_response = AntiScamDialogBot().respond(text)
    important_terms = extract_terms(text)[:4]
    terms_text = ", ".join(important_terms) if important_terms else "brak wyraznych terminow"
    reason_text = "; ".join(scan_reasons) if scan_reasons else "wynik ryzyka przekroczyl prog blokady"

    if scan_status == "LOW RISK":
        explanation = (
            f"AI z ai.py nie blokuje wysylania: /scan ocenil tresc jako {scan_status} "
            f"({risk_score}/100). Najwazniejsze terminy: {terms_text}."
        )
    else:
        explanation = (
            f"AI z ai.py wyjasnia blokade: /scan ocenil tresc jako {scan_status} "
            f"({risk_score}/100), bo wykryto sygnaly oszustwa: {reason_text}. "
            f"Najwazniejsze terminy z wiadomosci: {terms_text}."
        )

    return SendingBlockExplanation(
        source="antiscam.ai",
        explanation=explanation,
        recommended_action=bot_response.message,
    )


def _run_block_explanation_bridge() -> None:
    if len(sys.argv) > 1:
        raw_payload = base64.b64decode(sys.argv[1]).decode("utf-8")
        payload = json.loads(raw_payload)
    else:
        payload = json.load(sys.stdin)
    report = explain_sending_block(
        payload.get("text", ""),
        payload.get("scan_status", "LOW RISK"),
        int(payload.get("risk_score", 0)),
        list(payload.get("scan_reasons", [])),
    )
    json.dump(
        {
            "source": report.source,
            "explanation": report.explanation,
            "recommended_action": report.recommended_action,
        },
        sys.stdout,
        ensure_ascii=False,
    )


class KnowledgeGraph:
    def __init__(self) -> None:
        self._edges: dict[str, list[tuple[str, str]]] = defaultdict(list)

    def add(self, subject: str, relation: str, obj: str) -> None:
        self._edges[subject].append((relation, obj))

    def facts_about(self, subject: str) -> list[tuple[str, str]]:
        return list(self._edges.get(subject, []))

    def objects(self, relation: str) -> list[str]:
        return [
            obj
            for edges in self._edges.values()
            for edge_relation, obj in edges
            if edge_relation == relation
        ]


def cloud_deployment_profile() -> dict[str, str]:
    return {
        "iaas": "VM or container host for the API and SQLite volume",
        "paas": "Managed App Service with environment-based configuration",
        "faas": "Serverless scan endpoint for bursty message checks",
        "saas": "Hosted AntiScam dashboard consumed by end users",
    }


if __name__ == "__main__":
    _run_block_explanation_bridge()
