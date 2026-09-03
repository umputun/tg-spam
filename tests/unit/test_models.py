"""Unit tests for models module."""
import pytest
from pydantic import ValidationError
from antiscam.models import Message, ScanResult


class TestMessage:
    """Test Message model."""

    def test_message_valid_creation(self):
        """Test creating valid Message."""
        msg = Message(text="Test message")
        assert msg.text == "Test message"

    def test_message_empty_text(self):
        """Test Message with empty text is valid."""
        msg = Message(text="")
        assert msg.text == ""

    def test_message_long_text(self):
        """Test Message with long text."""
        long_text = "a" * 10000
        msg = Message(text=long_text)
        assert msg.text == long_text

    def test_message_special_characters(self):
        """Test Message with special characters."""
        msg = Message(text="Cześć! @#$%^&*() 🎉")
        assert msg.text == "Cześć! @#$%^&*() 🎉"

    def test_message_missing_text_field(self):
        """Test Message raises error when text is missing."""
        with pytest.raises(ValidationError):
            Message()

    def test_message_invalid_type(self):
        """Test Message raises error for invalid type."""
        with pytest.raises(ValidationError):
            Message(text=123)


class TestScanResult:
    """Test ScanResult model."""

    def test_scan_result_valid_creation(self):
        """Test creating valid ScanResult."""
        result = ScanResult(
            status="LOW RISK",
            risk_score=25,
            reasons=["Safe message"],
            safe_links=["https://google.com"],
            risky_links=[]
        )
        assert result.status == "LOW RISK"
        assert result.risk_score == 25

    def test_scan_result_high_risk(self):
        """Test ScanResult with high risk status."""
        result = ScanResult(
            status="HIGH RISK",
            risk_score=85,
            reasons=["BLIK detected", "Urgent language"],
            safe_links=[],
            risky_links=["https://phishing.com"]
        )
        assert result.status == "HIGH RISK"
        assert result.risk_score == 85

    def test_scan_result_medium_risk(self):
        """Test ScanResult with medium risk status."""
        result = ScanResult(
            status="MEDIUM RISK",
            risk_score=50,
            reasons=["Suspicious keywords"],
            safe_links=[],
            risky_links=[]
        )
        assert result.status == "MEDIUM RISK"

    def test_scan_result_empty_lists(self):
        """Test ScanResult with empty reason and link lists."""
        result = ScanResult(
            status="LOW RISK",
            risk_score=10,
            reasons=[],
            safe_links=[],
            risky_links=[]
        )
        assert len(result.reasons) == 0
        assert len(result.safe_links) == 0

    def test_scan_result_multiple_reasons(self):
        """Test ScanResult with multiple reasons."""
        reasons = ["Reason 1", "Reason 2", "Reason 3"]
        result = ScanResult(
            status="MEDIUM RISK",
            risk_score=60,
            reasons=reasons,
            safe_links=[],
            risky_links=[]
        )
        assert len(result.reasons) == 3

    def test_scan_result_missing_required_field(self):
        """Test ScanResult raises error for missing field."""
        with pytest.raises(ValidationError):
            ScanResult(
                status="LOW RISK",
                risk_score=25,
                reasons=[]
            )

    def test_scan_result_risk_score_coercion(self):
        """Test ScanResult coerces string to int for risk_score."""
        # Pydantic v2 coerces string to int
        result = ScanResult(
            status="LOW RISK",
            risk_score="25",
            reasons=[],
            safe_links=[],
            risky_links=[]
        )
        assert result.risk_score == 25
        assert isinstance(result.risk_score, int)
