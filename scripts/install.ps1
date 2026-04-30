#requires -Version 5.1
<#
.SYNOPSIS
    HotPlex Worker Gateway — Binary Installer (Windows)

.DESCRIPTION
    Downloads a GitHub release binary and installs it.
    For config, secrets, and service setup, run: hotplex onboard

.PARAMETER Prefix
    Installation directory (default: $env:ProgramFiles\HotPlex for admin,
    $HOME\.hotplex\bin for user)

.PARAMETER Release
    Download a specific release (e.g. v1.3.0)

.PARAMETER Latest
    Download the latest release

.PARAMETER Uninstall
    Remove the installed binary and PATH entry

.EXAMPLE
    .\install.ps1 -Latest
    .\install.ps1 -Release v1.3.0 -Prefix C:\Tools\HotPlex
    .\install.ps1 -Uninstall
#>

[CmdletBinding()]
param(
    [string]$Prefix = "",
    [string]$Release = "",
    [switch]$Latest,
    [switch]$Uninstall,
    [switch]$Help
)

$ErrorActionPreference = "Stop"
$Repo = "hrygo/hotplex"
$BinName = "hotplex.exe"

function Write-Info($msg)  { Write-Host "[INFO] " -ForegroundColor Green -NoNewline; Write-Host $msg }
function Write-Warn($msg)  { Write-Host "[WARN] " -ForegroundColor Yellow -NoNewline; Write-Host $msg }
function Write-Err($msg)   { Write-Host "[ERROR] " -ForegroundColor Red -NoNewline; Write-Host $msg }
function Write-Step($msg)  { Write-Host $msg -ForegroundColor Cyan }

# ── Help ─────────────────────────────────────────────────────────────────────

if ($Help) {
    Get-Help $MyInvocation.MyCommand.Path -Detailed
    exit 0
}

# ── Resolve architecture ─────────────────────────────────────────────────────

$ProcArch = $env:PROCESSOR_ARCHITECTURE
if ($ProcArch -eq "ARM64") {
    $Arch = "arm64"
} elseif ($ProcArch -eq "AMD64" -or $ProcArch -eq "x64") {
    $Arch = "amd64"
} else {
    Write-Err "Unsupported processor architecture: $ProcArch"
    exit 1
}

Write-Info "Platform: windows/$Arch"

# ── Uninstall mode ───────────────────────────────────────────────────────────

if ($Uninstall) {
    $InstallDirs = @(
        "$env:ProgramFiles\HotPlex",
        "$HOME\.hotplex\bin",
        "${env:ProgramFiles(x86)}\HotPlex"
    )

    $Found = $false
    foreach ($Dir in $InstallDirs) {
        $ExePath = Join-Path $Dir $BinName
        if (Test-Path $ExePath) {
            Write-Info "Removing: $ExePath"
            Remove-Item $ExePath -Force

            # Remove from User PATH
            $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
            if ($null -ne $UserPath) {
                $Parts = $UserPath -split ";" | Where-Object { $_ -ne $Dir -and $_ -ne "" }
                if ($Parts.Count -ne ($UserPath -split ";" | Where-Object { $_ -ne "" }).Count) {
                    [Environment]::SetEnvironmentVariable("Path", ($Parts -join ";"), "User")
                    Write-Info "Removed from user PATH: $Dir"
                }
            }

            # Remove from Machine PATH (admin installs)
            $MachinePath = [Environment]::GetEnvironmentVariable("Path", "Machine")
            if ($null -ne $MachinePath) {
                $Parts = $MachinePath -split ";" | Where-Object { $_ -ne $Dir -and $_ -ne "" }
                if ($Parts.Count -ne ($MachinePath -split ";" | Where-Object { $_ -ne "" }).Count) {
                    [Environment]::SetEnvironmentVariable("Path", ($Parts -join ";"), "Machine")
                    Write-Info "Removed from machine PATH: $Dir"
                }
            }

            # Clean empty dir
            $Remaining = Get-ChildItem $Dir -ErrorAction SilentlyContinue
            if ($null -eq $Remaining -or $Remaining.Count -eq 0) {
                Remove-Item $Dir -Force -Recurse -ErrorAction SilentlyContinue
                Write-Info "Removed empty directory: $Dir"
            }

            $Found = $true
            break
        }
    }

    if (-not $Found) {
        Write-Warn "HotPlex binary not found in standard locations."
    }

    Write-Info "Uninstall complete."
    exit 0
}

# ── Validate arguments ───────────────────────────────────────────────────────

if (-not $Latest -and [string]::IsNullOrEmpty($Release)) {
    Write-Err "No installation mode specified. Use -Latest or -Release <tag>."
    Write-Host ""
    Write-Host "  .\install.ps1 -Latest"
    Write-Host "  .\install.ps1 -Release v1.3.0"
    exit 1
}

# ── Resolve install prefix ──────────────────────────────────────────────────

if ([string]::IsNullOrEmpty($Prefix)) {
    $IsAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    $Prefix = if ($IsAdmin) { "$env:ProgramFiles\HotPlex" } else { "$HOME\.hotplex\bin" }
}

$TargetPath = Join-Path $Prefix $BinName

# ── Resolve release tag ──────────────────────────────────────────────────────

if ($Latest) {
    Write-Info "Querying latest release from GitHub..."
    try {
        $ReleaseUrl = "https://api.github.com/repos/$Repo/releases/latest"
        $Response = Invoke-RestMethod -Uri $ReleaseUrl -UseBasicParsing
        $Release = $Response.tag_name
        Write-Info "Latest release: $Release"
    } catch {
        Write-Err "Failed to detect latest release: $($_.Exception.Message)"
        Write-Host "Specify -Release <tag> manually."
        exit 1
    }
}

if ($Release -notmatch '^v\d+\.\d+\.\d+(-[a-zA-Z0-9.]+)?$') {
    Write-Err "Invalid release tag: $Release (expected vX.Y.Z)"
    exit 1
}

# ── Download ─────────────────────────────────────────────────────────────────

$BinaryName = "hotplex-windows-${Arch}.exe"
$BaseUrl = "https://github.com/$Repo/releases/download/$Release"
$DownloadUrl = "$BaseUrl/$BinaryName"
$ChecksumUrl = "$BaseUrl/checksums.txt"

$TmpDir = Join-Path $env:TEMP "hotplex-install-$(Get-Random)"
New-Item -ItemType Directory -Path $TmpDir -Force | Out-Null

$Success = $false

try {
    # Download binary
    Write-Info "Downloading hotplex $Release for windows/$Arch..."
    $BinaryPath = Join-Path $TmpDir $BinaryName

    $PrevProgress = $ProgressPreference
    $ProgressPreference = "SilentlyContinue"
    try {
        Invoke-WebRequest -Uri $DownloadUrl -OutFile $BinaryPath -UseBasicParsing
    } catch {
        Write-Err "Download failed: $($_.Exception.Message)"
        Write-Host "The release may not include Windows binaries."
        Write-Host "Check available releases at: https://github.com/$Repo/releases"
        return
    }
    $ProgressPreference = $PrevProgress

    # Verify file is not empty
    $FileSize = (Get-Item $BinaryPath).Length
    if ($FileSize -eq 0) {
        Write-Err "Downloaded file is empty — release binary may not exist for this platform."
        return
    }

    # ── Verify checksum ───────────────────────────────────────────────────────

    $ChecksumPath = Join-Path $TmpDir "checksums.txt"
    try {
        $ProgressPreference = "SilentlyContinue"
        Invoke-WebRequest -Uri $ChecksumUrl -OutFile $ChecksumPath -UseBasicParsing
        $ProgressPreference = $PrevProgress

        $ExpectedLine = Get-Content $ChecksumPath | Where-Object { $_ -like "*$BinaryName*" } | Select-Object -First 1
        if ($ExpectedLine) {
            $Expected = ($ExpectedLine -split "\s+")[0]
            $Actual = (Get-FileHash -Path $BinaryPath -Algorithm SHA256).Hash.ToLower()
            if ($Expected -ne $Actual) {
                Write-Err "Checksum mismatch!"
                Write-Host "  Expected: $Expected"
                Write-Host "  Actual:   $Actual"
                return
            }
            Write-Info "Checksum verified."
        } else {
            Write-Warn "Binary not found in checksums file — skipping verification."
        }
    } catch {
        Write-Warn "Checksums file unavailable — skipping verification."
    }

    # ── Install ────────────────────────────────────────────────────────────────

    New-Item -ItemType Directory -Path $Prefix -Force | Out-Null
    Copy-Item $BinaryPath $TargetPath -Force

    Write-Info "Installed: $TargetPath"

    # ── Add to PATH ────────────────────────────────────────────────────────────

    $Scope = if ($IsAdmin) { "Machine" } else { "User" }
    $CurrentPath = [Environment]::GetEnvironmentVariable("Path", $Scope)
    $PathParts = if ($null -ne $CurrentPath) { $CurrentPath -split ";" } else { @() }

    $AlreadyInPath = $PathParts | Where-Object { $_ -eq $Prefix }
    if ($null -eq $AlreadyInPath) {
        $NewPath = if ($CurrentPath) { "$Prefix;$CurrentPath" } else { $Prefix }
        [Environment]::SetEnvironmentVariable("Path", $NewPath, $Scope)
        Write-Info "Added to $Scope PATH: $Prefix"
        Write-Warn "Open a new terminal for PATH changes to take effect."
    }

    # ── Verify ─────────────────────────────────────────────────────────────────

    & $TargetPath version

    $Success = $true

    # ── Next steps ─────────────────────────────────────────────────────────────

    Write-Host ""
    Write-Host "Next steps:" -ForegroundColor White
    Write-Step "  hotplex onboard          # Interactive setup (config, secrets, messaging)"
    Write-Step "  hotplex gateway start    # Start the gateway"
    Write-Step "  hotplex dev              # Start in dev mode"
    Write-Host ""
    Write-Host "Shell completions: hotplex completion powershell" -ForegroundColor DarkGray

} finally {
    Remove-Item $TmpDir -Recurse -Force -ErrorAction SilentlyContinue
    if (-not $Success) {
        exit 1
    }
}
