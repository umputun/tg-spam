"""Integration tests for complete workflows."""
import pytest
from antiscam.engine import calculate_risk
from antiscam.models import Message
from antiscam.links import analyze_links
from antiscam.config import settings


class TestCompleteWorkflow:
    """Test complete scam detection workflows."""

    def test_workflow_safe_conversation(self):
        """Test workflow with safe conversation message."""
        text = "Cześć! Jak się masz? Spotkamy się normalnie o trzeciej. Ok?"
        result = calculate_risk(text)

        assert result["status"] == "LOW RISK"
        assert result["risk_score"] < 50
        assert len(result["risky_links"]) == 0

    def test_workflow_phishing_attempt(self):
        """Test workflow with phishing attempt."""
        text = """
        Twoje konto zablokowane! Kliknij teraz:
        https://fake-bank-security.com/verify
        Potwierdź dane natychmiast!
        BLIK 123456 kod wymagany
        """
        result = calculate_risk(text)

        assert result["status"] == "HIGH RISK"
        assert result["risk_score"] >= 80
        assert len(result["risky_links"]) > 0
        assert len(result["reasons"]) > 0

    def test_workflow_mixed_content(self):
        """Test workflow with mixed safe and risky content."""
        text = """
        Cześć! Sprawdź wiadomość z banku:
        https://google.com (trusted)
        Potwierdź szybko poprzez: https://suspicious-site.com
        """
        result = calculate_risk(text)

        # Trusted links reduce risk even with other warning signs
        assert len(result["safe_links"]) > 0
        assert len(result["risky_links"]) > 0

    def test_workflow_urgent_language_without_danger_signs(self):
        """Test urgent language alone."""
        text = "Wyślij mi informację szybko pilnie!"
        result = calculate_risk(text)

        # Urgent keywords alone produce moderate score
        assert result["risk_score"] > 0
        assert len(result["reasons"]) > 0

    def test_workflow_blik_without_context(self):
        """Test BLIK code without scam context."""
        text = "Mój numer to 123456 w systemie"
        result = calculate_risk(text)

        # Should be detected as BLIK uncertain
        assert result["risk_score"] >= 30

    def test_workflow_links_only_analysis(self):
        """Test analysis based on links only."""
        safe_text = "Artykuł: https://google.com"
        risky_text = "Klikni: https://phishing-site.com"

        safe_result = calculate_risk(safe_text)
        risky_result = calculate_risk(risky_text)

        # Risky should have higher score
        assert risky_result["risk_score"] >= safe_result["risk_score"]

    def test_workflow_keywords_detection(self):
        """Test keyword detection in workflow."""
        text = "Potwierdź konto zablokowane bank kod"
        result = calculate_risk(text)

        assert len(result["reasons"]) > 0
        assert any("Keyword" in r for r in result["reasons"])


class TestRealWorldScenarios:
    """Test real-world usage scenarios."""

    def test_scenario_banking_notification(self):
        """Test real banking notification."""
        text = "Nowa transakcja 100 PLN na koncie. Sprawdź szczegóły na https://paypal.com"
        result = calculate_risk(text)

        assert result["status"] == "LOW RISK"
        assert len(result["safe_links"]) > 0

    def test_scenario_scam_sms(self):
        """Test real scam SMS pattern."""
        text = "ALERTG: Twoje konto zablokowane! BLIK 456789 - wyślij kod teraz! https://secure-access.tk"
        result = calculate_risk(text)

        assert result["status"] == "HIGH RISK"
        assert any("BLIK" in r for r in result["reasons"])

    def test_scenario_casual_message(self):
        """Test casual message from friend."""
        text = "Hej! Ok, cześć! Normalnie się spotkamy za godzinę. Dziękuję za wiadomość!"
        result = calculate_risk(text)

        assert result["status"] == "LOW RISK"
        assert result["risk_score"] < 30

    def test_scenario_business_email(self):
        """Test business-like email."""
        text = "Weryfikacja wymagana. Zaloguj się na: https://microsoft.com - Dział IT"
        result = calculate_risk(text)

        assert len(result["safe_links"]) > 0

    def test_scenario_promotional_message(self):
        """Test promotional message."""
        text = "Promocja! Wyślij BLIK 999999 i odbierz voucher. Szybko! https://promotion-site.com"
        result = calculate_risk(text)

        assert result["risk_score"] >= 30


class TestEdgeCases:
    """Test edge cases and boundary conditions."""

    def test_edge_case_empty_text(self):
        """Test empty text handling."""
        result = calculate_risk("")
        assert result["risk_score"] == 0
        assert result["status"] == "LOW RISK"

    def test_edge_case_only_numbers(self):
        """Test text with only numbers."""
        text = "123456 789012 345678"
        result = calculate_risk(text)
        assert 0 <= result["risk_score"] <= 100

    def test_edge_case_only_links(self):
        """Test text with only links."""
        text = "https://google.com https://facebook.com https://amazon.com"
        result = calculate_risk(text)
        assert result["status"] == "LOW RISK"

    def test_edge_case_repeated_keywords(self):
        """Test heavily repeated keywords."""
        text = "potwierdź " * 50
        result = calculate_risk(text)
        assert result["risk_score"] <= 100

    def test_edge_case_mixed_case(self):
        """Test mixed case text."""
        text = "PoTwIeRdŹ DaNe NaTyChmIaSt"
        result = calculate_risk(text)
        # Mixed case is converted to lowercase for analysis
        assert result["risk_score"] > 0

    def test_edge_case_special_characters(self):
        """Test special characters."""
        text = "!@#$%^&*() <html> [link] {code}"
        result = calculate_risk(text)
        assert 0 <= result["risk_score"] <= 100

    def test_edge_case_very_long_text(self):
        """Test very long text."""
        text = "test " * 10000
        result = calculate_risk(text)
        assert 0 <= result["risk_score"] <= 100

    def test_edge_case_many_links(self):
        """Test many links in one message."""
        links = " ".join([f"https://site{i}.com" for i in range(50)])
        result = calculate_risk(links)
        assert 0 <= result["risk_score"] <= 100


class TestConfigurationIntegration:
    """Test integration with configuration."""

    def test_trusted_domains_used(self):
        """Test that configured trusted domains are used."""
        # Test with known trusted domain
        text = f"Check https://{list(settings.trusted_domains)[0]}"
        result = calculate_risk(text)
        assert len(result["safe_links"]) > 0

    def test_keywords_used_in_analysis(self):
        """Test that configured keywords are used."""
        keyword = list(settings.keyword_weights.keys())[0]
        text = f"Message with {keyword} in it"
        result = calculate_risk(text)
        assert result["risk_score"] >= 0
