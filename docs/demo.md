# Demonstracja projektu

## Przygotowanie

Uruchamiaj komendy z katalogu repozytorium:

```powershell
cd C:\Users\kondz\antiscam
venv\Scripts\activate
```

Najpierw sprawdz testy, gdy aplikacja C# nie jest jeszcze uruchomiona:

```powershell
dotnet restore AntiScamBlog.sln
python -m pytest
dotnet test AntiScamBlog.sln
```

## Automatyczne wykonanie demonstracji

Skrypt `Invoke-Demo.ps1` uruchamia na tymczasowej bazie SQLite scenariusze C# z
Demo 1-8. Sprawdza odpowiedzi HTTP i przerywa dzialanie przy pierwszej
niezgodnosci.

```powershell
.\Invoke-Demo.ps1
```

Wariant uruchomienia na innym porcie:

```powershell
.\Invoke-Demo.ps1 -CSharpPort 5000
```

## Testy C#

Testy .NET sa zapisane w projekcie `tests/AntiScam.Blog.Api.Tests` i korzystaja
z xUnit. Nie wymagaja uruchomionego serwera: testy integracyjne startuja API w
pamieci, tworza osobna tymczasowa baze SQLite oraz zastepuja integracje AI i
MongoDB deterministycznymi implementacjami testowymi.

Uruchom wszystkie 25 przypadkow testowych:

```powershell
dotnet test AntiScamBlog.sln
```

Mozna tez uruchomic tylko wybrana grupe:

```powershell
dotnet test tests\AntiScam.Blog.Api.Tests\AntiScam.Blog.Api.Tests.csproj --filter "FullyQualifiedName~Unit"
dotnet test tests\AntiScam.Blog.Api.Tests\AntiScam.Blog.Api.Tests.csproj --filter "FullyQualifiedName~Integration"
```

### Testy jednostkowe (13 przypadkow)

| Obszar | Testowane zachowanie |
| --- | --- |
| `AesGcmAuthenticatedEncryptor` | Szyfrowanie i odszyfrowanie zwraca pierwotna tresc oraz oznacza algorytm AES-GCM. |
| `AesGcmAuthenticatedEncryptor` | Zmiana danych dodatkowych (AAD) powoduje `CryptographicException`. |
| `BlogPostValidator` | Brak tytulu, opisu, tresci i autora zwraca bledy walidacji dla wszystkich pol. |
| `BlogPostValidator` | Kompletny wpis nie zwraca bledow walidacji. |
| `RiskAnalyzer` | Zwykly wpis edukacyjny otrzymuje status `LOW RISK` i moze zostac opublikowany. |
| `RiskAnalyzer` | Wiadomosc z kodem BLIK jest oznaczona jako `HIGH RISK`, zablokowana i zawiera powod `BLIK CONFIRMED`. |
| `RiskAnalyzer` | Obfuskowany zapis `B L I K` oraz `k-o-d` jest normalizowany i blokowany. |
| `RiskAnalyzer` | Podszywanie sie pod zaufana domene i literowka `g00gle.com` sa wykrywane jako ryzykowne linki. |
| `SlugGenerator` | Polski tytul jest zamieniany na przyjazny adres URL. |
| `SlugGenerator` | Spacje, interpunkcja i znaki specjalne sa usuwane z adresu URL. |
| `SlugGenerator` | Pusty tytul zwraca domyslny slug `post`. |

### Testy integracyjne API (12 przypadkow)

| Endpoint lub obszar | Testowane zachowanie |
| --- | --- |
| `GET /api/posts` | Zwraca co najmniej dwa wpisy startowe. |
| `GET /api/posts/latest` | Zwraca najnowszy wpis z listy wpisow. |
| `GET /api/storage` | Raportuje SQLite jako magazyn glowny oraz aktywny MongoDB jako magazyn incydentow. |
| `GET /api/incidents` | Z testowym magazynem incydentow zwraca pusta liste. |
| `POST /api/posts` | Poprawny wpis zwraca `201 Created`, otrzymuje slug i jest dostepny przez `GET`. |
| `POST /api/posts` | Wpis bez tytulu zwraca `400 Bad Request`. |
| `POST /api/posts` | Wpis z oszustwem BLIK zwraca `422`, zawiera ocene i wyjasnienie AI, a wpis nie jest zapisywany. |
| `POST /api/posts` | Obfuskowany BLIK i link `g00gle.com` zwracaja `422`, a wpis nie jest zapisywany. |
| `POST /api/posts/{id}/comments` | Bezpieczny komentarz zwraca `201 Created` i jest dostepny przez `GET`. |
| `POST /api/posts/{id}/comments` | Komentarz scamowy zwraca `422` i nie pojawia sie na liscie komentarzy. |
| `GET /` | Strona startowa zwraca oczekiwany statyczny HTML oraz link do wszystkich wpisow. |

Nastepnie uruchom blog API:

```powershell
dotnet run --project src\AntiScam.Blog.Api\AntiScam.Blog.Api.csproj --urls http://0.0.0.0:5000
```

Serwer C# nasluchuje wtedy na wszystkich interfejsach sieciowych pod adresem
`0.0.0.0:5000`. Na komputerze serwera strona demo jest dostepna pod adresem
`http://localhost:5000/`; z drugiego urzadzenia w tej samej sieci nalezy uzyc
`http://ADRES_IP_SERWERA:5000/`. Nie nalezy wpisywac `0.0.0.0` jako adresu w
przegladarce lub w `Invoke-WebRequest` — jest to adres nasluchu, nie cel polaczenia.
Jesli `Invoke-WebRequest` zwraca w `catch` wartosc `0`, oznacza to brak polaczenia z serwerem, a nie odpowiedz HTTP. Sprawdz wtedy, czy aplikacja C# nadal dziala i czy uzywasz tego samego portu.

## Demo 1: wpis bezpieczny

```powershell
Invoke-WebRequest -Uri http://localhost:5000/api/posts `
  -Method POST `
  -ContentType "application/json" `
  -UseBasicParsing `
  -Body '{"title":"Bezpieczne spotkanie","summary":"Normalny wpis edukacyjny.","content":"Czesc, opisujemy spokojne zasady ochrony przed phishingiem.","author":"AntiScam Team"}'
```

Oczekiwany wynik: `201 Created`.

## Demo 2: wpis ryzykowny

```powershell
try {
  Invoke-WebRequest -Uri http://localhost:5000/api/posts `
    -Method POST `
    -ContentType "application/json" `
    -UseBasicParsing `
    -Body '{"title":"Pilny BLIK","summary":"Konto zablokowane.","content":"Wyslij kod BLIK 123456 natychmiast i kliknij teraz.","author":"Scammer"}'
} catch {
  if ($_.Exception.Response) {
    [int]$_.Exception.Response.StatusCode
    $reader = [System.IO.StreamReader]::new($_.Exception.Response.GetResponseStream())
    $reader.ReadToEnd()
  } else {
    "Brak polaczenia z serwerem"
  }
}
```

Oczekiwany wynik: `422`. Wpis nie zostaje zapisany, a odpowiedz zawiera `aiExplanation` wygenerowane przez `antiscam/ai.py`.

## Demo 3: sprawdzenie, ze wpis nie istnieje

```powershell
try {
  Invoke-WebRequest -Uri http://localhost:5000/api/posts/pilny-blik -UseBasicParsing
} catch {
  if ($_.Exception.Response) {
    [int]$_.Exception.Response.StatusCode
  } else {
    "Brak polaczenia z serwerem"
  }
}
```

Oczekiwany wynik: `404`.

## Demo 4: testowanie komentarzy

Na stronie glownej jest widoczny tylko najnowszy wpis. Kliknij **Zobacz wszystkie posty**,
aby otworzyc `http://localhost:5000/?view=all`; pod kazdym wpisem znajduje sie formularz komentarza.

Mozna tez przetestowac API bezposrednio. Najpierw pobierz identyfikatory wpisow:

```powershell
Invoke-RestMethod -Uri http://localhost:5000/api/posts
```

W ponizszych poleceniach zamien `1` na istniejace `id` wpisu.

Bezpieczny komentarz powinien zostac zapisany:

```powershell
Invoke-WebRequest -Uri http://localhost:5000/api/posts/1/comments `
  -Method POST `
  -ContentType "application/json" `
  -UseBasicParsing `
  -Body '{"content":"Dziekuje za przydatne wskazowki.","author":"Czytelnik"}'
```

Oczekiwany wynik: `201 Created`. Komentarz jest widoczny po odswiezeniu strony
oraz w odpowiedzi `GET /api/posts/1/comments`.

Komentarz scamowy powinien zostac zablokowany przez ten sam algorytm co wpisy:

```powershell
try {
  Invoke-WebRequest -Uri http://localhost:5000/api/posts/1/comments `
    -Method POST `
    -ContentType "application/json" `
    -UseBasicParsing `
    -Body '{"content":"Wyslij kod BLIK 123456 natychmiast.","author":"Oszust"}'
} catch {
  if ($_.Exception.Response) {
    [int]$_.Exception.Response.StatusCode
    $reader = [System.IO.StreamReader]::new($_.Exception.Response.GetResponseStream())
    $reader.ReadToEnd()
  } else {
    "Brak polaczenia z serwerem"
  }
}
```

Oczekiwany wynik: `422`. Komentarz nie jest zapisywany, a odpowiedz zawiera ocene
`risk` wraz z powodami blokady, np. `BLIK CONFIRMED`.

## Demo 5: stan API i magazynow C#

Ponizsze zapytania pokazuja stan aplikacji oraz konfiguracje magazynow. SQLite
przechowuje wpisy, uzytkownikow i sesje; MongoDB, jesli jest dostepny, przechowuje
incydenty zablokowanych wpisow.

```powershell
Invoke-RestMethod -Uri http://localhost:5000/api/health
Invoke-RestMethod -Uri http://localhost:5000/api/storage
Invoke-RestMethod -Uri "http://localhost:5000/api/incidents?limit=10"
Invoke-RestMethod -Uri http://localhost:5000/api/workspace
```

Oczekiwany wynik: zdrowie API ma status `ok`, a `/api/storage` wskazuje SQLite
jako magazyn podstawowy. Lista incydentow moze byc pusta, gdy MongoDB nie jest
uruchomione albo nie zablokowano jeszcze zadnego wpisu.

## Demo 10: Python API

```powershell
uvicorn antiscam.api:app --reload
```

```powershell
Invoke-WebRequest -Uri http://localhost:8000/scan `
  -Method POST `
  -ContentType "application/json" `
  -UseBasicParsing `
  -Body '{"text":"Wyslij BLIK 123456 natychmiast"}'
```

Oczekiwany wynik: wysoki wynik ryzyka. W polu `reasons` widoczny jest tez bazowy wynik
`ML intent score`, wyliczony przez hybrydowy pipeline TF-IDF + Naive Bayes.

## Demo 11: Hybryda ML + twarde reguly

```powershell
Invoke-WebRequest -Uri http://localhost:8000/scan `
  -Method POST `
  -ContentType "application/json" `
  -UseBasicParsing `
  -Body '{"text":"Konto zablokowane, kliknij https://g00gle.com/login i potwierdz kod BLIK 123456 natychmiast"}'
```

Oczekiwany wynik: `HIGH RISK`. Model ML nadaje bazowy wynik intencji, a reguly BLIK,
podejrzanego linku i typosquattingu dzialaja jako twarde modyfikatory wyniku.

## Demo 12: Normalizacja obfuskacji

```powershell
Invoke-WebRequest -Uri http://localhost:8000/scan `
  -Method POST `
  -ContentType "application/json" `
  -UseBasicParsing `
  -Body '{"text":"B L I K 123456 k-o-d natychmiast"}'
```

Oczekiwany wynik: `HIGH RISK`. `normalization.py` laczy rozstrzelone litery,
usuwa proste znaki wstawione w slowa i przekazuje oczyszczony tekst do `engine.py`.

## Demo 13: Bezpieczne wycinanie domen i literowki

```powershell
Invoke-WebRequest -Uri http://localhost:8000/scan `
  -Method POST `
  -ContentType "application/json" `
  -UseBasicParsing `
  -Body '{"text":"Nie loguj sie przez https://google.com.evil.example ani https://g00gle.com/login"}'
```

Oczekiwany wynik: `HIGH RISK`. `links.py` uzywa `tldextract`, wiec
`google.com.evil.example` nie jest traktowane jak zaufane `google.com`, a
`g00gle.com` trafia do `Typosquatting links` dzieki odleglosci Levenshteina.

## Demo 14: Python AI explain

```powershell
Invoke-WebRequest -Uri http://localhost:8000/ai/explain `
  -Method POST `
  -ContentType "application/json" `
  -UseBasicParsing `
  -Body '{"text":"Wyslij BLIK 123456 natychmiast"}'
```

Oczekiwany wynik: raport AI/NLP z `blocked_after_scan`, `block_explanation`, `scan_reasons` i zaleceniem bezpiecznej reakcji.
