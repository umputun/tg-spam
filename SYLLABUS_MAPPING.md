# AntiScam - mapowanie na sylabusy

Ten dokument mapuje wymagania z trzech sylabusow PDF na elementy repozytorium AntiScam. Stan po aktualizacji: projekt spelnia wymagania jako projekt demonstracyjno-laboratoryjny, pod warunkiem przedstawienia go razem z dokumentami z katalogu `docs/`.

## Wnioski z analizy sylabusow

1. `Podstawy bezpieczenstwa komputerowego` wymaga identyfikacji wektorow ataku, zapobiegania podatnosciom, przegladu kodu, korzystania ze zrodel i reagowania na incydenty.
2. `Bezpieczenstwo systemow komputerowych` wymaga samodzielnej implementacji rozwiazania bezpieczenstwa, oceny implementacji, narzedzi ochronnych, projektu, pracy pisemnej, prezentacji i demonstracji.
3. `Bezpieczenstwo informatyczne` dodaje mocny nacisk na kryptologie: szyfry, funkcje jednokierunkowe, uwierzytelnianie, zarzadzanie kluczami, PKI, bezpieczne przechowywanie danych, ataki programistyczne oraz aspekty prawne i etyczne.

## Pokrycie efektow: Podstawy bezpieczenstwa komputerowego

| Efekt | Wymaganie | Pokrycie w projekcie |
| --- | --- | --- |
| W1 | Popularne wektory ataku na infrastrukture lokalna lub zdalna | `antiscam/engine.py`, `antiscam/links.py`, `src/AntiScam.Blog.Api/Services/RiskAnalyzer.cs`, `docs/project_report.md` |
| U1 | Wykrywanie i zapobieganie popularnym wektorom ataku | Endpoint `POST /scan`, blokada publikacji `POST /api/posts`, testy integracyjne API |
| U2 | Przeglad kodu pod katem podatnosci | `docs/security_audit.md`, testy regresyjne, brak konkatenacji SQL w repozytorium SQLite |
| U3 | Korzystanie z wlasciwych zrodel | Sekcja z literatura i zrodlami w `docs/project_report.md` oraz `docs/security_audit.md` |
| U4 | Przeciwdzialanie incydentom | `docs/demo.md`, `docs/labs/lab-03-incident-response.md`, odpowiedzi `422` dla ryzykownych wpisow |
| K1-K2 | Ciagle uczenie i kontekst spoleczny | Sekcje etyki, ograniczen i social engineering w dokumentacji |

## Pokrycie efektow: Bezpieczenstwo systemow komputerowych

| Efekt | Wymaganie | Pokrycie w projekcie |
| --- | --- | --- |
| W1 | Metody rozwiazywania problemow bezpieczenstwa | Scoring phishingu, walidacja wejscia, testy, audyt |
| W2 | Prawne wymogi bezpiecznych systemow | `docs/project_report.md` opisuje minimalizacje danych, prywatnosc i odpowiedzialnosc |
| W3 | Narzedzia zapewniania bezpieczenstwa | FastAPI, ASP.NET Core, SQLite, testy, checklisty audytowe |
| U1 | Ocena implementacji i ograniczanie zagrozen | `docs/security_audit.md`, `RiskAnalyzer`, blokada publikacji ryzyka |
| U2 | Bezpieczna implementacja systemow | Parametryzowane SQLite, escaping HTML, walidacja i testy |
| U3 | Dobor narzedzi bezpieczenstwa | Dokumentacja wyboru narzedzi w raporcie i laboratoriach |
| U4/K1 | Wspolpraca i projekt | `docs/labs/`, `docs/demo.md`, `docs/presentation_outline.md` |

## Pokrycie efektow: Bezpieczenstwo informatyczne

| Efekt | Wymaganie | Pokrycie w projekcie |
| --- | --- | --- |
| W1 | Kryptologia, narzedzia i metody bezpieczenstwa | `src/AntiScam.Blog.Api/Security/SecurePasswordHasher.cs`, `AesGcmAuthenticatedEncryptor.cs`, `docs/cryptography.md` |
| U1 | Narzedzia bezpieczenstwa i demonstracja slabych punktow | Laboratoria z phishingiem, walidacja ryzyka, kryptografia i testy integralnosci |
| K1 | Etyka i prawo w atakach | `docs/project_report.md`, `docs/security_audit.md`, sekcje o ograniczeniach demonstracji |

## Formalne deliverables

| Deliverable z sylabusow | Plik w repo |
| --- | --- |
| Projekt | Kod Python + C# WebAPI, testy, `docs/project_report.md` |
| Praca pisemna | `docs/project_report.md` |
| Prezentacja multimedialna | `docs/presentation_outline.md` |
| Demonstracja | `docs/demo.md` |
| Laboratoria/zadania | `docs/labs/` |
| Przeglad bezpieczenstwa | `docs/security_audit.md` |

## Rozszerzenie AI_antiscam

Folder `AI_antiscam/` zawiera oddzielny zestaw 11 sylabusow zwiazanych z AI, NLP, tlumaczeniem, systemami dialogowymi, inzynieria wiedzy, chmura i sztuczna empatia. Ich szczegolowe mapowanie znajduje sie w `docs/ai_syllabus_mapping.md`.

Najwazniejsze dowody zgodnosci:

- `antiscam/ai.py` - lekka warstwa AI/NLP,
- `tests/unit/test_ai.py` - testy jednostkowe komponentow AI,
- `docs/ai_project_report.md` - raport projektu AI,
- `docs/ai_ethics.md` - etyka AI i sztuczna empatia,
- `docs/ai_labs/` - laboratoria dla sylabusow AI.
