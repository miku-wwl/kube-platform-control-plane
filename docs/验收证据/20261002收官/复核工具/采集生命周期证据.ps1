param([Parameter(Mandatory)][string]$SessionFile,[ValidateSet('Draft','CreatePlan','CreateApplied','Updated','DestroyPlan','DestroyApproved','Deleted')][string]$Phase)
$ErrorActionPreference='Stop'
$session=Get-Content -LiteralPath $SessionFile -Raw | ConvertFrom-Json
$dest=Join-Path (Split-Path $PSScriptRoot -Parent) 'stage2'
New-Item -ItemType Directory -Force -Path $dest | Out-Null
$envName=$session.environmentName
$ns='platform-system'
function Kube([string]$config,[string[]]$items){
    $raw=& kubectl --kubeconfig $config @items -o json 2>&1
    $code=$LASTEXITCODE
    if($code -ne 0){throw (($raw | ForEach-Object{[string]$_}) -join "`n")}
    if(($raw -join '').Trim()){return ($raw -join "`n") | ConvertFrom-Json}
    return $null
}
function Check([bool]$value,[string]$name){$script:checks[$name]=$value;if(-not $value){throw "Failed assertion: $name"}}
function Bucket([bool]$present){
    $raw=& docker exec localstack-main awslocal s3api head-bucket --bucket $session.managedBucket 2>&1
    $code=$LASTEXITCODE
    $msg=($raw | ForEach-Object{[string]$_}) -join "`n"
    Check $(if($present){$code -eq 0}else{$code -ne 0 -and $msg -match '404|Not Found'}) 'bucketExpectedState'
    return [ordered]@{command='awslocal s3api head-bucket --bucket '+$session.managedBucket;exitCode=$code;output=$msg;expectedPresent=$present}
}
function Binding($plan,$approval,$apply){
    $planFields=[ordered]@{name=$plan.metadata.name;uid=$plan.metadata.uid;planRef=$plan.status.planRef;planDigest=$plan.status.planDigest;executionContextDigest=$plan.spec.executionContextDigest;effectivePlanInputDigest=$plan.status.effectivePlanInputDigest;planReportRef=$plan.status.planReportRef;planReportDigest=$plan.status.planReportDigest}
    Check ($approval.spec.planRunRef.name -eq $plan.metadata.name -and $approval.spec.planRunUID -eq $plan.metadata.uid) 'approvalPlanIdentity'
    foreach($field in @('planDigest','executionContextDigest','effectivePlanInputDigest','planReportRef','planReportDigest')){Check ($approval.spec.$field -eq $planFields[$field]) ('approval_'+$field);Check ($apply.spec.$field -eq $planFields[$field]) ('apply_'+$field)}
    Check ($apply.spec.planRunUID -eq $plan.metadata.uid -and $apply.spec.approvalUID -eq $approval.metadata.uid) 'applyPlanAndApprovalIdentity'
    Check ($apply.spec.planRef -eq $plan.status.planRef) 'applySavedPlanRef'
    Check ($apply.spec.approvalRef.name -eq $approval.metadata.name) 'applyApprovalName'
    return [ordered]@{plan=$planFields;approval=[ordered]@{name=$approval.metadata.name;uid=$approval.metadata.uid;spec=$approval.spec};apply=[ordered]@{name=$apply.metadata.name;uid=$apply.metadata.uid;spec=$apply.spec;executionOutcome=$apply.status.executionOutcome}}
}
$checks=[ordered]@{}
$environment=Kube $session.managementKubeconfig @('get','platformenvironment',$envName,'-n',$ns,'--ignore-not-found')
$service=Kube $session.targetKubeconfig @('get','service',$session.serviceName,'-n',$session.runtimeNamespace,'--ignore-not-found')
$record=[ordered]@{runId=$session.runId;phase=$Phase;capturedAtUTC=[DateTime]::UtcNow.ToString('o');environmentName=$envName;namespace=$ns;checks=$checks}
if($Phase -eq 'Draft'){
    Check ($null -eq $environment) 'noEnvironmentBeforeSubmit'
    $record.environmentQuery='kubectl get platformenvironment '+$envName+' --ignore-not-found -o json: empty'
}elseif($Phase -eq 'Deleted'){
    Check ($null -eq $environment) 'environmentNotFound'
    Check ($null -eq $service) 'runtimeServiceNotFound'
    $stack=Kube $session.managementKubeconfig @('get','infrastack',"$envName-infra",'-n',$ns,'--ignore-not-found')
    $set=Kube $session.managementKubeconfig @('get','resourceset',"$envName-resources",'-n',$ns,'--ignore-not-found')
    Check ($null -eq $stack) 'infraStackNotFound'
    Check ($null -eq $set) 'resourceSetNotFound'
    $record.bucket=Bucket $false
    $approved=Get-Content -LiteralPath (Join-Path $dest 'DestroyApproved.json') -Raw | ConvertFrom-Json
    $uid=$approved.binding.apply.uid
    $raw=& docker exec localstack-main awslocal s3 cp "s3://$($session.artifactBucket)/runs/$uid/terminal-result.json" - 2>&1
    if($LASTEXITCODE -ne 0){throw 'Cannot retrieve Destroy terminal result'}
    $terminal=($raw -join "`n") | ConvertFrom-Json
    Check ($terminal.terraformExitCode -eq 0 -and $terminal.executionOutcome -eq 'Succeeded') 'destroyTerminalSucceeded'
    Check ($terminal.planDigest -eq $approved.binding.plan.planDigest -and $terminal.planRef -eq $approved.binding.plan.planRef) 'destroyTerminalExactPlan'
    $record.terminal=$terminal
    $record.resourceQueries=[ordered]@{environment='NotFound';infraStack='NotFound';resourceSet='NotFound';runtimeService='NotFound';managedBucket='404 Not Found'}
}else{
    Check ($null -ne $environment) 'environmentExists'
    $runs=Kube $session.managementKubeconfig @('get','terraformruns','-n',$ns)
    $mode=if($Phase -like 'Destroy*'){'Destroy'}else{'Reconcile'}
    $plan=$runs.items | Where-Object {$_.spec.stackRef.name -eq "$envName-infra" -and $_.spec.operation -eq 'Plan' -and $_.spec.planMode -eq $mode} | Sort-Object {$_.metadata.creationTimestamp} -Descending | Select-Object -First 1
    Check ($null -ne $plan) 'planExists'
    $applies=@($runs.items | Where-Object {$_.spec.operation -eq 'Apply' -and $_.spec.planRunUID -eq $plan.metadata.uid})
    $record.environment=[ordered]@{uid=$environment.metadata.uid;generation=$environment.metadata.generation;deletionTimestamp=$environment.metadata.deletionTimestamp;spec=$environment.spec;status=$environment.status}
    $record.plan=[ordered]@{name=$plan.metadata.name;uid=$plan.metadata.uid;mode=$plan.spec.planMode;outcome=$plan.status.executionOutcome;status=$plan.status}
    if($Phase -in @('CreatePlan','DestroyPlan')){
        Check ($plan.status.executionOutcome -eq 'ChangesPresent') 'planChangesPresent'
        Check ($applies.Count -eq 0) 'noApplyBeforeApproval'
        $record.bucket=Bucket ($Phase -eq 'DestroyPlan')
        $graph=Invoke-RestMethod -Uri "http://127.0.0.1:8090/api/terraform-runs/$ns/$($plan.metadata.name)/plan"
        $record.planPreview=$graph
        if($Phase -eq 'DestroyPlan'){Check ($null -ne $environment.metadata.deletionTimestamp) 'deletionTimestampPresent';Check ($null -eq $service) 'runtimePrunedBeforeDestroy';Check ($graph.graph.summary.delete -eq 1) 'destroyPreviewOneDelete'}else{Check ($graph.graph.summary.create -eq 1) 'createPreviewOneCreate'}
    }elseif($Phase -in @('CreateApplied','DestroyApproved')){
        Check ($applies.Count -eq 1) 'oneApplyForPlan'
        $approvals=Kube $session.managementKubeconfig @('get','changeapprovals','-n',$ns)
        $approval=$approvals.items | Where-Object {$_.spec.planRunUID -eq $plan.metadata.uid} | Select-Object -First 1
        Check ($null -ne $approval) 'approvalExists'
        $record.binding=Binding $plan $approval $applies[0]
        $detail=Invoke-RestMethod -Uri "http://127.0.0.1:8090/api/environments/$ns/$envName"
        Check ($detail.latestApproval -eq 'Recorded') 'uiApprovalRecorded'
        $record.uiDetail=$detail
        if($Phase -eq 'CreateApplied'){
            Check ($applies[0].status.executionOutcome -eq 'Succeeded') 'createApplySucceeded'
            Check ($detail.ready -and $detail.infrastructure -eq 'Ready' -and $detail.runtime -eq 'Ready') 'environmentAndRuntimeReady'
            Check ($null -ne $service) 'runtimeServiceExists'
            $record.serviceUID=$service.metadata.uid
            $record.bucket=Bucket $true
            $uid=$applies[0].metadata.uid
            $raw=& docker exec localstack-main awslocal s3 cp "s3://$($session.artifactBucket)/runs/$uid/terminal-result.json" - 2>&1
            if($LASTEXITCODE -ne 0){throw 'Cannot retrieve create terminal result'}
            $record.terminal=($raw -join "`n") | ConvertFrom-Json
            Check ($record.terminal.terraformExitCode -eq 0) 'createTerminalExitZero'
            Check ($record.terminal.executionOutcome -eq 'Succeeded' -and $record.terminal.planDigest -eq $plan.status.planDigest -and $record.terminal.planRef -eq $plan.status.planRef) 'createTerminalExactPlan'
        }
    }elseif($Phase -eq 'Updated'){
        $created=Get-Content -LiteralPath (Join-Path $dest 'CreateApplied.json') -Raw | ConvertFrom-Json
        Check ($environment.metadata.generation -eq 2 -and $environment.status.observedGeneration -eq 2 -and $environment.spec.capacity.nodeCount -eq 2) 'generationTwoObserved'
        Check ($plan.status.executionOutcome -eq 'NoChange' -and $applies.Count -eq 0) 'noChangeWithoutExtraApply'
        $allApplies=@($runs.items | Where-Object {$_.spec.stackRef.name -eq "$envName-infra" -and $_.spec.operation -eq 'Apply'})
        Check ($allApplies.Count -eq 1) 'totalApplyCountStillOne'
        Check ($null -ne $service -and $service.metadata.uid -eq $created.serviceUID) 'serviceUIDUnchanged'
        $detail=Invoke-RestMethod -Uri "http://127.0.0.1:8090/api/environments/$ns/$envName"
        Check ($detail.ready -and $detail.infrastructure -eq 'Ready' -and $detail.runtime -eq 'Ready') 'updatedReady'
        $record.uiDetail=$detail
        $record.serviceUID=$service.metadata.uid
        $record.bucket=Bucket $true
    }
}
$record | ConvertTo-Json -Depth 45 | Set-Content -LiteralPath (Join-Path $dest ($Phase+'.json')) -Encoding utf8
Write-Host "$Phase PASS assertions=$($checks.Count)"
