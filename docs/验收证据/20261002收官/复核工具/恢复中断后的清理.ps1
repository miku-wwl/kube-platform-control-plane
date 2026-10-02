param([Parameter(Mandatory)][string]$SessionFile)
$ErrorActionPreference='Stop'
$session=Get-Content -LiteralPath $SessionFile -Raw | ConvertFrom-Json
if($session.runId -notmatch '^\d{14}-[0-9a-f]{8}$'){throw 'Invalid owned run ID'}
$suffix=$session.runId.Replace('-','')
$names=@('management','target-a','target-b') | ForEach-Object {"pcp-e2e-$suffix-$_"}
$buckets=@("pcp-e2e-artifacts-$suffix","pcp-e2e-$suffix","pcp-e2e-multi-$suffix-a","pcp-e2e-multi-$suffix-b","pcp-stage2-$suffix")
$steps=@()
foreach($name in $names){
    $output=kind delete cluster --name $name 2>&1
    $code=$LASTEXITCODE
    if($code -ne 0){throw "Owned cluster cleanup failed: $name"}
    $steps+=[ordered]@{command="kind delete cluster --name $name";exitCode=$code;output=($output -join "`n")}
    Write-Host "Removed owned cluster $name"
}
$raw=docker exec localstack-main awslocal s3api list-buckets --output json
if($LASTEXITCODE -ne 0){throw 'Cannot verify LocalStack buckets'}
$present=($raw | ConvertFrom-Json).Buckets.Name
foreach($bucket in $buckets){
    if($present -contains $bucket){
        $output=docker exec localstack-main awslocal s3 rb "s3://$bucket" --force 2>&1
        if($LASTEXITCODE -ne 0){throw "Owned bucket cleanup failed: $bucket"}
    }
}
$expected=[IO.Path]::GetFullPath((Join-Path ([IO.Path]::GetTempPath()) ('pcp-stage1-e2e-'+$session.runId)))
if([IO.Path]::GetFullPath($session.stateRoot) -ne $expected){throw 'Temporary path does not match owned run'}
if(Test-Path -LiteralPath $expected){
    $resolved=(Resolve-Path -LiteralPath $expected).Path
    if($resolved -ne $expected){throw 'Resolved temporary path changed'}
    Remove-Item -LiteralPath $resolved -Recurse -Force
}
$output=docker image rm ('pcp-stage1-git:'+$session.runId) 2>&1
if($LASTEXITCODE -ne 0){throw 'Cannot remove owned Git image alias'}
$remaining=kind get clusters 2>&1
if(@($names | Where-Object {$remaining -contains $_}).Count){throw 'Owned Kind clusters remain'}
$raw=docker exec localstack-main awslocal s3api list-buckets --output json
if($LASTEXITCODE -ne 0){throw 'Cannot verify final LocalStack state'}
$present=($raw | ConvertFrom-Json).Buckets.Name
if(@($buckets | Where-Object {$present -contains $_}).Count){throw 'Owned buckets remain'}
if(Test-Path -LiteralPath $expected){throw 'Owned temporary state remains'}
$root=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../../..'))
$hold=Join-Path $root ('artifacts/e2e/'+$session.runId+'/stage2-hold.flag')
if([IO.Path]::GetFullPath($session.holdFile) -ne [IO.Path]::GetFullPath($hold)){throw 'Unexpected hold-file path'}
if(Test-Path -LiteralPath $hold){Remove-Item -LiteralPath $hold}
[ordered]@{runId=$session.runId;capturedAtUTC=[DateTime]::UtcNow.ToString('o');originalHarnessExit='NOT OBSERVED: interrupted before final summary';recoveryCleanup='PASS';ownedClusters=$names;ownedBuckets=$buckets;ownedClustersRemaining=@();ownedBucketsRemaining=@();temporaryStateExists=$false;steps=$steps} | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path (Split-Path $PSScriptRoot -Parent) 'recovery-cleanup.json') -Encoding utf8
Write-Host 'Recovery cleanup PASS; this does not replace a complete Stage 1 rerun.'
