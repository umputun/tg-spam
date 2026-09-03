"""Unit tests for scoring module."""
import pytest
from antiscam.scoring import (
    score_keywords,
    score_safe_context,
    score_intent,
    score_blik
)


class TestScoreKeywords:
    """Test keyword scoring functionality."""

    def test_score_keywords_no_keywords(self):
        """Test text with no risky keywords."""
        text = "Cześć, jak się masz?"
        score, reason = score_keywords(text)
        assert score == 0
        assert reason is None

    def test_score_keywords_single_keyword(self):
        """Test text with one risky keyword."""
        text = "potwierdź swoje dane"
        score, reason = score_keywords(text)
        assert score > 0
        assert reason is not None

    def test_score_keywords_multiple_keywords(self):
        """Test text with multiple risky keywords."""
        text = "blik kod przelew szybko wyślij"
        score, reason = score_keywords(text)
        assert score > 0
        assert "Keyword score" in reason

    def test_score_keywords_capped_at_30(self):
        """Test keyword score is capped at 30."""
        text = "blik " * 100 + "kod " * 100 + "przelew " * 100
        score, reason = score_keywords(text)
        assert score <= 30

    def test_score_keywords_repeated_word(self):
        """Test repeated keyword increases score."""
        text1 = "potwierdź"
        text2 = "potwierdź potwierdź"
        score1, _ = score_keywords(text1)
        score2, _ = score_keywords(text2)
        assert score2 > score1


class TestScoreSafeContext:
    """Test safe context scoring functionality."""

    def test_safe_context_no_signals(self):
        """Test text with no safe signals."""
        text = "wyślij pieniądze natychmiast"
        score, reason = score_safe_context(text)
        assert score == 0
        assert reason is None

    def test_safe_context_one_signal(self):
        """Test text with one safe signal."""
        text = "cześć, jak się masz"
        score, reason = score_safe_context(text)
        assert score == 0
        assert reason is None

    def test_safe_context_two_signals(self):
        """Test text with two safe signals."""
        text = "cześć, dziękuję za pomoc"
        score, reason = score_safe_context(text)
        assert score == -10
        assert "Low-risk context detected" in reason

    def test_safe_context_multiple_signals(self):
        """Test text with multiple safe signals."""
        text = "hej, ok, cześć, spotkanie"
        score, reason = score_safe_context(text)
        assert score == -10


class TestScoreIntent:
    """Test intent scoring functionality."""

    def test_score_intent_no_signals(self):
        """Test text with no intent signals."""
        text = "spotkamy się jutro"
        score, reason = score_intent(text)
        assert score == 0
        assert reason is None

    def test_score_intent_one_signal(self):
        """Test text with one intent signal."""
        text = "ostatnia szansa na promocję"
        score, reason = score_intent(text)
        assert score == 10
        assert "Scam intent detected" in reason

    def test_score_intent_konto_zablokowane(self):
        """Test konto zablokowane intent signal."""
        text = "twoje konto zablokowane"
        score, reason = score_intent(text)
        assert score == 10

    def test_score_intent_natychmiast(self):
        """Test natychmiast intent signal."""
        text = "kliknij natychmiast"
        score, reason = score_intent(text)
        assert score == 10


class TestScoreBlik:
    """Test BLIK code scoring functionality."""

    def test_score_blik_no_numbers(self):
        """Test text with no BLIK codes."""
        text = "bez kodów"
        score, reason = score_blik([], text)
        assert score == 0
        assert reason is None

    def test_score_blik_confirmed(self):
        """Test BLIK code with confirming context."""
        numbers = ["123456"]
        text = "blik 123456 kod"
        score, reason = score_blik(numbers, text)
        assert score == 60
        assert "BLIK CONFIRMED" in reason

    def test_score_blik_uncertain(self):
        """Test BLIK code without confirming context."""
        numbers = ["123456"]
        text = "twój numer to 123456"
        score, reason = score_blik(numbers, text)
        assert score == 30
        assert "BLIK UNCERTAIN" in reason

    def test_score_blik_multiple_numbers(self):
        """Test multiple BLIK codes."""
        numbers = ["123456", "789012"]
        text = "blik kod"
        score, reason = score_blik(numbers, text)
        assert score == 60
        assert "123456" in reason
        assert "789012" in reason

    def test_score_blik_with_przelew_context(self):
        """Test BLIK with przelew context."""
        numbers = ["111111"]
        text = "przelew blik 111111"
        score, reason = score_blik(numbers, text)
        assert score == 60
