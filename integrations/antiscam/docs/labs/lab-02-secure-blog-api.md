# Laboratorium 2: bezpieczne WebAPI bloga

## Cel

Student potrafi wykazac, ze API blokuje publikacje tresci ryzykownych i nie zapisuje ich do SQLite.

## Zadania

1. Uruchom C# WebAPI.
2. Dodaj wpis `LOW RISK` i potwierdz status `201`.
3. Dodaj wpis z trescia BLIK i potwierdz status `422`.
4. Sprawdz przez `GET /api/posts/{slug}`, ze wpis ryzykowny nie istnieje.

## Kryteria zaliczenia

- 30% uruchomienie API,
- 30% poprawna publikacja wpisu bezpiecznego,
- 30% poprawna blokada wpisu ryzykownego,
- 10% opis mechanizmu obronnego.
