& {
[CmdletBinding()]
param(
    [switch]$FromSource,
    [string]$Version = "latest",
    [string]$InstallDir = "",
    [switch]$Service,
    [switch]$OpenFirewall,
    [string]$Listen = ":8080",
    [switch]$TlsAuto,
    [switch]$CheckOnly,
    [switch]$Yes,
    [switch]$Uninstall
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
$Repo = "csx5/lowi"
$MinGoMinor = 24

function Write-Info([string]$Msg) { Write-Host "[INFO] $Msg" }
function Write-Ok([string]$Msg)   { Write-Host "[OK]   $Msg" -ForegroundColor Green }
function Write-Warn([string]$Msg) { Write-Warning $Msg }

function Test-IsAdmin {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]$identity
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Confirm-Action([string]$Prompt) {
    if ($Yes) { return $true }
    $ans = Read-Host "$Prompt [j/N]"
    return ($ans -match '^(j|ja|y|yes)$')
}

function Get-GoVersionInfo {
    $cmd = Get-Command go -ErrorAction SilentlyContinue
    if (-not $cmd) { return $null }
    try {
        $raw = & $cmd.Source version 2>$null
        if ($raw -match 'go(\d+)\.(\d+)') {
            return @{
                Path  = $cmd.Source
                Major = [int]$Matches[1]
                Minor = [int]$Matches[2]
                Text  = $raw
            }
        }
    } catch {}
    return $null
}

function Test-GoOk($Info) {
    if (-not $Info) { return $false }
    return ($Info.Major -gt 1) -or ($Info.Major -eq 1 -and $Info.Minor -ge $MinGoMinor)
}

function Install-MissingDependency([string]$ToolName, [string]$WingetId, [string]$ChocoPkg) {
    if (-not (Confirm-Action "$ToolName fehlt oder ist veraltet. Automatisch installieren?")) {
        throw "Abhaengigkeit '$ToolName' wird benoetigt."
    }
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        Write-Info "Installiere $ToolName ueber winget ($WingetId) ..."
        & winget install --id $WingetId --exact --accept-source-agreements --accept-package-agreements
        if ($LASTEXITCODE -ne 0) { throw "winget Installation von $ToolName fehlgeschlagen." }
    } elseif (Get-Command choco -ErrorAction SilentlyContinue) {
        Write-Info "Installiere $ToolName ueber Chocolatey ($ChocoPkg) ..."
        & choco install $ChocoPkg -y
        if ($LASTEXITCODE -ne 0) { throw "Chocolatey Installation von $ToolName fehlgeschlagen." }
    } else {
        throw "Weder winget noch choco gefunden. Bitte $ToolName manuell installieren."
    }
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
}

if ([string]::IsNullOrWhiteSpace($InstallDir)) {
    if ($Service -or (Test-IsAdmin)) {
        $InstallDir = Join-Path $env:ProgramFiles "it-kb"
    } else {
        $InstallDir = Join-Path $env:LOCALAPPDATA "it-kb"
    }
}

$ExePath = Join-Path $InstallDir "it-kb.exe"
$DataDir = Join-Path $InstallDir "data"
$ConfigPath = Join-Path $InstallDir "config.json"
$LocalRepoDir = $null
if ((Test-Path ".\go.mod") -and (Test-Path ".\cmd\it-kb\main.go")) {
    $LocalRepoDir = $PWD.Path
} elseif (-not [string]::IsNullOrWhiteSpace($PSScriptRoot) -and (Test-Path (Join-Path $PSScriptRoot "go.mod")) -and (Test-Path (Join-Path $PSScriptRoot "cmd\it-kb\main.go"))) {
    $LocalRepoDir = $PSScriptRoot
}
$InLocalRepo = ($null -ne $LocalRepoDir)

if ($Uninstall) {
    if (-not (Confirm-Action "it-kb unter '$InstallDir' entfernen? (Daten unter '$DataDir' bleiben erhalten)")) {
        Write-Info "Deinstallation abgebrochen."
        return
    }
    $svc = Get-Service -Name "it-kb" -ErrorAction SilentlyContinue
    if ($svc) {
        if (-not (Test-IsAdmin)) { throw "Zum Entfernen des Windows-Dienstes werden Administratorrechte benoetigt." }
        if ($svc.Status -ne "Stopped") {
            Stop-Service -Name "it-kb" -Force
        }
        if (Test-Path $ExePath) {
            & $ExePath service uninstall | Out-Null
        } else {
            & sc.exe delete it-kb | Out-Null
        }
        Write-Ok "Windows-Dienst 'it-kb' entfernt"
    }
    if (Test-Path $ExePath) {
        Remove-Item -Path $ExePath -Force
        Write-Ok "Binary entfernt: $ExePath"
    }
    Write-Info "Datenverzeichnis ($DataDir) und Konfiguration ($ConfigPath) blieben unveraendert."
    return
}

$arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { throw "Nur 64-Bit-Windows wird unterstuetzt." }

if ($CheckOnly) {
    Write-Info "System: Windows/${arch} | Zielverzeichnis: $InstallDir"
    $goInfo = Get-GoVersionInfo
    if (Test-GoOk $goInfo) { Write-Ok "Go: $($goInfo.Text)" } else { Write-Warn "Go >= 1.$MinGoMinor nicht installiert (fuer -FromSource benoetigt)." }
    if (Get-Command git -ErrorAction SilentlyContinue) { Write-Ok "git gefunden" } else { Write-Warn "git nicht installiert (nur fuer Remote--FromSource benoetigt)." }
    if (Get-Command winget -ErrorAction SilentlyContinue) { Write-Ok "Paketmanager: winget" } elseif (Get-Command choco -ErrorAction SilentlyContinue) { Write-Ok "Paketmanager: choco" } else { Write-Warn "Kein Paketmanager (winget/choco) gefunden" }
    return
}

if ($Service -and -not (Test-IsAdmin)) {
    throw "Fuer -Service sind Administratorrechte erforderlich (PowerShell als Administrator starten)."
}

$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("itkb-install-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $TempDir -Force | Out-Null

try {
    $BuiltExe = Join-Path $TempDir "it-kb.exe"

    if ($FromSource) {
        $goInfo = Get-GoVersionInfo
        if (-not (Test-GoOk $goInfo)) {
            Install-MissingDependency "Go (>= 1.$MinGoMinor)" "GoLang.Go" "golang"
            $goInfo = Get-GoVersionInfo
            if (-not (Test-GoOk $goInfo)) {
                throw "Go >= 1.$MinGoMinor wurde nach der Installation nicht gefunden. PowerShell ggf. neu oeffnen."
            }
        }
        Write-Ok "Go geprueft: $($goInfo.Text)"

        $SrcDir = $LocalRepoDir
        if (-not $InLocalRepo) {
            if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
                Install-MissingDependency "Git" "Git.Git" "git"
            }
            $SrcDir = Join-Path $TempDir "src"
            Write-Info "Klone https://github.com/${Repo}.git ..."
            $env:GIT_TERMINAL_PROMPT = "0"
            if ($Version -ne "latest") {
                & git clone --depth 1 --branch $Version "https://github.com/${Repo}.git" $SrcDir
            } else {
                & git clone --depth 1 "https://github.com/${Repo}.git" $SrcDir
            }
            if ($LASTEXITCODE -ne 0) {
                $hasGh = (Get-Command gh -ErrorAction SilentlyContinue)
                if ($hasGh) {
                    if (Test-Path $SrcDir) { Remove-Item -Path $SrcDir -Recurse -Force -ErrorAction SilentlyContinue }
                    if ($Version -ne "latest") {
                        & gh repo clone $Repo $SrcDir -- --depth 1 --branch $Version
                    } else {
                        & gh repo clone $Repo $SrcDir -- --depth 1
                    }
                }
                if ($LASTEXITCODE -ne 0) { throw "git clone fehlgeschlagen (bei privatem Repo 'gh auth login' oder lokales Repository nutzen)." }
            }
        }

        $BuildVer = if ($Version -eq "latest") { "dev" } else { $Version }
        if ((Test-Path (Join-Path $SrcDir ".git")) -and (Get-Command git -ErrorAction SilentlyContinue)) {
            $gitVer = (& git -C $SrcDir describe --tags --always 2>$null)
            if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($gitVer)) { $BuildVer = $gitVer.Trim() }
        }

        Write-Info "Kompiliere it-kb.exe ($BuildVer) ..."
        Push-Location $SrcDir
        try {
            $env:CGO_ENABLED = "0"
            & $goInfo.Path build -ldflags "-s -w -X main.version=$BuildVer" -o $BuiltExe ./cmd/it-kb
            if ($LASTEXITCODE -ne 0) { throw "go build fehlgeschlagen." }
        } finally {
            Pop-Location
        }
        Write-Ok "Build erfolgreich"
    } else {
        $AssetName = "it-kb-windows-${arch}.exe"
        $BaseUrl = if ($Version -eq "latest") {
            "https://github.com/${Repo}/releases/latest/download"
        } else {
            "https://github.com/${Repo}/releases/download/${Version}"
        }
        $SumsFile = Join-Path $TempDir "SHA256SUMS.txt"

        Write-Info "Lade Release-Binary ($AssetName) von $Repo ..."
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
        try {
            Invoke-WebRequest -Uri "$BaseUrl/$AssetName" -OutFile $BuiltExe -UseBasicParsing
            Invoke-WebRequest -Uri "$BaseUrl/SHA256SUMS.txt" -OutFile $SumsFile -UseBasicParsing
        } catch {
            $token = if ($env:GITHUB_TOKEN) { $env:GITHUB_TOKEN } else { $env:GH_TOKEN }
            $ghCmd = Get-Command gh -ErrorAction SilentlyContinue
            if ($ghCmd) {
                Write-Info "Oeffentlicher Download nicht verfuegbar - verwende GitHub CLI (gh) ..."
                if ($Version -eq "latest") {
                    & $ghCmd.Source release download --repo $Repo --pattern $AssetName --pattern "SHA256SUMS.txt" --dir $TempDir --clobber
                } else {
                    & $ghCmd.Source release download $Version --repo $Repo --pattern $AssetName --pattern "SHA256SUMS.txt" --dir $TempDir --clobber
                }
                if ($LASTEXITCODE -ne 0) { throw "Release-Download via gh fehlgeschlagen." }
                Move-Item -Path (Join-Path $TempDir $AssetName) -Destination $BuiltExe -Force
            } elseif ($token) {
                Write-Info "Oeffentlicher Download nicht verfuegbar - verwende GITHUB_TOKEN ..."
                $apiUrl = if ($Version -eq "latest") { "https://api.github.com/repos/$Repo/releases/latest" } else { "https://api.github.com/repos/$Repo/releases/tags/$Version" }
                $hdrJson = @{ Authorization = "Bearer $token"; Accept = "application/vnd.github+json" }
                $rel = Invoke-RestMethod -Uri $apiUrl -Headers $hdrJson
                $binAsset = $rel.assets | Where-Object { $_.name -eq $AssetName } | Select-Object -First 1
                $sumAsset = $rel.assets | Where-Object { $_.name -eq "SHA256SUMS.txt" } | Select-Object -First 1
                if (-not $binAsset -or -not $sumAsset) { throw "Release-Assets nicht ueber GitHub API gefunden." }
                $hdrBin = @{ Authorization = "Bearer $token"; Accept = "application/octet-stream" }
                Invoke-WebRequest -Uri $binAsset.url -Headers $hdrBin -OutFile $BuiltExe -UseBasicParsing
                Invoke-WebRequest -Uri $sumAsset.url -Headers $hdrBin -OutFile $SumsFile -UseBasicParsing
            } else {
                throw "Release-Download fehlgeschlagen (bei privatem Repo 'gh auth login', GITHUB_TOKEN setzen oder im geklonten Repo '-FromSource' nutzen)."
            }
        }

        $ExpectedHash = $null
        foreach ($line in Get-Content $SumsFile) {
            if ($line -match '^\s*([A-Fa-f0-9]{64})\s+\*?(.+)$') {
                $hash = $Matches[1].ToLowerInvariant()
                $name = $Matches[2].Trim()
                if ($name -eq $AssetName -or $name -eq "dist/$AssetName" -or $name.EndsWith("/$AssetName")) {
                    $ExpectedHash = $hash
                    break
                }
            }
        }
        if (-not $ExpectedHash) {
            throw "Kein SHA-256-Eintrag fuer '$AssetName' in SHA256SUMS.txt gefunden."
        }
        $ActualHash = (Get-FileHash -Path $BuiltExe -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($ActualHash -ne $ExpectedHash) {
            throw "SHA-256-Pruefung fehlgeschlagen! Erwartet: $ExpectedHash, erhalten: $ActualHash"
        }
        Write-Ok "SHA-256-Pruefsumme verifiziert ($ActualHash)"
    }

    $WasRunning = $false
    $existingSvc = Get-Service -Name "it-kb" -ErrorAction SilentlyContinue
    if ($existingSvc -and $existingSvc.Status -eq "Running") {
        if (-not (Test-IsAdmin)) { throw "Laufender Dienst 'it-kb' kann nur mit Administratorrechten aktualisiert werden." }
        $WasRunning = $true
        Write-Info "Stoppe laufenden Dienst 'it-kb' fuer Update ..."
        Stop-Service -Name "it-kb" -Force
    }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    New-Item -ItemType Directory -Path $DataDir -Force | Out-Null
    Copy-Item -Path $BuiltExe -Destination $ExePath -Force
    Write-Ok "Binary installiert: $ExePath"

    if (Test-Path $ConfigPath) {
        Write-Info "Bestehende Konfiguration bleibt unveraendert: $ConfigPath"
    } else {
        $cfgObj = [ordered]@{
            listen                     = $Listen
            data_dir                   = $DataDir
            site_title                 = "IT-Wissensdatenbank"
            auth_required              = $true
            session_hours              = 12
            bootstrap_admin            = ""
            bootstrap_password         = ""
            tls_auto                   = [bool]$TlsAuto
            tls_cert                   = ""
            tls_key                    = ""
            trusted_proxies            = @()
            auto_backup_dir            = ""
            auto_backup_interval_hours = 0
            auto_backup_keep           = 7
            auto_backup_passphrase     = ""
            review_reminder_days       = 180
            max_image_mb               = 15
            max_video_mb               = 100
            max_attachment_mb          = 25
        }
        $cfgJson = $cfgObj | ConvertTo-Json -Depth 4
        [System.IO.File]::WriteAllText($ConfigPath, $cfgJson, (New-Object System.Text.UTF8Encoding($false)))
        Write-Ok "Standard-Konfiguration angelegt: $ConfigPath"
    }

    if ($Service) {
        if (-not $existingSvc) {
            & $ExePath service install --config $ConfigPath
            if ($LASTEXITCODE -ne 0) { throw "Dienst-Registrierung fehlgeschlagen." }
        }
        & sc.exe start it-kb | Out-Null
        Write-Ok "Windows-Dienst 'it-kb' aktiviert und gestartet"
    } elseif ($WasRunning) {
        Start-Service -Name "it-kb"
        Write-Ok "Windows-Dienst 'it-kb' neu gestartet"
    }

    if ($OpenFirewall) {
        if (-not (Test-IsAdmin)) { throw "Fuer -OpenFirewall sind Administratorrechte erforderlich." }
        $port = ($Listen -split ':')[-1]
        if (-not ($port -match '^\d+$')) { $port = "8080" }
        $ruleName = "IT-Wissensdatenbank (it-kb)"
        if (-not (Get-NetFirewallRule -DisplayName $ruleName -ErrorAction SilentlyContinue)) {
            New-NetFirewallRule -DisplayName $ruleName -Direction Inbound -Protocol TCP -LocalPort ([int]$port) -Action Allow -Profile Domain,Private | Out-Null
            Write-Ok "Firewall-Regel fuer Port $port (Domain/Private) erstellt"
        }
    }

    $verOut = & $ExePath version
    Write-Ok "Installiert: $verOut"
    $proto = if ($TlsAuto) { "https" } else { "http" }
    $port = ($Listen -split ':')[-1]
    if (-not ($port -match '^\d+$')) { $port = "8080" }
    Write-Host ""
    Write-Host "Starten: & `"$ExePath`" serve --config `"$ConfigPath`""
    Write-Host "Web-UI:  ${proto}://localhost:${port}"
} finally {
    if (Test-Path $TempDir) {
        Remove-Item -Path $TempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}
} @args
