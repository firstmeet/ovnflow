[CmdletBinding()]
param(
    [string]$Distro = "",
    [switch]$NoReset,
    [switch]$NoTest,
    [switch]$NoCli
)

$ErrorActionPreference = "Stop"

function ConvertTo-WslPath {
    param([Parameter(Mandatory = $true)][string]$Path)

    $resolved = (Resolve-Path $Path).Path
    if ($resolved -match '^([A-Za-z]):\\(.*)$') {
        $drive = $Matches[1].ToLowerInvariant()
        $rest = $Matches[2] -replace '\\', '/'
        return "/mnt/$drive/$rest"
    }
    throw "Cannot convert path to WSL mount form: $resolved"
}

function Quote-Sh {
    param([Parameter(Mandatory = $true)][string]$Value)
    return "'" + ($Value -replace "'", "'\''") + "'"
}

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$wslRepoRoot = ConvertTo-WslPath $repoRoot
$defaultUser = (& wsl.exe sh -lc "id -un").Trim()
$scriptArgs = @()
if ($NoReset) { $scriptArgs += "--no-reset" }
if ($NoTest) { $scriptArgs += "--no-test" }
if ($NoCli) { $scriptArgs += "--no-cli" }

$wslArgs = @()
if ($Distro.Trim() -ne "") {
    $wslArgs += @("-d", $Distro)
}
$wslArgs += @("-u", "root")

$quotedRepo = Quote-Sh $wslRepoRoot
$quotedScriptArgs = ($scriptArgs | ForEach-Object { Quote-Sh $_ }) -join " "
$quotedDefaultUser = Quote-Sh $defaultUser
$command = "cd $quotedRepo && SDKCHECK_RUN_AS_USER=$quotedDefaultUser bash scripts/sdkcheck-wsl-native.sh $quotedScriptArgs"

Write-Host "Running external ovnflow SDK checker in WSL from $wslRepoRoot"
Write-Host "Warning: unless -NoReset is used, native WSL OVN/OVS databases will be reset."
& wsl.exe @wslArgs sh -lc $command
exit $LASTEXITCODE
