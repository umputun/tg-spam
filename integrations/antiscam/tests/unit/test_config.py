"""Unit tests for config module."""
import pytest
from antiscam.config import Settings


class TestSettings:
    """Test Settings configuration class."""

    def test_settings_initialization(self):
        """Test Settings object can be initialized."""
        settings = Settings()
        assert settings is not None

    def test_trusted_domains_default(self):
        """Test default trusted domains are set correctly."""
        settings = Settings()
        expected_domains = {
            "google.com",
            "facebook.com",
            "messenger.com",
            "microsoft.com",
            "apple.com",
            "amazon.com",
            "paypal.com",
            "chatgpt.com"
        }
        assert settings.trusted_domains == expected_domains

    def test_blik_pattern_is_string(self):
        """Test BLIK pattern is defined."""
        settings = Settings()
        assert isinstance(settings.blik_pattern, str)
        assert settings.blik_pattern == r"\b\d{6}\b"

    def test_url_pattern_is_string(self):
        """Test URL pattern is defined."""
        settings = Settings()
        assert isinstance(settings.url_pattern, str)

    def test_keyword_weights_not_empty(self):
        """Test keyword weights dictionary is populated."""
        settings = Settings()
        assert len(settings.keyword_weights) > 0
        assert settings.keyword_weights["blik"] == 10

    def test_safe_signals_not_empty(self):
        """Test safe signals list is populated."""
        settings = Settings()
        assert len(settings.safe_signals) > 0
        assert "cześć" in settings.safe_signals

    def test_intent_signals_not_empty(self):
        """Test intent signals list is populated."""
        settings = Settings()
        assert len(settings.intent_signals) > 0
        assert "ostatnia szansa" in settings.intent_signals
