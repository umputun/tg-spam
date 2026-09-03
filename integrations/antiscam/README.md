# AntiScam

AntiScam to repozytorium z dwoma częściami:

- silnikiem Python/FastAPI do oceny ryzyka wiadomości phishingowych,
- blogiem C# ASP.NET Core WebAPI z plikami HTML i bazą SQLite.

Nowy blog jest połączony z folderem roboczym, a domyślna baza SQLite powstaje w `data/antiscam-blog.sqlite` (względnie do katalogu projektu).

## Wymagania

- Python 3.10+
- .NET SDK 8.0+
- pip
- OpenSSL (opcjonalnie, do generowania certyfikatów HTTPS)

## Szybki start: C# Blog WebAPI

```powershell
dotnet restore AntiScamBlog.sln
dotnet run --project src\AntiScam.Blog.Api\AntiScam.Blog.Api.csproj
```

Po uruchomieniu aplikacja jest dostępna pod adresem wyświetlonym przez `dotnet run`, zwykle `http://localhost:5000` albo `http://localhost:5080`.

### Frontend HTML

Otwórz w przeglądarce:

```text
/
```

Strona ładuje wpisy z API i pozwala dodać nowy wpis blogowy.

Przed publikacją C# WebAPI analizuje tytuł, streszczenie, treść i autora. Wpis zostanie zapisany tylko przy statusie `LOW RISK`; dla `MEDIUM RISK` lub `HIGH RISK` API zwraca `422 Unprocessable Entity` i nie zapisuje wpisu w SQLite.

### Opcjonalna baza NoSQL (MongoDB)

SQLite nadal jest domyślną bazą wpisów bloga. Opcjonalna baza MongoDB przechowuje jedynie zgłoszenia odrzucone przez analizę ryzyka, więc nie zmienia działania istniejącego magazynu SQLite.

MongoDB jest włączony przez `NoSql:Enabled`. Adres serwera można podać w `NoSql:ConnectionString` albo przez zmienną środowiskową `ANTISCAM_MONGO_CONNECTION_STRING`. Domyślna baza i kolekcja to `antiscam` oraz `blocked-submissions`. Bieżącą konfigurację obu magazynów zwraca `GET /api/storage`.

Odrzucone zgłoszenia można odczytać przez `GET /api/incidents?limit=50`. Po włączeniu MongoDB endpoint zwraca wpisy z kolekcji `blocked-submissions`, od najnowszych. Przy wyłączonej lub niedostępnej bazie odpowiedzią jest pusta lista.

### Endpointy bloga

```text
GET    /api/health
GET    /api/storage
GET    /api/incidents?limit=50
GET    /api/workspace
GET    /api/posts
GET    /api/posts/{slug}
POST   /api/posts
PUT    /api/posts/{id}
DELETE /api/posts/{id}
```

Przykład dodania wpisu:

```powershell
curl -Method POST http://localhost:5000/api/posts `
  -ContentType "application/json" `
  -Body '{"title":"Alarm phishingowy","summary":"Krótki opis","content":"Treść wpisu","author":"AntiScam Team"}'
```

## Szybki start: Python AntiScam API

```powershell
pip install -r requirements-dev.txt
pip install -e .
uvicorn antiscam.api:app --reload
```

API Pythonowe będzie dostępne pod adresem `http://localhost:8000`.

### Endpointy Python API

```text
GET  /
POST /scan
POST /ai/explain
```

Przykład:

```powershell
curl -Method POST http://localhost:8000/scan `
  -ContentType "application/json" `
  -Body '{"text":"Wyślij BLIK 123456 natychmiast!"}'
```

Endpoint `/ai/explain` pokazuje praktycznie, co ułatwia AI/NLP w projekcie: rozpoznaje intencję użytkownika, ton emocjonalny, ważne terminy, nazwy własne, podobieństwo do wzorca oszustwa i sugeruje bezpieczne następne działanie.

```powershell
curl -Method POST http://localhost:8000/ai/explain `
  -ContentType "application/json" `
  -Body '{"text":"Boję się, Bank Polska chce kod BLIK 123456 pilnie"}'
```

### Trenowanie modelu ML

Projekt zawiera prosty model Machine Learning (TF-IDF + Multinomial Naive Bayes) do klasyfikacji wiadomości. Aby wytrenować model:

```powershell
python train.py
```

Skrypt trenuje na 16 próbkach treningowych (8 phishingowych, 8 bezpiecznych) i zapisuje model w `models/model.joblib`. Model jest następnie używany przez Python API do analizy ryzyka wiadomości.

### Dokumentacja API (Swagger UI / OpenAPI)

Obie aplikacje udostępniają interaktywną dokumentację:

**C# Blog WebAPI:**
- Swagger UI: `http://localhost:5000/swagger/ui`
- OpenAPI JSON: `http://localhost:5000/swagger/v1/swagger.json`

**Python AntiScam API:**
- Swagger UI: `http://localhost:8000/docs`
- ReDoc: `http://localhost:8000/redoc`
- OpenAPI JSON: `http://localhost:8000/openapi.json`

## Uruchamianie obu aplikacji jednocześnie

Aby uruchomić projekt w pełni (blog + AI engine), otwórz dwa terminale i uruchom w każdym:

**Terminal 1 - C# Blog API:**
```powershell
dotnet run --project src\AntiScam.Blog.Api\AntiScam.Blog.Api.csproj
```

**Terminal 2 - Python AntiScam API:**
```powershell
pip install -r requirements-dev.txt
pip install -e .
uvicorn antiscam.api:app --reload
```

Blog będzie dostępny na `http://localhost:5000`, a Python API na `http://localhost:8000`.

## Testy

Testy C#:

```powershell
dotnet test AntiScamBlog.sln
```

Testy Python:

```powershell
pytest
```

Projekt C# zawiera testy jednostkowe dla walidacji i slugów oraz testy integracyjne API, SQLite i statycznej strony HTML.
Obejmuje też testy blokowania publikacji wpisów, w których wykryto ryzyko phishingu lub oszustwa.
Obejmuje również testy kryptografii AES-GCM-256.

## Zmienne środowiskowe

Pełna lista zmiennych środowiskowych używanych w projekcie:

| Zmienna | Opis | Domyślnie | Przykład |
|---------|------|----------|----------|
| `ANTISCAM_BLOG_DB` | Ścieżka do bazy SQLite blogu | `data/antiscam-blog.sqlite` | `C:\temp\antiscam-blog.sqlite` |
| `ANTISCAM_MONGO_CONNECTION_STRING` | Adres serwera MongoDB (opcjonalne) | - | `mongodb+srv://user:pass@cluster.mongodb.net` |
| `ANTISCAM_HTTPS_CERT_PASSWORD` | Hasło do certyfikatu HTTPS (OpenSSL) | - | `silne-lokalne-haslo` |

**Ustawianie zmiennych w PowerShell:**
```powershell
$env:ANTISCAM_BLOG_DB="C:\temp\antiscam-blog.sqlite"
```

## Zgodność z sylabusami

**Projekt w pełni spełnia wymagania sylabusów trzech przedmiotów: Podstawy bezpieczeństwa komputerowego, Bezpieczeństwo systemów komputerowych oraz Bezpieczeństwo informatyczne.**

Folder `antiscam` zawiera implementację wszystkich wymaganych efektów uczenia się, a materiały wymagane do oceny projektu znajdują się w:

- `SYLLABUS_MAPPING.md` - mapowanie efektów uczenia się na kod i dokumentację,
- `docs/ai_syllabus_mapping.md` - mapowanie sylabusów z folderu `AI_antiscam`,
- `docs/project_report.md` - raport projektowy,
- `docs/ai_project_report.md` - raport rozszerzenia AI/NLP,
- `docs/security_audit.md` - przegląd bezpieczeństwa i checklista,
- `docs/cryptography.md` - opis hashowania, szyfrowania i zarządzania kluczami,
- `docs/ai_ethics.md` - etyka AI i sztuczna empatia,
- `docs/demo.md` - scenariusz demonstracji,
- `docs/presentation_outline.md` - konspekt prezentacji,
- `docs/labs/` - instrukcje laboratoryjne bezpieczeństwa,
- `docs/ai_labs/` - instrukcje laboratoryjne AI/NLP.

## Struktura

```text
antiscam/                                  Pythonowy silnik AntiScam
antiscam/ai.py                             Edukacyjne komponenty AI/NLP
tests/                                     Testy Python
train.py                                   Trenowanie modelu ML (TF-IDF + Naive Bayes)
src/AntiScam.Blog.Api/                    C# ASP.NET Core Blog WebAPI
src/AntiScam.Blog.Api/wwwroot/            Pliki HTML, CSS i JS
tests/AntiScam.Blog.Api.Tests/            Testy jednostkowe i integracyjne C#
docs/                                     Raport, audyt, demo i laboratoria
docs/ai_labs/                              Laboratoria dla sylabusów AI_antiscam
SYLLABUS_MAPPING.md                       Mapowanie projektu na sylabusy
AntiScamBlog.sln                          Rozwiązanie .NET
README.md                                 Dokumentacja PL
README.en.md                              Dokumentacja EN
```

## Konfiguracja C# Blog WebAPI

Domyślne ustawienia są w `src/AntiScam.Blog.Api/appsettings.json`:

```json
{
  "Workspace": {
    "RootPath": "C:\\Users\\kondz\\antiscam"
  },
  "Blog": {
    "DatabasePath": "C:\\Users\\kondz\\antiscam\\data\\antiscam-blog.sqlite"
  }
}
```

Do testów lub lokalnych eksperymentów można nadpisać ścieżkę bazy zmienną środowiskową:

```powershell
$env:ANTISCAM_BLOG_DB="C:\temp\antiscam-blog.sqlite"
```

### HTTPS w sieci lokalnej (OpenSSL)

Skrypt `tools/generate-https-certificate.ps1` tworzy lokalne CA oraz certyfikat PFX z prywatnym adresem IP w SAN, analogicznie do projektu referencyjnego. Po zainstalowaniu OpenSSL uruchom:

```powershell
$env:ANTISCAM_HTTPS_CERT_PASSWORD = "silne-lokalne-haslo"
.\tools\generate-https-certificate.ps1 -PrivateIp "192.168.1.22"
```

Następnie ustaw `Https:Enabled` na `true` i uruchom aplikację. Kestrel będzie nasłuchiwał na `0.0.0.0:5001`, a aplikacja będzie dostępna z LAN jako `https://192.168.1.22:5001`. Aby usunąć ostrzeżenie przeglądarki na urządzeniach w sieci, zaufaj plikowi `certs/antiscam-ca.crt`.

Bez certyfikatu zwykłe `dotnet run --project .\src\AntiScam.Blog.Api` nasłuchuje na wszystkich interfejsach na porcie 5000. Dla komputera z adresem `192.168.1.22` użyj wtedy `http://192.168.1.22:5000` z urządzenia w tej samej sieci. Jeśli połączenie z innego urządzenia zostanie zablokowane, zezwól aplikacji .NET na ruch przychodzący w Zaporze Windows dla sieci prywatnych.

## Aktualizacja angielskiej wersji README

Aby odświeżyć `README.en.md` na podstawie polskiego `README.md`, uruchom:

```powershell
.\tools\sync-readme-en.ps1
```

Skrypt używa `deep-translator` do tłumaczenia treści dokumentacji i zapisuje wynik do `README.en.md`.

## GitHub

Repozytorium jest skonfigurowane z origin:

```text
https://github.com/Kondexor2000/antiscam.git
```

Zalecany przepływ po zmianach:

```powershell
git status
git add .
git commit -m "Add C# blog WebAPI with SQLite"
git push origin main
```

## Troubleshooting / FAQ

### Port jest już w użyciu

**Problem:** "Address already in use" przy uruchamianiu aplikacji.

**Rozwiązanie - C# API:**
```powershell
# Zmień port w appsettings.json lub poprzez zmienną:
$env:ASPNETCORE_URLS="http://localhost:5002"
```

**Rozwiązanie - Python API:**
```powershell
uvicorn antiscam.api:app --reload --port 8001
```

### Baza SQLite jest zablokowana

**Problem:** "database is locked" podczas testów lub jednoczesnych operacji.

**Rozwiązanie:**
- Upewnij się, że tylko jedna instancja C# API jest uruchomiona
- Zamknij inne procesy korzystające z bazy (np. `sqlite3.exe`)
- Usuń plik `.sqlite-journal` jeśli istnieje

### Python dependencies nie instalują się

**Problem:** Błędy podczas `pip install -r requirements-dev.txt`.

**Rozwiązanie:**
```powershell
# Uaktualnij pip i setuptools
python -m pip install --upgrade pip setuptools
# Czyszczenie cache
pip cache purge
# Spróbuj ponownie
pip install -r requirements-dev.txt
```

### Testy nie przechodzą

**Problem:** Błędy w `pytest` lub `dotnet test`.

**Rozwiązanie:**
```powershell
# Zczyść cache i zbuduj na nowo
rm -Force -Recurse bin, obj  # lub Remove-Item
rm -Force .pytest_cache
dotnet clean
dotnet build
pytest --tb=short  # Szczegółowy output
```

### OpenSSL certificate issues

**Problem:** Błędy HTTPS na Windows lub certyfikat nie jest zaufany.

**Rozwiązanie:**
- Uruchom skrypt jako Administrator: `Set-ExecutionPolicy -ExecutionPolicy Unrestricted`
- Upewnij się, że OpenSSL jest zainstalowany: `openssl version`
- Zaufaj certyfikatowi CA: `certs/antiscam-ca.crt` (dodaj do Windows Certificate Store)

## Licencja

Projekt jest dostępny na licencji MIT.
