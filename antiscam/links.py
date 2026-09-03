import re
import asyncio
import json
from dataclasses import dataclass, field
from urllib.parse import urlsplit
from urllib.request import urlopen

import tldextract

from .config import settings

_extract = tldextract.TLDExtract(suffix_list_urls=(), cache_dir=False)

WARNING_LIST_URLS = (
    "https://hole.cert.pl/domains/domains.json",
)


@dataclass(frozen=True)
class LinkAnalysis:
    safe_links: list[str] = field(default_factory=list)
    risky_links: list[str] = field(default_factory=list)
    typosquatting_links: list[str] = field(default_factory=list)
    warning_list_links: list[str] = field(default_factory=list)


def extract_links(text: str) -> list[str]:
    return re.findall(settings.url_pattern, text)


def extract_domain(url: str) -> str:
    extracted = _extract(url)
    if not extracted.suffix or not extracted.domain:
        return _fallback_registered_domain(url)
    return f"{extracted.domain}.{extracted.suffix}".lower()


def extract_host(url: str) -> str:
    extracted = _extract(url)
    parts = [part for part in [extracted.subdomain, extracted.domain, extracted.suffix] if part]
    host = ".".join(parts).lower()
    return host or _fallback_host(url)


def _fallback_host(url: str) -> str:
    try:
        return (urlsplit(url).hostname or "").lower().removeprefix("www.")
    except ValueError:
        return ""


def _fallback_registered_domain(url: str) -> str:
    host = _fallback_host(url)
    labels = [label for label in host.split(".") if label]
    if len(labels) < 2:
        return ""
    return ".".join(labels[-2:])


def is_trusted_domain(domain: str) -> bool:
    return domain in settings.trusted_domains


def levenshtein_distance(left: str, right: str) -> int:
    if left == right:
        return 0
    if not left:
        return len(right)
    if not right:
        return len(left)

    previous = list(range(len(right) + 1))
    for left_index, left_char in enumerate(left, start=1):
        current = [left_index]
        for right_index, right_char in enumerate(right, start=1):
            current.append(
                min(
                    previous[right_index] + 1,
                    current[right_index - 1] + 1,
                    previous[right_index - 1] + (left_char != right_char),
                )
            )
        previous = current

    return previous[-1]


def is_typosquatting_domain(domain: str, max_distance: int = 2) -> bool:
    if not domain or is_trusted_domain(domain):
        return False

    return any(
        levenshtein_distance(domain, trusted_domain) <= max_distance
        for trusted_domain in settings.trusted_domains
    )


def analyze_links_detailed(text: str) -> LinkAnalysis:
    links = extract_links(text)
    safe_links: list[str] = []
    risky_links: list[str] = []
    typosquatting_links: list[str] = []

    for link in links:
        domain = extract_domain(link)

        if is_trusted_domain(domain):
            safe_links.append(link)
        else:
            risky_links.append(link)
            if is_typosquatting_domain(domain):
                typosquatting_links.append(link)

    return LinkAnalysis(
        safe_links=safe_links,
        risky_links=risky_links,
        typosquatting_links=typosquatting_links,
    )


def analyze_links(text: str) -> tuple[list[str], list[str]]:
    analysis = analyze_links_detailed(text)
    return analysis.safe_links, analysis.risky_links


async def check_external_warning_lists(
    links: list[str],
    fetch_json=None,
    warning_list_urls: tuple[str, ...] = WARNING_LIST_URLS,
) -> list[str]:
    """Check links against external warning-list APIs.

    Tests can pass ``fetch_json`` to avoid network calls. The default implementation
    uses stdlib I/O inside ``asyncio.to_thread``.
    """

    if not links:
        return []

    fetch = fetch_json or _fetch_json
    warning_domains: set[str] = set()
    payloads = await asyncio.gather(
        *(fetch(url) for url in warning_list_urls),
        return_exceptions=True,
    )

    for payload in payloads:
        if isinstance(payload, Exception):
            continue
        warning_domains.update(_extract_warning_domains(payload))

    flagged: list[str] = []
    for link in links:
        domain = extract_domain(link)
        if domain in warning_domains:
            flagged.append(link)

    return flagged


async def _fetch_json(url: str):
    def load():
        with urlopen(url, timeout=3) as response:
            return json.loads(response.read().decode("utf-8"))

    return await asyncio.to_thread(load)


def _extract_warning_domains(payload) -> set[str]:
    if isinstance(payload, list):
        return {str(item).lower() for item in payload if item}
    if isinstance(payload, dict):
        domains = payload.get("domains") or payload.get("data") or payload.get("items") or []
        if isinstance(domains, list):
            return {
                str(item.get("domain", item)).lower() if isinstance(item, dict) else str(item).lower()
                for item in domains
                if item
            }
    return set()
