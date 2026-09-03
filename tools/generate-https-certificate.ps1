[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidatePattern('^(?:10\.|192\.168\.|172\.(?:1[6-9]|2[0-9]|3[0-1])\.)')]
    [string]$PrivateIp,
    [string]$CommonName = "antiscam.local",
    [string]$OutputDirectory = (Join-Path $PSScriptRoot "..\certs"),
    [int]$ValidDays = 365
)

$openSsl = Get-Command openssl -ErrorAction Stop
if ([string]::IsNullOrWhiteSpace($env:ANTISCAM_HTTPS_CERT_PASSWORD)) {
    throw "Set ANTISCAM_HTTPS_CERT_PASSWORD before generating the PFX certificate."
}

$certificateDirectory = [IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force -Path $certificateDirectory | Out-Null
$configPath = Join-Path $certificateDirectory "openssl.cnf"
$caKey = Join-Path $certificateDirectory "antiscam-ca.key"
$caCert = Join-Path $certificateDirectory "antiscam-ca.crt"
$serverKey = Join-Path $certificateDirectory "antiscam-server.key"
$request = Join-Path $certificateDirectory "antiscam-server.csr"
$serverCert = Join-Path $certificateDirectory "antiscam-server.crt"
$pfx = Join-Path $certificateDirectory "antiscam-server.pfx"

@"
[req]
prompt = no
default_md = sha256
distinguished_name = dn
req_extensions = req_ext

[dn]
C = PL
O = AntiScam
CN = $CommonName

[req_ext]
subjectAltName = @alt_names

[cert_ext]
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
DNS.2 = $CommonName
IP.1 = 127.0.0.1
IP.2 = $PrivateIp
"@ | Set-Content -LiteralPath $configPath -Encoding utf8NoBOM

& $openSsl.Source genrsa -out $caKey 4096
& $openSsl.Source req -x509 -new -nodes -key $caKey -sha256 -days $ValidDays -out $caCert -subj "/C=PL/O=AntiScam/CN=AntiScam Local CA"
& $openSsl.Source req -new -nodes -newkey rsa:2048 -keyout $serverKey -out $request -config $configPath
& $openSsl.Source x509 -req -in $request -CA $caCert -CAkey $caKey -CAcreateserial -out $serverCert -days $ValidDays -sha256 -extfile $configPath -extensions cert_ext
& $openSsl.Source pkcs12 -export -out $pfx -inkey $serverKey -in $serverCert -certfile $caCert -passout env:ANTISCAM_HTTPS_CERT_PASSWORD

Write-Host "Server certificate: $pfx"
Write-Host "Trust this CA on LAN clients to avoid browser warnings: $caCert"
