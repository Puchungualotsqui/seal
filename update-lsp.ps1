$h = if ($env:SEAL_HOME) { $env:SEAL_HOME } else { "$HOME\.seal" }
Stop-Process -Name zed -Force -ErrorAction SilentlyContinue
Remove-Item "$h\std" -Recurse -Force -ErrorAction SilentlyContinue
Copy-Item .\std "$h\std" -Recurse
go build -o "$HOME\bin\seal-lsp.exe" ./cmd/seal-lsp
