# Kryptografia w projekcie

Ten dokument wyjasnia, jak projekt pokrywa wymagania kryptologii i zarzadzania kluczami z sylabusow.

## Szyfrowanie uwierzytelnione

`AesGcmAuthenticatedEncryptor` uzywa AES-GCM-256:

- klucz 32 bajty,
- losowy nonce 12 bajtow,
- tag uwierzytelniajacy 16 bajtow,
- opcjonalne `additionalData`, ktore wiaze szyfrogram z kontekstem, np. identyfikatorem wpisu.

AES-GCM zapewnia poufnosc i integralnosc. Zmiana tagu, szyfrogramu, nonce lub dodatkowych danych powoduje blad odszyfrowania.

## Zarzadzanie kluczami

Kluczy nie wolno commitowac do repozytorium. Zalecany model:

1. Wygeneruj klucz poza repozytorium.
2. Przechowuj go w zmiennej srodowiskowej, menedzerze sekretow lub chronionym magazynie systemowym.
3. Rotuj klucze zgodnie z polityka organizacji.
4. Loguj tylko identyfikatory operacji, nigdy klucze ani plaintext sekretow.

Przykladowe wygenerowanie klucza demonstracyjnego:

```powershell
[Convert]::ToBase64String([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
```

## Granice demonstracji

Kod kryptograficzny w repozytorium jest elementem edukacyjnym i testowanym komponentem bibliotecznym. Produkcyjne wdrozenie powinno uzyc centralnego zarzadzania sekretami, rotacji kluczy, monitoringu oraz przegladu kryptograficznego.
