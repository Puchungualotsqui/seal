param(
    [string]$Version = "v0.8.2"
)

$ErrorActionPreference = "Stop"

$releaseName = $Version.TrimStart("v")
$archiveName = "simde-$releaseName.zip"
$archiveUrl =
    "https://github.com/simd-everywhere/simde/archive/refs/tags/$Version.zip"

$temporaryRoot =
    Join-Path `
        ([System.IO.Path]::GetTempPath()) `
        ("seal-simde-" + [Guid]::NewGuid().ToString("N"))

$archivePath =
    Join-Path `
        $temporaryRoot `
        $archiveName

$extractedRoot =
    Join-Path `
        $temporaryRoot `
        "extracted"

$sourceRoot =
    Join-Path `
        $extractedRoot `
        "simde-$releaseName"

$destinationRoot =
    Join-Path `
        $PSScriptRoot `
        "vendor\simde"

New-Item `
    -ItemType Directory `
    -Path $temporaryRoot `
    -Force |
    Out-Null

try {
    Invoke-WebRequest `
        -Uri $archiveUrl `
        -OutFile $archivePath

    Expand-Archive `
        -Path $archivePath `
        -DestinationPath $extractedRoot

    if (Test-Path $destinationRoot) {
        Remove-Item `
            -Path $destinationRoot `
            -Recurse `
            -Force
    }

    New-Item `
        -ItemType Directory `
        -Path $destinationRoot `
        -Force |
        Out-Null

    Copy-Item `
        -Path (
            Join-Path `
                $sourceRoot `
                "simde"
        ) `
        -Destination (
            Join-Path `
                $destinationRoot `
                "simde"
        ) `
        -Recurse

    Copy-Item `
        -Path (
            Join-Path `
                $sourceRoot `
                "COPYING"
        ) `
        -Destination $destinationRoot

    Copy-Item `
        -Path (
            Join-Path `
                $sourceRoot `
                "README.md"
        ) `
        -Destination $destinationRoot

    @"
# Vendored SIMDe

Version: $Version

Files copied from the upstream release:

- COPYING
- README.md
- simde/
"@ |
        Set-Content `
            -Path (
                Join-Path `
                    $destinationRoot `
                    "UPSTREAM.md"
            )

    Write-Host (
        "SIMDe $Version vendored into " +
        $destinationRoot
    )
}
finally {
    if (Test-Path $temporaryRoot) {
        Remove-Item `
            -Path $temporaryRoot `
            -Recurse `
            -Force
    }
}
