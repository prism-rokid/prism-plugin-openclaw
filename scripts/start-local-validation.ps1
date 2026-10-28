param(
    [string]$ValidationRoot = (Join-Path $env:LOCALAPPDATA 'PrismHubWindowsValidation/openclaw'),
    [int]$Port = 19873
)

# Dot-source this script, then launch Hub in the same PowerShell session so it
# inherits the generated Gateway token. Credentials stay in process memory.
$ErrorActionPreference = 'Stop'
if (-not $env:MINIMAX_API_KEY) {
    throw 'Set MINIMAX_API_KEY in this process environment before starting the validation Gateway.'
}
$nodeExecutable = Join-Path $ValidationRoot 'node_modules/node/bin/node.exe'
$gatewayEntry = Join-Path $ValidationRoot 'node_modules/openclaw/openclaw.mjs'
$gatewayConfig = Join-Path $ValidationRoot 'state/openclaw.json'
foreach ($requiredPath in @($nodeExecutable, $gatewayEntry, $gatewayConfig)) {
    if (-not (Test-Path -LiteralPath $requiredPath -PathType Leaf)) {
        throw "Missing isolated validation artifact: $requiredPath"
    }
}
if (-not $env:OPENCLAW_GATEWAY_TOKEN) {
    $tokenBytes = New-Object byte[] 32
    $tokenRng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $tokenRng.GetBytes($tokenBytes) } finally { $tokenRng.Dispose() }
    $env:OPENCLAW_GATEWAY_TOKEN = [Convert]::ToBase64String($tokenBytes)
}
$env:OPENCLAW_STATE_DIR = Split-Path -Parent $gatewayConfig
$env:OPENCLAW_CONFIG_PATH = $gatewayConfig
$env:OPENCLAW_GATEWAY_URL = "ws://127.0.0.1:$Port"
$env:NODE_DISABLE_COMPILE_CACHE = '1'
$env:OPENCLAW_NO_RESPAWN = '1'
$env:OPENCLAW_DISABLE_BONJOUR = '1'
$validationGateway = Start-Process -FilePath $nodeExecutable `
    -ArgumentList @("`"$gatewayEntry`"", 'gateway', 'run', '--port', "$Port", '--bind', 'loopback') `
    -WindowStyle Hidden -PassThru
[PSCustomObject]@{ ProcessId = $validationGateway.Id; Endpoint = $env:OPENCLAW_GATEWAY_URL; Config = $gatewayConfig }
