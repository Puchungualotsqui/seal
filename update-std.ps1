$sealHome = if ($env:SEAL_HOME) { $env:SEAL_HOME } else { "$HOME\.seal" }

Remove-Item "$sealHome\std" -Recurse -Force -ErrorAction SilentlyContinue
Copy-Item .\std "$sealHome\std" -Recurse

Write-Host "Updated std at $sealHome\std"
