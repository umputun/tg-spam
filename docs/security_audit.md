# Przeglad bezpieczenstwa

## Zakres

Przeglad obejmuje:

- Pythonowy silnik `antiscam/`,
- C# WebAPI `src/AntiScam.Blog.Api/`,
- frontend HTML/CSS/JS w `wwwroot`,
- testy jednostkowe i integracyjne,
- dokumentacje i scenariusze demonstracyjne.

## Checklista

| Obszar | Status | Dowod |
| --- | --- | --- |
| Walidacja danych wejsciowych | OK | `BlogPostValidator`, modele Pydantic |
| Blokada tresci ryzykownych | OK | `RiskAnalyzer`, test `CreatePost_DoesNotPersistPostWhenRiskIsDetected` |
| SQL injection | OK | Parametry `SqliteCommand.Parameters.AddWithValue` |
| XSS w wyswietlaniu wpisow | OK | `escapeHtml` w `wwwroot/app.js` |
| Sekrety w repozytorium | OK | Brak kluczy; dokument `docs/cryptography.md` opisuje zmienne srodowiskowe |
| Kryptografia | OK | PBKDF2-HMAC-SHA256 i AES-GCM-256 z testami |
| Testy regresyjne | OK | `dotnet test AntiScamBlog.sln`, `python -m pytest` |
| Artefakty builda i baza runtime | OK | `.gitignore` ignoruje `bin/`, `obj/`, pliki SQLite runtime |

## Ryzyka szczatkowe

- Scoring phishingu jest heurystyczny i moze dawac falszywe alarmy.
- Projekt nie korzysta jeszcze z zewnetrznej reputacji domen.
- Brak uwierzytelniania uzytkownikow bloga; w obecnym zakresie jest to demonstracja lokalna.
- Brak pipeline CI w repozytorium.

## Rekomendacje

1. Dodac CI uruchamiajace testy Python i .NET.
2. Dodac uwierzytelnianie oraz role dla panelu publikacji.
3. Dodac automatyczne skanowanie zaleznosci.
4. Rozszerzyc reputacje domen o aktualizowana liste z zaufanego zrodla.

## Wynik

Po aktualizacji projekt spelnia wymagania audytowe na poziomie projektu dydaktycznego: dokumentuje ryzyka, pokazuje mitigacje i ma testy potwierdzajace najwazniejsze mechanizmy obronne.
