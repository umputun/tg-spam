[CmdletBinding()]
param(
    [int]$CSharpPort = 5000,
    [switch]$KeepServers
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSCommandPath
# Niektore hosty PowerShell przekazuja jednoczesnie zmienne PATH i Path. .NET
# odrzuca taki zestaw przy Start-Process, wiec normalizujemy go przed startem API.
$processPath = [Environment]::GetEnvironmentVariable('Path', 'Process')
if (![string]::IsNullOrWhiteSpace($processPath)) {
    [Environment]::SetEnvironmentVariable('PATH', $null, 'Process')
    [Environment]::SetEnvironmentVariable('Path', $processPath, 'Process')
    $env:Path = $processPath
}
$csharpProcess = $null
$temporaryDatabase = Join-Path $env:TEMP ("antiscam-demo-{0}.sqlite" -f [guid]::NewGuid().ToString('N'))
$logDirectory = Join-Path $env:TEMP ("antiscam-demo-logs-{0}" -f [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $logDirectory | Out-Null

function Assert-Equal {
    param([object]$Actual, [object]$Expected, [string]$Message)
    if ($Actual -ne $Expected) { throw "$Message Otrzymano: $Actual; oczekiwano: $Expected." }
}

function Get-HttpFailureStatus {
    param([scriptblock]$Request)
    try {
        & $Request | Out-Null
        return 200
    } catch {
        if ($null -eq $_.Exception.Response) { throw }
        return [int]$_.Exception.Response.StatusCode
    }
}

function Wait-ForHttpService {
    param([string]$Uri, [System.Diagnostics.Process]$Process, [string]$Name)
    $deadline = (Get-Date).AddSeconds(30)
    do {
        Start-Sleep -Milliseconds 500
        try {
            Invoke-RestMethod -Uri $Uri -ErrorAction Stop | Out-Null
            return
        } catch {
            if ($Process.HasExited) { throw "$Name zakonczyl dzialanie przed uruchomieniem." }
        }
    } while ((Get-Date) -lt $deadline)
    throw "$Name nie odpowiada pod adresem $Uri w ciagu 30 sekund."
}

function Start-CSharpDemo {
    $existing = Get-NetTCPConnection -LocalPort $CSharpPort -State Listen -ErrorAction SilentlyContinue
    if ($null -ne $existing) { throw "Port $CSharpPort jest juz zajety. Zatrzymaj istniejacy serwer albo uzyj -CSharpPort." }

    $previousDatabase = $env:ANTISCAM_BLOG_DB
    $env:ANTISCAM_BLOG_DB = $temporaryDatabase
    try {
        $script:csharpProcess = Start-Process dotnet -ArgumentList @(
            'run', '--no-restore', '--project', 'src\AntiScam.Blog.Api\AntiScam.Blog.Api.csproj', '--',
            "--Network:HttpPort=$CSharpPort", '--Network:BindToLan=true'
        ) -WorkingDirectory $repoRoot -PassThru -WindowStyle Hidden `
          -RedirectStandardOutput (Join-Path $logDirectory 'csharp.out.log') `
          -RedirectStandardError (Join-Path $logDirectory 'csharp.err.log')
        Wait-ForHttpService -Uri "http://127.0.0.1:$CSharpPort/api/health" -Process $script:csharpProcess -Name 'API C#'
    } finally {
        $env:ANTISCAM_BLOG_DB = $previousDatabase
    }
}

function Invoke-CSharpDemos {
    $base = "http://127.0.0.1:$CSharpPort"
    $health = Invoke-RestMethod -Uri "$base/api/health"
    Assert-Equal $health.status 'ok' 'Nieprawidlowy stan API.'
    $storage = Invoke-RestMethod -Uri "$base/api/storage"
    Assert-Equal $storage.primary.provider 'SQLite' 'Nieprawidlowy magazyn podstawowy.'

    $posts = Invoke-RestMethod -Uri "$base/api/posts"
    if ($posts.Count -lt 2) { throw 'Brakuje wpisow startowych.' }
    $latest = Invoke-RestMethod -Uri "$base/api/posts/latest"
    Assert-Equal $latest.id $posts[0].id 'Endpoint latest nie zwraca najnowszego wpisu.'

    $safePost = Invoke-RestMethod -Uri "$base/api/posts" -Method POST -ContentType 'application/json' -Body '{"title":"Automatyczny bezpieczny wpis","summary":"Demonstracja C#.","content":"Weryfikuj nadawce i nie podawaj kodow autoryzacyjnych.","author":"AntiScam Team"}'
    Assert-Equal (Invoke-RestMethod -Uri "$base/api/posts/$($safePost.slug)").id $safePost.id 'Nie mozna pobrac utworzonego wpisu.'

    $riskyPostStatus = Get-HttpFailureStatus { Invoke-WebRequest -Uri "$base/api/posts" -Method POST -ContentType 'application/json' -UseBasicParsing -Body '{"title":"Pilny BLIK","summary":"Konto zablokowane.","content":"Wyslij kod BLIK 123456 natychmiast i kliknij teraz.","author":"Scammer"}' }
    Assert-Equal $riskyPostStatus 422 'Ryzykowny wpis nie zostal zablokowany.'

    $comment = Invoke-RestMethod -Uri "$base/api/posts/$($posts[0].id)/comments" -Method POST -ContentType 'application/json' -Body '{"content":"Dziekuje za przydatne wskazowki.","author":"Czytelnik"}'
    $riskyCommentStatus = Get-HttpFailureStatus { Invoke-WebRequest -Uri "$base/api/posts/$($posts[0].id)/comments" -Method POST -ContentType 'application/json' -UseBasicParsing -Body '{"content":"Wyslij kod BLIK 123456 natychmiast.","author":"Oszust"}' }
    Assert-Equal $riskyCommentStatus 422 'Ryzykowny komentarz nie zostal zablokowany.'

    $suffix = [guid]::NewGuid().ToString('N').Substring(0, 8)
    $adminName = "admin-demo-$suffix"
    $readerName = "reader-demo-$suffix"
    $password = 'StrongPassword123!'
    $admin = Invoke-RestMethod -Uri "$base/api/auth/register" -Method POST -ContentType 'application/json' -Body (@{ userName = $adminName; password = $password } | ConvertTo-Json -Compress)
    Assert-Equal $admin.role 'Admin' 'Pierwsze konto nie otrzymalo roli administratora.'
    $reader = Invoke-RestMethod -Uri "$base/api/auth/register" -Method POST -ContentType 'application/json' -Body (@{ userName = $readerName; password = $password } | ConvertTo-Json -Compress)
    $login = Invoke-RestMethod -Uri "$base/api/auth/login" -Method POST -ContentType 'application/json' -Body (@{ userName = $adminName; password = $password } | ConvertTo-Json -Compress)
    $headers = @{ Authorization = "Bearer $($login.accessToken)" }

    $users = Invoke-RestMethod -Uri "$base/api/admin/users" -Headers $headers
    $readerFromApi = $users | Where-Object { $_.id -eq $reader.id } | Select-Object -First 1
    if ($null -eq $readerFromApi) { throw 'Nie znaleziono konta czytelnika przez API administratora.' }
    $block = Invoke-WebRequest -Uri "$base/api/admin/users/$($readerFromApi.id)/block" -Method POST -Headers $headers -UseBasicParsing
    Assert-Equal $block.StatusCode 204 'Blokowanie konta nie zwrocilo 204.'
    $blockedLoginStatus = Get-HttpFailureStatus { Invoke-WebRequest -Uri "$base/api/auth/login" -Method POST -ContentType 'application/json' -UseBasicParsing -Body (@{ userName = $readerName; password = $password } | ConvertTo-Json -Compress) }
    Assert-Equal $blockedLoginStatus 403 'Zablokowane konto zostalo dopuszczone do logowania.'

    $delete = Invoke-WebRequest -Uri "$base/api/posts/$($safePost.id)" -Method DELETE -Headers $headers -UseBasicParsing
    Assert-Equal $delete.StatusCode 204 'Usuniecie wpisu nie zwrocilo 204.'
    Assert-Equal (Get-HttpFailureStatus { Invoke-WebRequest -Uri "$base/api/posts/$($safePost.slug)" -UseBasicParsing }) 404 'Usuniety wpis pozostaje dostepny.'
    Assert-Equal (Invoke-WebRequest -Uri "$base/api/admin/posts/$($safePost.id)/restore" -Method POST -Headers $headers -UseBasicParsing).StatusCode 204 'Przywrocenie wpisu nie zwrocilo 204.'
    $updated = Invoke-RestMethod -Uri "$base/api/posts/$($safePost.id)" -Method PUT -ContentType 'application/json' -Body '{"title":"Zaktualizowane zasady bezpieczenstwa","summary":"Krotka aktualizacja.","content":"Nie podawaj nikomu kodow autoryzacyjnych i weryfikuj nadawce.","author":"AntiScam Team"}'
    Assert-Equal $updated.title 'Zaktualizowane zasady bezpieczenstwa' 'Aktualizacja wpisu nie powiodla sie.'
    Assert-Equal (Invoke-WebRequest -Uri "$base/api/auth/logout" -Method POST -Headers $headers -UseBasicParsing).StatusCode 204 'Wylogowanie nie zwrocilo 204.'
    Assert-Equal (Get-HttpFailureStatus { Invoke-WebRequest -Uri "$base/api/admin/users" -Headers $headers -UseBasicParsing }) 401 'Wylogowany token nadal ma dostep administratora.'

    Write-Host 'Demonstracje C# 1-8: OK' -ForegroundColor Green
}

try {
    Start-CSharpDemo
    Invoke-CSharpDemos
    Write-Host 'Automatyczne demonstracje zakonczone powodzeniem.' -ForegroundColor Green
} finally {
    if (!$KeepServers) {
        foreach ($process in @($csharpProcess)) {
            if ($null -ne $process -and !$process.HasExited) { Stop-Process -Id $process.Id -Force }
        }
        Remove-Item -LiteralPath $temporaryDatabase -Force -ErrorAction SilentlyContinue
    }
    if (!$KeepServers) { Remove-Item -LiteralPath $logDirectory -Recurse -Force -ErrorAction SilentlyContinue }
}
