param([Parameter(Mandatory)][string]$RunDirectory,[string]$OutputDirectory=(Split-Path $PSScriptRoot -Parent))
$ErrorActionPreference='Stop'
$dest=$OutputDirectory
New-Item -ItemType Directory -Force -Path $dest | Out-Null
$evidence=Get-Content -LiteralPath (Join-Path $RunDirectory 'image-evidence.json') -Raw | ConvertFrom-Json
$rows=@()
foreach($image in $evidence.images){
    $pods=@($evidence.observedPods | Where-Object {$_.requestedImage -eq $image.tag})
    if($pods.Count -eq 0){throw "No running Pod observation for $($image.component)"}
    $identities=@($pods | Select-Object node,runningImageID -Unique)
    foreach($identity in $identities){
        $runningDigest=($identity.runningImageID -split '@')[-1]
        $raw=docker exec $identity.node ctr -n k8s.io content get $runningDigest
        if($LASTEXITCODE -ne 0){throw 'Cannot inspect actual Pod image index'}
        $importIndex=$raw | ConvertFrom-Json
        $raw=docker exec $identity.node ctr -n k8s.io content get $image.builtImageID
        if($LASTEXITCODE -ne 0){throw 'Cannot inspect built OCI index in Kind'}
        $builtIndex=$raw | ConvertFrom-Json
        $matched=@($importIndex.manifests | Where-Object {$_.digest -eq $image.builtImageID}).Count -eq 1
        if(-not $matched){throw 'Actual Pod image does not reference this build'}
        $rows+=[ordered]@{component=$image.component;tag=$image.tag;builtOCIIndexDigest=$image.builtImageID;actualPodImageID=$identity.runningImageID;node=$identity.node;builtIndexIncludedByRunningIndex=$matched;kindImportIndex=$importIndex;builtIndex=$builtIndex;observedPodCount=$pods.Count}
    }
}
[ordered]@{runId=$evidence.runId;capturedAtUTC=[DateTime]::UtcNow.ToString('o');images=$rows} | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath (Join-Path $dest 'image-identity-chain.json') -Encoding utf8
Copy-Item -LiteralPath (Join-Path $RunDirectory 'image-evidence.json') -Destination (Join-Path $dest 'image-evidence.json') -Force
Write-Host "Image identity PASS components=$($evidence.images.Count) PodObservations=$($evidence.observedPods.Count)"
