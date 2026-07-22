$seal = (Get-Command seal.exe).Source
$failed = @()

Get-ChildItem .\examples\base, .\examples\core -Directory | ForEach-Object {
    Push-Location $_.FullName

    & $seal run . -compiler tcc

    if ($LASTEXITCODE) {
        $failed += $_.FullName
    }

    Pop-Location
}

if ($failed) {
    $failed | ForEach-Object { Write-Host "FAILED: $_" }
    exit 1
}

Write-Host "All examples passed."
