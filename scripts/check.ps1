$ErrorActionPreference = "Stop"
Push-Location (Split-Path -Parent $PSScriptRoot)

$unformatted = & gofmt -l .
if ($LASTEXITCODE -ne 0) {
    Pop-Location
    exit $LASTEXITCODE
}
if ($unformatted) {
    Write-Output $unformatted
    Pop-Location
    throw "Unformatted Go files"
}

& go build ./...
if ($LASTEXITCODE -ne 0) {
    Pop-Location
    exit $LASTEXITCODE
}
& go vet ./...
if ($LASTEXITCODE -ne 0) {
    Pop-Location
    exit $LASTEXITCODE
}
& go test -v ./... -count=1
$code = $LASTEXITCODE
Pop-Location
exit $code
