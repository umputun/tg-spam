import pytest
from fastapi.testclient import TestClient
from antiscam.api import app


@pytest.fixture
def client():
    """FastAPI test client for API endpoints."""
    return TestClient(app)


@pytest.fixture
def sample_safe_message():
    """Sample safe message for testing."""
    return "Cześć, spotkamy się normalnie o trzeciej."


@pytest.fixture
def sample_high_risk_message():
    """Sample high-risk scam message."""
    return "BLIK 123456 - wyślij natychmiast kod do potwierdzenia konta!"


@pytest.fixture
def sample_medium_risk_message():
    """Sample medium-risk message."""
    return "Potwierdź swoje dane bankowe szybko: https://malicious-bank.com"


@pytest.fixture
def sample_message_with_trusted_link():
    """Sample message with trusted link."""
    return "Sprawdź artykuł na https://google.com o bezpieczeństwie"

