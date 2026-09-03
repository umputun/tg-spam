TRUSTED_DOMAINS = {
    "google.com",
    "facebook.com",
    "messenger.com",
    "microsoft.com",
    "apple.com",
    "paypal.com",
    "chatgpt.com"
}

BLIK_PATTERN = r"\b\d{6}\b"

URL_PATTERN = r"https?://(?:[-\w.]|(?:%[\da-fA-F]{2}))+[^\s]*"

KEYWORD_WEIGHTS = {
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
}

SAFE_SIGNALS = [
    "dziękuję",
    "spotkanie",
    "normalnie",
    "ok",
    "cześć",
    "hej"
]

INTENT_SIGNALS = [
    "ostatnia szansa",
    "konto zablokowane",
    "natychmiast",
    "kliknij teraz",
    "potwierdź dane"
]