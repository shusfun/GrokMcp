param([switch]$Run)
$ErrorActionPreference = 'Stop'
$repoPath = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$candidateDir = Join-Path $repoPath 'dist/protocol-chain-candidate'
New-Item -ItemType Directory -Force $candidateDir | Out-Null
$savedAcceptanceEnv = @{}
foreach ($name in @('GROK_LIVE','GROK_ACCEPTANCE_APPROVED','GROK_ACCEPTANCE_REPO','GROK_ACCEPTANCE_ROOT')) { $savedAcceptanceEnv[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
Push-Location $repoPath
try {
  go test -c -ldflags '-H windowsgui' ./internal/mcp -o (Join-Path $candidateDir 'mcp-acceptance.test.exe')
  if ($LASTEXITCODE -ne 0) { throw 'Acceptance harness build failed' }
  if (-not $Run) { Write-Output 'Harness prepared. Real Grok was not started.'; return }
  # -Run 仅在用户已明确确认本次隔离真实验收后使用，不修改设备配置。
  $env:GROK_LIVE = '1'
  $env:GROK_ACCEPTANCE_APPROVED = '1'
  $env:GROK_ACCEPTANCE_REPO = $repoPath
  $env:GROK_ACCEPTANCE_ROOT = Join-Path $repoPath 'dist/acp-acceptance'
  $attemptLabel = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfff')
  $runner = Start-Process -FilePath (Join-Path $candidateDir 'mcp-acceptance.test.exe') -ArgumentList @('-test.run','^TestLiveMCPCompleteChain$','-test.v','-test.timeout','45m') -WorkingDirectory $repoPath -WindowStyle Hidden -PassThru -Wait -RedirectStandardOutput (Join-Path $candidateDir ("acceptance-$attemptLabel.stdout.log")) -RedirectStandardError (Join-Path $candidateDir ("acceptance-$attemptLabel.stderr.log"))
  if ($runner.ExitCode -ne 0) { throw 'Real acceptance failed; preserve the isolated session and report' }
} finally {
  foreach ($name in $savedAcceptanceEnv.Keys) { [Environment]::SetEnvironmentVariable($name, $savedAcceptanceEnv[$name], 'Process') }
  Pop-Location
}
