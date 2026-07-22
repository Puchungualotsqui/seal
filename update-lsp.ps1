Stop-Process -Name zed -Force -ErrorAction SilentlyContinue
go build -o "$HOME\bin\seal-lsp.exe" ./cmd/seal-lsp
