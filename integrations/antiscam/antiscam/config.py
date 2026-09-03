from dataclasses import dataclass, field


@dataclass
class Settings:
    trusted_domains: set[str] = field(default_factory=lambda: {
        "google.com",
        "facebook.com",
        "messenger.com",
        "microsoft.com",
        "apple.com",
        "amazon.com",
        "paypal.com",
        "chatgpt.com"
    })

    blik_pattern: str = r"\b\d{6}\b"

    url_pattern: str = r"https?://(?:[-\w.]|(?:%[\da-fA-F]{2}))+[^\s]*"

    keyword_weights: dict = field(default_factory=lambda: {
        "blik": 10,
        "kod": 5,
        "przelew": 7,
        "pożycz": 8,
        "szybko": 4,
        "pilnie": 6,
        "natychmiast": 6,
        "wyślij": 5,
        "potwierdź": 5,
        "bank": 3,
        "konto": 3,
        "zablokowane": 7,
        "hasło": 5
    })

    safe_signals: list[str] = field(default_factory=lambda: [
        "dziękuję",
        "spotkanie",
        "normalnie",
        "ok",
        "cześć",
        "hej"
    ])

    intent_signals: list[str] = field(default_factory=lambda: [
        "ostatnia szansa",
        "konto zablokowane",
        "natychmiast",
        "kliknij teraz",
        "potwierdź dane"
    ])


settings = Settings()
