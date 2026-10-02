param([ValidateSet('initial','final')][string]$Phase)
$ErrorActionPreference='Stop'
$root=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../../..'))
$dest=Join-Path $root ('docs/验收证据/20261002收官/static-'+$Phase)
New-Item -ItemType Directory -Force -Path $dest | Out-Null
Set-Location $root
$checks=@(
    @{name='gofmt';file='gofmt';args=@('-l')+(rg --files -g '*.go' -g '!artifacts')},
    @{name='go-test';file='go';args=@('test','./...')},
    @{name='go-build';file='go';args=@('build','./cmd/controller','./cmd/terraform-runner','./cmd/platform-api')},
    @{name='go-vet';file='go';args=@('vet','./...')},
    @{name='go-race';file='go';args=@('test','-race','./...')}
)
if($Phase -eq 'initial'){$checks+=@{name='npm-ci';file='npm.cmd';args=@('--prefix','web','ci')}}
$checks+=@(
    @{name='frontend-build';file='npm.cmd';args=@('--prefix','web','run','build')},
    @{name='frontend-typecheck';file='npm.cmd';args=@('--prefix','web','run','typecheck')},
    @{name='frontend-lint';file='npm.cmd';args=@('--prefix','web','run','lint')},
    @{name='diff-check';file='git';args=@('diff','--check')}
)
$results=@()
foreach($check in $checks){
    $started=[DateTime]::UtcNow.ToString('o')
    $ErrorActionPreference='Continue'
    $out=& $check.file @($check.args) 2>&1
    $exitCode=$LASTEXITCODE
    $ErrorActionPreference='Stop'
    $text=($out | ForEach-Object{[string]$_}) -join "`n"
    if($check.name -eq 'gofmt' -and $text.Trim()){$exitCode=1}
    $text | Set-Content -LiteralPath (Join-Path $dest ($check.name+'.txt')) -Encoding utf8
    $results+= [ordered]@{check=$check.name;command=$check.file+' '+($check.args -join ' ');startedAtUTC=$started;finishedAtUTC=[DateTime]::UtcNow.ToString('o');exitCode=$exitCode;log=$check.name+'.txt'}
    $results | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $dest 'results.json') -Encoding utf8
    Write-Host "$Phase/$($check.name) exit=$exitCode"
    if($exitCode -ne 0){Write-Host $text;exit $exitCode}
}
exit 0
