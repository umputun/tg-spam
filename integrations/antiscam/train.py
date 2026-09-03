from pathlib import Path

import joblib
from sklearn.feature_extraction.text import TfidfVectorizer
from sklearn.naive_bayes import MultinomialNB
from sklearn.pipeline import Pipeline

TRAINING_SAMPLES = (
    ("wyslij kod blik natychmiast ostatnia szansa", "scam"),
    ("konto zablokowane kliknij link i potwierdz dane", "scam"),
    ("doplat 1 zl do paczki inaczej zwrot przesylki", "scam"),
    ("bank wymaga pilnej weryfikacji hasla", "scam"),
    ("odbierz nagrode podaj kod sms teraz", "scam"),
    ("twoje konto zostanie zablokowane zaloguj sie natychmiast", "scam"),
    ("prosimy o szybki przelew na nowy rachunek", "scam"),
    ("kliknij teraz aby uniknac blokady konta", "scam"),
    ("czesc spotkamy sie jutro o trzeciej", "safe"),
    ("dziekuje za dokument wysle poprawki wieczorem", "safe"),
    ("przypomnienie o spotkaniu zespolu w poniedzialek", "safe"),
    ("artykul edukacyjny o ochronie przed phishingiem", "safe"),
    ("potwierdzam odbior paczki wszystko ok", "safe"),
    ("normalna wiadomosc od znajomego bez linku", "safe"),
    ("raport jest gotowy do omowienia na zajeciach", "safe"),
    ("hej czy mozesz oddzwonic po pracy", "safe"),
)

MODEL_DIR = Path("models")
MODEL_PATH = MODEL_DIR / "model.joblib"


def train() -> None:
    classifier = Pipeline(
        [
            (
                "tfidf",
                TfidfVectorizer(
                    ngram_range=(1, 2),
                    lowercase=True,
                ),
            ),
            (
                "nb",
                MultinomialNB(alpha=0.5),
            ),
        ]
    )

    texts = [text for text, _ in TRAINING_SAMPLES]
    labels = [label for _, label in TRAINING_SAMPLES]

    classifier.fit(texts, labels)

    MODEL_DIR.mkdir(exist_ok=True)

    joblib.dump(classifier, MODEL_PATH)

    print(f"Model zapisany: {MODEL_PATH}")


if __name__ == "__main__":
    train()