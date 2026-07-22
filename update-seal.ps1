$bin = "$HOME\bin"
New-Item $bin -ItemType Directory -Force | Out-Null

go build -o "$bin\seal.exe" ./cmd/sealc

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (($userPath -split ";") -notcontains $bin) {
    [Environment]::SetEnvironmentVariable(
        "Path",
        (($userPath.TrimEnd(";") + ";$bin").TrimStart(";")),
        "User"
    )
}

$env:Path = "$bin;$env:Path"
where.exe seal
