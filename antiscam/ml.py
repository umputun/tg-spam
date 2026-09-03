from __future__ import annotations

import warnings
from dataclasses import dataclass
from pathlib import Path

import joblib
from sklearn.exceptions import InconsistentVersionWarning
from sklearn.pipeline import Pipeline


@dataclass(frozen=True)
class MlRiskAssessment:
    label: str
    scam_probability: float
    score: int


BASE_DIR = Path(__file__).resolve().parent.parent

MODEL_PATH = BASE_DIR / "models" / "model.joblib"


def load_classifier(path: Path) -> Pipeline:
    with warnings.catch_warnings(record=True) as caught:
        warnings.simplefilter("always")
        classifier = joblib.load(path)

    mismatched_versions = [
        warning for warning in caught
        if issubclass(warning.category, InconsistentVersionWarning)
    ]

    if mismatched_versions:
        warnings.warn(
            "Model version mismatch detected: the saved model was created with a different "
            "scikit-learn version than the runtime. Predictions may be unreliable; rebuild it "
            "with the current environment via 'python train.py'.",
            RuntimeWarning,
            stacklevel=2,
        )

    return classifier


if not MODEL_PATH.exists():
    raise FileNotFoundError(
        f"Nie znaleziono modelu: {MODEL_PATH}. "
        "Uruchom najpierw train.py."
    )

_CLASSIFIER: Pipeline = load_classifier(MODEL_PATH)

_CLASSES = list(_CLASSIFIER.classes_)

if "scam" not in _CLASSES:
    raise RuntimeError(
        "Model nie zawiera klasy 'scam'."
    )

_SCAM_INDEX = _CLASSES.index("scam")


def classify_message(text: str) -> MlRiskAssessment:
    text = text.strip()

    if not text:
        return MlRiskAssessment(
            label="safe",
            scam_probability=0.0,
            score=0,
        )

    probabilities = _CLASSIFIER.predict_proba([text])[0]

    scam_probability = float(
        probabilities[_SCAM_INDEX]
    )

    label = (
        "scam"
        if scam_probability >= 0.5
        else "safe"
    )

    return MlRiskAssessment(
        label=label,
        scam_probability=scam_probability,
        score=round(scam_probability * 100),
    )