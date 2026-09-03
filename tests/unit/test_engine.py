"""Unit tests for engine module."""
import pytest
from antiscam.engine import detect_blik, calculate_risk


class TestDetectBlik:
    """Test BLIK code detection."""

    def test_detect_blik_single_code(self):
        """Test detection of single BLIK code."""
        text = "Wyślij 123456"
        codes = detect_blik(text)
        assert len(codes) == 1
        assert "123456" in codes

    def test_detect_blik_multiple_codes(self):
        """Test detection of multiple BLIK codes."""
        text = "Kody: 111111 222222 333333"
        codes = detect_blik(text)
        assert len(codes) == 3

    def test_detect_blik_no_codes(self):
        """Test text with no BLIK codes."""
        text = "Normalny tekst bez kodów"
        codes = detect_blik(text)
        assert len(codes) == 0

    def test_detect_blik_invalid_format(self):
        """Test invalid code format is not detected."""
        text = "Kod 12345 lub 1234567"
        codes = detect_blik(text)
        assert len(codes) == 0

    def test_detect_blik_case_insensitive(self):
        """Test BLIK detection is case independent."""
        text = "BLIK 654321"
        codes = detect_blik(text)
        assert "654321" in codes


class TestCalculateRisk:
    """Test risk calculation functionality."""

    def test_calculate_risk_low_risk_safe_message(self):
        """Test low-risk classification for safe message."""
        text = "Cześć, spotkamy się normalnie"
        result = calculate_risk(text)
        assert result["status"] == "LOW RISK"
        assert result["risk_score"] < 50

    def test_calculate_risk_high_risk_blik_code(self):
        """Test high-risk classification with BLIK code."""
        text = "Wyślij BLIK 123456 natychmiast kod potwierdź"
        result = calculate_risk(text)
        assert result["status"] == "HIGH RISK"
        assert result["risk_score"] >= 80

    def test_calculate_risk_deobfuscates_spaced_blik(self):
        """Test de-obfuscation before risk scoring."""
        text = "Wyślij b l i k 123456 k o d natychmiast"
        result = calculate_risk(text)
        assert result["status"] == "HIGH RISK"
        assert result["risk_score"] >= 80

    def test_calculate_risk_deobfuscates_homoglyph_blik(self):
        """Test homoglyph mapping before risk scoring."""
        text = "Wyślij ВLΙК 123456 kod natychmiast"
        result = calculate_risk(text)
        assert result["status"] == "HIGH RISK"
        assert result["risk_score"] >= 80

    def test_calculate_risk_medium_risk_keywords(self):
        """Test risky keywords increase score."""
        text = "Potwierdź dane szybko pilnie"
        result = calculate_risk(text)
        # Keywords increase risk but may not reach 30 without other factors
        assert result["risk_score"] > 10

    def test_calculate_risk_has_reasons(self):
        """Test result includes reasons."""
        text = "BLIK 999999 kod"
        result = calculate_risk(text)
        assert len(result["reasons"]) > 0

    def test_calculate_risk_result_structure(self):
        """Test result has required fields."""
        text = "Test message"
        result = calculate_risk(text)
        assert "status" in result
        assert "risk_score" in result
        assert "reasons" in result
        assert "safe_links" in result
        assert "risky_links" in result

    def test_calculate_risk_score_range(self):
        """Test risk score is in valid range 0-100."""
        texts = [
            "Cześć",
            "BLIK 123456",
            "Wyślij pieniądze natychmiast kod potwierdź konto zablokowane"
        ]
        for text in texts:
            result = calculate_risk(text)
            assert 0 <= result["risk_score"] <= 100

    def test_calculate_risk_trusted_links_decrease_score(self):
        """Test trusted links decrease risk score."""
        text1 = "Sprawdź https://malicious-phishing.com"
        text2 = "Sprawdź https://google.com"
        result1 = calculate_risk(text1)
        result2 = calculate_risk(text2)
        # Result2 should have lower or equal score due to trusted link
        assert result2["risk_score"] <= result1["risk_score"]

    def test_calculate_risk_risky_links_increase_score(self):
        """Test risky links increase risk score."""
        text1 = "Normalny tekst"
        text2 = "Normalny tekst https://phishing.com"
        result1 = calculate_risk(text1)
        result2 = calculate_risk(text2)
        assert result2["risk_score"] >= result1["risk_score"]

    def test_calculate_risk_includes_ml_baseline(self):
        """Test hybrid pipeline starts with the shallow ML model."""
        result = calculate_risk("konto zablokowane kliknij link i potwierdz dane")
        assert any("ML intent score" in reason for reason in result["reasons"])
        assert result["risk_score"] > 0

    def test_calculate_risk_typosquatting_link_is_high_risk(self):
        """Test close lookalike of trusted domain is high risk."""
        result = calculate_risk("Kliknij https://g00gle.com/login")
        assert result["status"] == "HIGH RISK"
        assert any("Typosquatting links" in reason for reason in result["reasons"])

    def test_calculate_risk_status_classifications(self):
        """Test risk classifications."""
        low_text = "Cześć spotkanie ok"
        medium_text = "BLIK 456789 kod potwierdź natychmiast wyślij"
        high_text = "BLIK 123456 kod wyślij natychmiast potwierdź konto zablokowane"

        low_result = calculate_risk(low_text)
        medium_result = calculate_risk(medium_text)
        high_result = calculate_risk(high_text)

        assert low_result["status"] == "LOW RISK"
        assert medium_result["status"] in ["MEDIUM RISK", "HIGH RISK"]
        assert high_result["status"] == "HIGH RISK"
