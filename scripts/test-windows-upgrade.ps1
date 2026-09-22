$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# 只在隔离的 Actions runner 上执行，绝不修改开发机的安装注册项。
if ($env:GITHUB_ACTIONS -ne 'true') { throw 'This installation test only runs in GitHub Actions.' }
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$fixtureId = 'GrokMcpUpgradeTest-' + [guid]::NewGuid().ToString('N')
$fixtureRoot = Join-Path $repoRoot ('dist\' + $fixtureId)
$installDir = Join-Path $fixtureRoot 'installed'
$oldSource = Join-Path $fixtureRoot 'old'
$oldOutput = Join-Path $fixtureRoot 'old-installer'
$newOutput = Join-Path $fixtureRoot 'new-installer'
@($fixtureRoot, $oldSource, $oldOutput, $newOutput) | ForEach-Object { New-Item -ItemType Directory -Path $_ -Force | Out-Null }

$compiler = Get-Command iscc -ErrorAction SilentlyContinue
if ($compiler) { $isccPath = $compiler.Source } else {
  $isccPath = Join-Path ${env:ProgramFiles(x86)} 'Inno Setup 6\ISCC.exe'
}
if (-not (Test-Path -LiteralPath $isccPath)) { throw 'Inno Setup compiler is missing.' }

$oldZip = Join-Path $fixtureRoot 'GrokMcp-0.1.22-windows-amd64.zip'
& gh release download 'v0.1.22' --repo $env:GITHUB_REPOSITORY --pattern 'GrokMcp-0.1.22-windows-amd64.zip' --dir $fixtureRoot
if ($LASTEXITCODE -ne 0) { throw 'Could not download the old release payload.' }
Expand-Archive -LiteralPath $oldZip -DestinationPath $oldSource
$newSource = Join-Path $repoRoot 'dist\windows-amd64'
$newVersion = $env:VERSION.TrimStart('v')
if ($newVersion -notmatch '^\d+\.\d+\.\d+$') { throw 'Invalid candidate version.' }

function Build-FixtureInstaller([string]$Version, [string]$Source, [string]$Output) {
  & $isccPath '/Qp' "/DMyAppVersion=$Version" "/DMyAppId=$fixtureId" "/DAppName=$fixtureId" "/DSourceDir=$Source" "/DOutputDir=$Output" (Join-Path $repoRoot 'build\windows\GrokMcp.iss') | Out-Host
  if ($LASTEXITCODE -ne 0) { throw "Fixture installer compilation failed: $Version" }
  return Join-Path $Output "GrokMcp-$Version-windows-amd64-installer.exe"
}

function Invoke-FixtureInstaller([string]$Path, [bool]$FirstInstall) {
  $arguments = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART')
  if ($FirstInstall) { $arguments += ('/DIR="' + $installDir + '"') }
  $installer = Start-Process -FilePath $Path -ArgumentList $arguments -WindowStyle Hidden -Wait -PassThru
  if ($installer.ExitCode -ne 0) { throw "Fixture installer failed: $($installer.ExitCode)" }
}

$oldInstaller = Build-FixtureInstaller '0.1.22' $oldSource $oldOutput
$newInstaller = Build-FixtureInstaller $newVersion $newSource $newOutput
try {
  Invoke-FixtureInstaller $oldInstaller $true
  $installedExe = Join-Path $installDir 'GrokMcp.exe'
  $oldHash = (Get-FileHash -LiteralPath (Join-Path $oldSource 'GrokMcp.exe')).Hash
  if ((Get-FileHash -LiteralPath $installedExe).Hash -ne $oldHash) { throw 'Old payload was not installed.' }
  $sentinel = Join-Path $installDir 'settings.keep'
  [IO.File]::WriteAllText($sentinel, 'preserve-user-data')

  # 不传 /DIR，确认根据同一 AppId 复用旧目录，且没有卸载后直接退出。
  Invoke-FixtureInstaller $newInstaller $false
  $newHash = (Get-FileHash -LiteralPath (Join-Path $newSource 'GrokMcp.exe')).Hash
  if ((Get-FileHash -LiteralPath $installedExe).Hash -ne $newHash) { throw 'Upgrade did not replace the executable.' }
  if ([IO.File]::ReadAllText($sentinel) -ne 'preserve-user-data') { throw 'Upgrade removed user data.' }
  Invoke-FixtureInstaller $newInstaller $false
  if ((Get-FileHash -LiteralPath $installedExe).Hash -ne $newHash) { throw 'Reinstall removed the application.' }
  Write-Output 'PASS: v0.1.22 payload upgraded in place; user data retained; reinstall remains installed. No app was launched.'
} finally {
  $uninstaller = Join-Path $installDir 'unins000.exe'
  if (Test-Path -LiteralPath $uninstaller) {
    $cleanup = Start-Process -FilePath $uninstaller -ArgumentList '/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART' -WindowStyle Hidden -Wait -PassThru
    if ($cleanup.ExitCode -ne 0) { throw 'Fixture uninstallation failed.' }
  }
}
