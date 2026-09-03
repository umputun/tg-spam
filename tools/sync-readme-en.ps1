$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
$source = Join-Path $root 'README.md'
$target = Join-Path $root 'README.en.md'

$pythonCommand = Get-Command python -ErrorAction SilentlyContinue
if (-not $pythonCommand) {
    $pythonCommand = Get-Command py -ErrorAction SilentlyContinue
}

if (-not $pythonCommand) {
    throw 'Python was not found on PATH. Install Python 3.10+ and try again.'
}

& $pythonCommand.Source -m antiscam.readme_sync --source $source --target $target --source-lang pl --target-lang en
if ($LASTEXITCODE -ne 0) {
    throw "README sync failed with exit code $LASTEXITCODE"
}

Write-Host "README.en.md was refreshed successfully."
