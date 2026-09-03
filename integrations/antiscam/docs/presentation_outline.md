# Konspekt prezentacji: AntiScam

## Slajd 1: Problem

Phishing, manipulacja, linki i prosby o kody BLIK sa realnym zagrozeniem dla uzytkownikow.

## Slajd 2: Cele projektu

- wykrywac ryzykowne komunikaty,
- blokowac publikacje tresci ryzykownych,
- pokazac bezpieczne wzorce implementacji,
- przygotowac projekt zgodny z wymaganiami sylabusow.

## Slajd 3: Architektura

Python/FastAPI odpowiada za skanowanie wiadomosci. C# ASP.NET Core WebAPI odpowiada za blog, SQLite, HTML i blokade publikacji.

## Slajd 4: Scoring ryzyka

System analizuje BLIK, linki, slowa kluczowe, bezpieczny kontekst i sygnaly manipulacji.

## Slajd 5: Blokada publikacji

Wpis blogowy jest zapisywany tylko dla `LOW RISK`. `MEDIUM RISK` i `HIGH RISK` koncza sie odpowiedzia `422`.

## Slajd 6: Kryptografia

Projekt zawiera PBKDF2-HMAC-SHA256 dla hasel i AES-GCM-256 dla szyfrowania uwierzytelnionego.

## Slajd 7: Przeglad bezpieczenstwa

Walidacja wejscia, parametry SQL, escaping HTML, brak sekretow w repozytorium, testy regresyjne.

## Slajd 8: Testy i demonstracja

Pokaz `dotnet test`, `python -m pytest`, bezpiecznego wpisu i blokady wpisu BLIK.

## Slajd 9: Ograniczenia

Heurystyki nie zastępuja produkcyjnego systemu antyfraudowego. Potrzebne sa reputacja domen, CI i uwierzytelnianie.

## Slajd 10: Wnioski

Projekt laczy implementacje, dokumentacje, audyt, laboratoria i demonstracje, pokrywajac wymagania trzech sylabusow.
