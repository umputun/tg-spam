"""Unit tests for the shallow ML risk model."""

import warnings
from pathlib import Path

import pytest
from sklearn.exceptions import InconsistentVersionWarning

import antiscam.ml as ml
from antiscam.ml import classify_message


def test_ml_classifier_scores_scam_intent_above_safe_message():
    scam = classify_message("konto zablokowane kliknij link i potwierdz dane natychmiast")
    safe = classify_message("czesc spotkamy sie jutro o trzeciej")

    assert scam.label == "scam"
    assert scam.score > safe.score


def test_ml_classifier_empty_message_is_zero_risk():
    result = classify_message("")

    assert result.label == "safe"
    assert result.scam_probability == 0.0
    assert result.score == 0


def test_load_classifier_warns_on_inconsistent_version_warning(monkeypatch):
    def fake_load(_path):
        warnings.warn(
            InconsistentVersionWarning(
                estimator_name="Pipeline",
                current_sklearn_version="1.7.2",
                original_sklearn_version="1.8.0",
            )
        )
        return object()

    monkeypatch.setattr(ml.joblib, "load", fake_load)

    with pytest.warns(RuntimeWarning, match="different scikit-learn version|rebuild it"):
        result = ml.load_classifier(Path("models/model.joblib"))

    assert result is not None


def test_load_classifier_does_not_warn_for_normal_load(monkeypatch):
    def fake_load(_path):
        return object()

    monkeypatch.setattr(ml.joblib, "load", fake_load)

    with warnings.catch_warnings(record=True) as caught:
        warnings.simplefilter("always")
        result = ml.load_classifier(Path("models/model.joblib"))

    assert result is not None
    assert not any(issubclass(w.category, RuntimeWarning) for w in caught)
