"""Unit tests for links module."""
import pytest
from antiscam.links import (
    check_external_warning_lists,
    extract_links,
    extract_domain,
    extract_host,
    is_trusted_domain,
    is_typosquatting_domain,
    levenshtein_distance,
    analyze_links,
    analyze_links_detailed,
)


class TestExtractLinks:
    """Test link extraction functionality."""

    def test_extract_single_link(self):
        """Test extraction of a single link."""
        text = "Check this https://example.com for details"
        links = extract_links(text)
        assert len(links) == 1
        assert "https://example.com" in links[0]

    def test_extract_multiple_links(self):
        """Test extraction of multiple links."""
        text = "Visit https://google.com or https://facebook.com"
        links = extract_links(text)
        assert len(links) == 2

    def test_extract_no_links(self):
        """Test text without links returns empty list."""
        text = "This is just plain text without any links"
        links = extract_links(text)
        assert len(links) == 0

    def test_extract_http_link(self):
        """Test extraction of http (not https) link."""
        text = "Visit http://example.com"
        links = extract_links(text)
        assert len(links) == 1


class TestExtractDomain:
    """Test domain extraction functionality."""

    def test_extract_domain_https(self):
        """Test extracting domain from HTTPS URL."""
        url = "https://google.com/search?q=test"
        domain = extract_domain(url)
        assert domain == "google.com"

    def test_extract_domain_www(self):
        """Test extracting domain with www prefix."""
        url = "https://www.example.com"
        domain = extract_domain(url)
        assert domain == "example.com"

    def test_extract_domain_uses_registered_domain_not_pasted_subdomain(self):
        """Test pasted trusted domain in subdomain is not trusted."""
        url = "https://google.com.evil.example/login"
        domain = extract_domain(url)
        assert domain == "evil.example"

    def test_extract_host_keeps_subdomain_for_diagnostics(self):
        """Test full host extraction keeps subdomains."""
        url = "https://login.google.com/search"
        host = extract_host(url)
        assert host == "login.google.com"

    def test_extract_domain_lowercase(self):
        """Test domain is converted to lowercase."""
        url = "https://GOOGLE.COM"
        domain = extract_domain(url)
        assert domain == "google.com"

    def test_extract_domain_invalid_url(self):
        """Test invalid URL returns empty string."""
        url = "not a url"
        domain = extract_domain(url)
        assert domain == ""


class TestIsTrustedDomain:
    """Test trusted domain verification."""

    def test_trusted_domain_google(self):
        """Test Google is recognized as trusted."""
        assert is_trusted_domain("google.com") is True

    def test_trusted_domain_paypal(self):
        """Test PayPal is recognized as trusted."""
        assert is_trusted_domain("paypal.com") is True

    def test_untrusted_domain(self):
        """Test untrusted domain is rejected."""
        assert is_trusted_domain("malicious-site.com") is False

    def test_subdomain_of_trusted(self):
        """Test registered domain extracted from subdomain is trusted."""
        assert is_trusted_domain(extract_domain("https://mail.google.com")) is True

    def test_similar_but_different_domain(self):
        """Test similar domain name is not trusted."""
        assert is_trusted_domain("googl.com") is False

    def test_pasted_trusted_domain_in_subdomain_is_not_trusted(self):
        """Test google.com.evil.example is not accepted as google.com."""
        assert is_trusted_domain(extract_domain("https://google.com.evil.example")) is False


class TestTyposquatting:
    """Test edit-distance typosquatting detection."""

    def test_levenshtein_distance_counts_edits(self):
        """Test Levenshtein implementation."""
        assert levenshtein_distance("g00gle.com", "google.com") == 2

    def test_typosquatting_domain_detected(self):
        """Test close trusted-domain lookalike is detected."""
        assert is_typosquatting_domain("g00gle.com") is True

    def test_unrelated_domain_not_typosquatting(self):
        """Test unrelated domain is not typosquatting."""
        assert is_typosquatting_domain("malicious-site.com") is False


class TestAnalyzeLinks:
    """Test link analysis functionality."""

    def test_analyze_no_links(self):
        """Test text with no links."""
        text = "Plain text without links"
        safe, risky = analyze_links(text)
        assert len(safe) == 0
        assert len(risky) == 0

    def test_analyze_trusted_link_only(self):
        """Test text with only trusted links."""
        text = "Check https://google.com"
        safe, risky = analyze_links(text)
        assert len(safe) == 1
        assert len(risky) == 0

    def test_analyze_risky_link_only(self):
        """Test text with only risky links."""
        text = "Click https://malicious.com"
        safe, risky = analyze_links(text)
        assert len(safe) == 0
        assert len(risky) == 1

    def test_analyze_mixed_links(self):
        """Test text with both trusted and risky links."""
        text = "Visit https://google.com or https://phishing-site.com"
        safe, risky = analyze_links(text)
        assert len(safe) == 1
        assert len(risky) == 1

    def test_analyze_multiple_risky_links(self):
        """Test multiple risky links."""
        text = "Links: https://bad1.com https://bad2.com https://bad3.com"
        safe, risky = analyze_links(text)
        assert len(risky) == 3

    def test_analyze_detailed_flags_typosquatting(self):
        """Test detailed analysis flags lookalike trusted domains."""
        analysis = analyze_links_detailed("Kliknij https://g00gle.com/login")
        assert analysis.safe_links == []
        assert analysis.risky_links == ["https://g00gle.com/login"]
        assert analysis.typosquatting_links == ["https://g00gle.com/login"]


class TestExternalWarningLists:
    """Test async external warning-list integration."""

    @pytest.mark.anyio
    async def test_check_external_warning_lists_with_injected_fetcher(self):
        """Test warning list checks without network dependency."""

        async def fake_fetch(_url):
            return ["phishing.test", "bad.example"]

        flagged = await check_external_warning_lists(
            ["https://phishing.test/login", "https://google.com"],
            fetch_json=fake_fetch,
        )

        assert flagged == ["https://phishing.test/login"]
