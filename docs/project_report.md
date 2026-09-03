# Raport projektowy: AntiScam

## Cel projektu

AntiScam jest projektem demonstracyjno-laboratoryjnym pokazujacym, jak wykrywac ryzykowne komunikaty phishingowe, jak blokowac publikacje tresci o wysokim ryzyku oraz jak dokumentowac i testowac bezpieczna implementacje uslugi.

Projekt sklada sie z dwoch czesci:

- Python/FastAPI: silnik oceny ryzyka wiadomosci.
- C# ASP.NET Core WebAPI: blog edukacyjny z HTML, SQLite i blokada publikacji tresci ryzykownych.

## Zakres bezpieczenstwa

Projekt obejmuje:

- wykrywanie kodow BLIK i manipulacyjnego kontekstu,
- analize linkow i domen,
- scoring slow kluczowych,
- walidacje danych wejsciowych,
- blokowanie publikacji wpisow `MEDIUM RISK` i `HIGH RISK`,
- parametryzowane zapytania SQLite,
- escaping tresci w frontendzie,
- przyklady kryptografii: PBKDF2-HMAC-SHA256 oraz AES-GCM-256,
- testy jednostkowe i integracyjne.

## Architektura

Pythonowy silnik AntiScam (`antiscam/`) odpowiada za lekka analize tekstu i endpoint `/scan`. Czesc C# (`src/AntiScam.Blog.Api/`) udostepnia blog, statyczne pliki HTML/CSS/JS, REST API oraz repozytorium SQLite.

Ryzykowny wpis blogowy nie jest zapisywany. API zwraca `422 Unprocessable Entity` z wynikiem oceny ryzyka, co jest testowane w `tests/AntiScam.Blog.Api.Tests/Integration/BlogApiTests.cs`.

## Kryptografia

Modul `src/AntiScam.Blog.Api/Security/` zawiera dwa kontrolowane przyklady:

- `AesGcmAuthenticatedEncryptor`: szyfrowanie uwierzytelnione AES-GCM-256 z losowym nonce i dodatkowo uwierzytelnianymi danymi.

Klucze szyfrujace nie sa przechowywane w repozytorium. W scenariuszu produkcyjnym powinny pochodzic z menedzera sekretow lub zmiennych srodowiskowych.

## Wyniki testow

Weryfikacja lokalna:

```powershell
dotnet test AntiScamBlog.sln
python -m pytest
```

Oczekiwany wynik po aktualizacji:

- testy C#: obejmuja walidacje, slugi, ocene ryzyka, blokade publikacji, SQLite, HTML i kryptografie,
- testy Python: obejmuja scoring, modele, linki, API i workflow.

## Ograniczenia

Silnik oceny ryzyka jest deterministyczny i edukacyjny. Nie zastępuje profesjonalnego systemu antyfraudowego ani aktualizowanej bazy reputacji URL. Projekt celowo nie wykonuje prawdziwych atakow i nie sluzy do omijania zabezpieczen.

## Aspekty prawne i etyczne

Demonstracje powinny odbywac sie na danych testowych, bez prawdziwych danych osobowych, hasel, tokenow i aktywnych linkow do zlosliwych domen. Laboratoria pokazuja bezpieczne wzorce obronne i nie zawieraja instrukcji szkodliwego wykorzystania podatnosci poza kontrolowanym scenariuszem edukacyjnym.

## Dalszy rozwoj

- integracja z aktualizowana lista reputacji domen,
- role i uwierzytelnianie dla panelu publikacji,
- raport HTML/PDF z wynikow skanowania,
- automatyczny pipeline CI z testami i audytem zaleznosci.
