"""Integration tests for API endpoints."""
import pytest
from fastapi.testclient import TestClient
from antiscam.api import app


@pytest.fixture
def client():
    """Create a test client for the API."""
    return TestClient(app)


class TestHomeEndpoint:
    """Test home endpoint."""

    def test_home_endpoint_returns_200(self, client):
        """Test home endpoint returns 200 status."""
        response = client.get("/")
        assert response.status_code == 200

    def test_home_endpoint_returns_message(self, client):
        """Test home endpoint returns expected message."""
        response = client.get("/")
        data = response.json()
        assert "message" in data
        assert "AntiScam API is running" in data["message"]


class TestScanEndpoint:
    """Test scan endpoint."""

    def test_scan_safe_message(self, client):
        """Test scanning a safe message."""
        response = client.post(
            "/scan",
            json={"text": "Cześć, spotkamy się normalnie o trzeciej."}
        )
        assert response.status_code == 200
        data = response.json()
        assert data["status"] == "LOW RISK"
        assert data["risk_score"] < 50

    def test_scan_high_risk_message(self, client):
        """Test scanning a high-risk message."""
        response = client.post(
            "/scan",
            json={"text": "BLIK 123456 wyślij kod natychmiast potwierdź!"}
        )
        assert response.status_code == 200
        data = response.json()
        assert data["status"] == "HIGH RISK"

    def test_scan_deobfuscates_blik_message(self, client):
        """Test scan endpoint normalizes obfuscated BLIK wording."""
        response = client.post(
            "/scan",
            json={"text": "В L Ι К 123456 k o d natychmiast"}
        )
        assert response.status_code == 200
        data = response.json()
        assert data["status"] == "HIGH RISK"

    def test_scan_response_structure(self, client):
        """Test response has required fields."""
        response = client.post(
            "/scan",
            json={"text": "Test message"}
        )
        assert response.status_code == 200
        data = response.json()
        assert "status" in data
        assert "risk_score" in data
        assert "reasons" in data
        assert "safe_links" in data
        assert "risky_links" in data

    def test_scan_empty_message(self, client):
        """Test scanning empty message."""
        response = client.post(
            "/scan",
            json={"text": ""}
        )
        assert response.status_code == 200
        data = response.json()
        assert data["risk_score"] == 0

    def test_scan_missing_text_field(self, client):
        """Test scan endpoint returns 422 for missing text field."""
        response = client.post(
            "/scan",
            json={}
        )
        assert response.status_code == 422

    def test_scan_invalid_json(self, client):
        """Test scan endpoint with invalid JSON."""
        response = client.post(
            "/scan",
            content="invalid json"
        )
        assert response.status_code == 422

    def test_scan_with_trusted_links(self, client):
        """Test scanning message with trusted links."""
        response = client.post(
            "/scan",
            json={"text": "Sprawdź artykuł na https://google.com"}
        )
        assert response.status_code == 200
        data = response.json()
        assert len(data["safe_links"]) > 0

    def test_scan_with_risky_links(self, client):
        """Test scanning message with risky links."""
        response = client.post(
            "/scan",
            json={"text": "Kliknij https://malicious-phishing.com"}
        )
        assert response.status_code == 200
        data = response.json()
        assert len(data["risky_links"]) > 0

    def test_scan_typosquatting_link_is_high_risk(self, client):
        """Test scan endpoint flags edit-distance domain spoofing."""
        response = client.post(
            "/scan",
            json={"text": "Pilnie kliknij https://g00gle.com/login"}
        )
        assert response.status_code == 200
        data = response.json()
        assert data["status"] == "HIGH RISK"
        assert any("Typosquatting links" in reason for reason in data["reasons"])

    def test_scan_with_blik_code(self, client):
        """Test scanning message with BLIK code."""
        response = client.post(
            "/scan",
            json={"text": "Kod BLIK 654321"}
        )
        assert response.status_code == 200
        data = response.json()
        assert data["risk_score"] >= 30

    def test_scan_multiple_requests(self, client):
        """Test multiple scan requests work correctly."""
        messages = [
            "Cześć",
            "BLIK 123456",
            "https://google.com",
            "Potwierdź dane szybko"
        ]
        responses = []
        for msg in messages:
            response = client.post("/scan", json={"text": msg})
            assert response.status_code == 200
            responses.append(response.json())

        # Verify responses are independent
        assert len(responses) == len(messages)
        for resp in responses:
            assert "status" in resp

    def test_scan_long_message(self, client):
        """Test scanning very long message."""
        long_text = "test " * 1000
        response = client.post(
            "/scan",
            json={"text": long_text}
        )
        assert response.status_code == 200
        data = response.json()
        assert "risk_score" in data

    def test_scan_unicode_characters(self, client):
        """Test scanning message with unicode characters."""
        response = client.post(
            "/scan",
            json={"text": "Cześć 你好 مرحبا 🎉"}
        )
        assert response.status_code == 200
        data = response.json()
        assert "status" in data

    def test_scan_risk_score_range(self, client):
        """Test risk score is always in valid range."""
        messages = [
            "Cześć",
            "BLIK 123456 kod przelew natychmiast",
            "https://google.com https://phishing.com"
        ]
        for msg in messages:
            response = client.post("/scan", json={"text": msg})
            data = response.json()
            assert 0 <= data["risk_score"] <= 100


class TestEndpointIntegration:
    """Test integration between endpoints."""

    def test_endpoints_independent(self, client):
        """Test endpoints don't affect each other."""
        # Call home endpoint
        home_response = client.get("/")
        assert home_response.status_code == 200

        # Call scan endpoint
        scan_response = client.post(
            "/scan",
            json={"text": "Test"}
        )
        assert scan_response.status_code == 200

        # Call home endpoint again
        home_response2 = client.get("/")
        assert home_response2.status_code == 200


class TestAiExplainEndpoint:
    """Test endpoint that explains practical AI assistance."""

    def test_ai_explain_returns_actionable_report(self, client):
        response = client.post(
            "/ai/explain",
            json={"text": "Boję się, Bank Polska chce kod BLIK 123456 pilnie"}
        )

        assert response.status_code == 200
        data = response.json()
        assert data["intent"] == "report_scam"
        assert data["emotion"] == "anxiety"
        assert data["scan_status"] == "HIGH RISK"
        assert data["risk_score"] >= 80
        assert data["blocked_after_scan"] is True
        assert "blocked" in data["block_explanation"]
        assert data["scan_reasons"]
        assert data["scam_similarity"] > 0
        assert "suggested_action" in data
        assert "Bank Polska" in data["named_entities"]
