param(
    [string]$ImageName = "forum:latest",
    [string]$ContainerName = "forum",
    [int]$Port = 8080
)

$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$DataDir = Join-Path $Root "data"

if (-not (Test-Path -LiteralPath $DataDir)) {
    New-Item -ItemType Directory -Path $DataDir | Out-Null
}

$existing = docker ps -aq --filter "name=^/$ContainerName$"
if ($existing) {
    docker rm -f $ContainerName | Out-Null
}

docker image build -t $ImageName $Root

docker container run -d `
    --name $ContainerName `
    -p "${Port}:8080" `
    -v "${DataDir}:/app/data" `
    -e DB_PATH=data/forum.db `
    -e SCHEMA_PATH=migrations/schema.sql `
    -e UPLOAD_DIR=data/uploads `
    -e UPLOAD_URL_PREFIX=/uploads `
    $ImageName

Write-Host "Forum is running at http://localhost:$Port"
