param(
    [string]$Version = "dev",
    [string]$OutDir = "dist"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
$oldGOOS = $env:GOOS
$oldGOARCH = $env:GOARCH
$oldCGO = $env:CGO_ENABLED
try {
    if ([System.IO.Path]::IsPathRooted($OutDir)) {
        $out = $OutDir
    } else {
        $out = Join-Path $root $OutDir
    }
    New-Item -ItemType Directory -Path $out -Force | Out-Null

    $winName = "go-bridge-$Version-windows-amd64"
    $linuxName = "go-bridge-$Version-linux-amd64"
    $winStage = Join-Path $out $winName
    $linuxStage = Join-Path $out $linuxName
    foreach ($stage in @($winStage, $linuxStage)) {
        if (Test-Path -LiteralPath $stage) {
            Remove-Item -LiteralPath $stage -Recurse -Force
        }
        New-Item -ItemType Directory -Path $stage | Out-Null
    }

    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    & go build -trimpath -o (Join-Path $winStage "go-bridge.exe") ./cmd/go-bridge
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & go build -trimpath -o (Join-Path $winStage "process-backend.exe") ./cmd/process-backend
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    & go build -trimpath -o (Join-Path $linuxStage "go-bridge") ./cmd/go-bridge
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & go build -trimpath -o (Join-Path $linuxStage "process-backend") ./cmd/process-backend
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    foreach ($stage in @($winStage, $linuxStage)) {
        Copy-Item -LiteralPath (Join-Path $root "README.md") -Destination $stage
        Copy-Item -LiteralPath (Join-Path $root ".env.example") -Destination $stage
        Copy-Item -LiteralPath (Join-Path $root "opencode.example.json") -Destination $stage
        Copy-Item -LiteralPath (Join-Path $root "specs") -Destination $stage -Recurse
        Copy-Item -LiteralPath (Join-Path $root "deploy") -Destination $stage -Recurse
    }
    Copy-Item -LiteralPath (Join-Path $root "scripts\start-linux.sh") -Destination (Join-Path $linuxStage "start.sh")

    $winZip = Join-Path $out "$winName.zip"
    if (Test-Path -LiteralPath $winZip) {
        Remove-Item -LiteralPath $winZip -Force
    }
    Compress-Archive -Path $winStage -DestinationPath $winZip -Force

    $linuxTar = Join-Path $out "$linuxName.tar.gz"
    if (Test-Path -LiteralPath $linuxTar) {
        Remove-Item -LiteralPath $linuxTar -Force
    }
    & tar -czf $linuxTar -C $out $linuxName
    if ($LASTEXITCODE -ne 0) {
        throw "tar failed; create the Linux archive manually or install tar"
    }

    Remove-Item -LiteralPath $winStage -Recurse -Force
    Remove-Item -LiteralPath $linuxStage -Recurse -Force

    Write-Output "Windows package: $winZip"
    Write-Output "Linux package: $linuxTar"
} finally {
    $env:GOOS = $oldGOOS
    $env:GOARCH = $oldGOARCH
    $env:CGO_ENABLED = $oldCGO
    Pop-Location
}
