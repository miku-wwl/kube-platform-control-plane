[CmdletBinding()]
param(
    [ValidateSet('all', 'lifecycle', 'recovery', 'multitarget', 'failclosed', 'clean')]
    [string]$Suite = 'all',
    [string]$LocalStackEndpoint = $(if ($env:PCP_LOCALSTACK_ENDPOINT) { $env:PCP_LOCALSTACK_ENDPOINT } else { 'http://localhost:4566' }),
    [switch]$KeepArtifacts
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$script:RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$script:RunId = ('{0}-{1}' -f (Get-Date).ToUniversalTime().ToString('yyyyMMddHHmmss'), ([guid]::NewGuid().ToString('N').Substring(0, 8)))
$script:StateRoot = Join-Path ([IO.Path]::GetTempPath()) ('pcp-stage1-e2e-' + $script:RunId)
$script:ArtifactRoot = Join-Path $script:RepoRoot ('artifacts/e2e/' + $script:RunId)
$script:LogRoot = Join-Path $script:ArtifactRoot 'logs'
$script:OwnedClusters = New-Object System.Collections.Generic.List[string]
$script:TempPaths = New-Object System.Collections.Generic.List[string]
$script:Results = New-Object System.Collections.Generic.List[object]
$script:StartedProcesses = New-Object System.Collections.Generic.List[object]
$script:LocalStackContainer = ''
$script:ManagementCluster = ''
$script:ManagementContext = ''
$script:ManagementKubeconfig = ''
$script:TargetACluster = ''
$script:TargetBCluster = ''
$script:TargetAContext = ''
$script:TargetBContext = ''
$script:TargetKubeconfigA = ''
$script:TargetKubeconfigB = ''
$script:RunnerImage = ''
$script:ManagerImage = ''
$script:OriginalGitImageID = ''
$script:OwnedGitImage = ''
$script:ArtifactBucket = ''
$script:ArtifactPrefix = ''
$script:SourceA = $null
$script:SourceB = $null
$script:BackendNames = New-Object System.Collections.Generic.List[string]
$script:OwnedBuckets = New-Object System.Collections.Generic.List[string]
$script:EnvironmentNames = New-Object System.Collections.Generic.List[string]
$script:SuiteCompleted = $false
$script:ExitCode = 0

New-Item -ItemType Directory -Force -Path $script:ArtifactRoot, $script:LogRoot, $script:StateRoot | Out-Null

function Write-Stage {
    param([string]$Message)
    Write-Host ('[{0}] {1}' -f (Get-Date).ToString('HH:mm:ss'), $Message) -ForegroundColor Cyan
}

function Add-Result {
    param([string]$Name, [ValidateSet('PASS', 'FAIL', 'BLOCKED_LOCAL_ENVIRONMENT')][string]$Status, [string]$Evidence = '')
    $script:Results.Add([pscustomobject]@{ Name = $Name; Status = $Status; Evidence = $Evidence })
    Write-Host ('{0} {1}' -f $Name, $Status) -ForegroundColor $(if ($Status -eq 'PASS') { 'Green' } else { 'Yellow' })
}

function Invoke-Tool {
    param(
        [Parameter(Mandatory)][string]$File,
        [Parameter(Mandatory)][string[]]$Arguments,
        [int[]]$AllowedExitCodes = @(0),
        [string]$LogName = ''
    )
    $logPath = if ($LogName) { Join-Path $script:LogRoot $LogName } else { $null }
    $oldPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $output = & $File @Arguments 2>&1
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $oldPreference
    $text = ($output | ForEach-Object { [string]$_ }) -join "`n"
    if ($logPath) {
        $text | Set-Content -Path $logPath -Encoding UTF8
    }
    if ($AllowedExitCodes -notcontains $exitCode) {
        throw "Command failed ($exitCode): $File $($Arguments -join ' ')`n$text"
    }
    return $text
}

function Invoke-Kubectl {
    param(
        [Parameter(Mandatory)][string[]]$Arguments,
        [string]$Kubeconfig = $script:ManagementKubeconfig,
        [int[]]$AllowedExitCodes = @(0),
        [string]$LogName = ''
    )
    $args = @('--kubeconfig', $Kubeconfig) + $Arguments
    return Invoke-Tool -File 'kubectl' -Arguments $args -AllowedExitCodes $AllowedExitCodes -LogName $LogName
}

function Get-KubeJson {
    param([Parameter(Mandatory)][string[]]$Arguments, [string]$Kubeconfig = $script:ManagementKubeconfig)
    $raw = Invoke-Kubectl -Arguments ($Arguments + @('-o', 'json')) -Kubeconfig $Kubeconfig
    if ([string]::IsNullOrWhiteSpace($raw)) { return $null }
    return $raw | ConvertFrom-Json
}

function Write-KubeObject {
    param([Parameter(Mandatory)]$Object)
    $path = Join-Path $script:StateRoot ([guid]::NewGuid().ToString('N') + '.json')
    $Object | ConvertTo-Json -Depth 100 | Set-Content -Path $path -Encoding UTF8
    $script:TempPaths.Add($path)
    Invoke-Kubectl -Arguments @('apply', '-f', $path) | Out-Null
}

function Remove-KubeObject {
    param([Parameter(Mandatory)][string[]]$Arguments, [string]$Kubeconfig = $script:ManagementKubeconfig)
    Invoke-Kubectl -Arguments ($Arguments + @('--ignore-not-found=true')) -Kubeconfig $Kubeconfig -AllowedExitCodes @(0, 1) | Out-Null
}

function Wait-Until {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][scriptblock]$Condition,
        [int]$TimeoutSeconds = 300,
        [int]$PollSeconds = 2
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $lastError = ''
    while ((Get-Date) -lt $deadline) {
        try {
            $value = & $Condition
            if ($null -ne $value -and $value -ne $false) { return $value }
        } catch {
            $lastError = $_.Exception.Message
        }
        Start-Sleep -Seconds $PollSeconds
    }
    if ($lastError) { throw "Timed out waiting for $Name. Last error: $lastError" }
    throw "Timed out waiting for $Name"
}

function Get-Condition {
    param($Object, [string]$Type = 'Ready')
    if ($null -eq $Object -or $null -eq $Object.status) { return $null }
    return @($Object.status.conditions) | Where-Object { $_.type -eq $Type } | Select-Object -First 1
}

function Wait-Condition {
    param([string]$Resource, [string]$Name, [string]$Namespace = 'platform-system', [string]$Reason = '', [int]$TimeoutSeconds = 420)
    $resourceName = $Resource
    $objectName = $Name
    $namespaceName = $Namespace
    $reasonName = $Reason
    return Wait-Until -Name "$resourceName/$objectName" -TimeoutSeconds $TimeoutSeconds -Condition {
        $object = Get-KubeJson -Arguments @('get', $resourceName, $objectName, '-n', $namespaceName)
        $condition = Get-Condition $object
        if ($condition -and $condition.status -eq 'True' -and $condition.observedGeneration -eq $object.metadata.generation -and $object.status.observedGeneration -eq $object.metadata.generation -and (!$reasonName -or $condition.reason -eq $reasonName)) { return $object }
        return $null
    }
}

function Test-Dependencies {
    param([switch]$RequireDocker)
    Write-Stage 'Checking local toolchain'
    $names = @('go', 'terraform', 'git', 'python')
    if ($RequireDocker) { $names = @('docker', 'kind', 'kubectl') + $names }
    foreach ($name in $names) {
        if (-not (Get-Command $name -ErrorAction SilentlyContinue)) { throw "Required command not found: $name" }
    }
    if ($RequireDocker) { Invoke-Tool -File 'docker' -Arguments @('info') -LogName 'docker-info.txt' | Out-Null }
    $tf = Invoke-Tool -File 'terraform' -Arguments @('version') -LogName 'terraform-version.txt'
    if ($tf -notmatch 'Terraform v1\.14\.0') { throw "Terraform 1.14.0 is required. Observed: $tf" }
}

function Test-LocalStack {
    $uri = [Uri]$LocalStackEndpoint
    if ($uri.Host -notin @('localhost', '127.0.0.1', '::1')) { throw "LocalStack endpoint must be local, got $($uri.Host)" }
    Write-Stage "Checking external LocalStack at $LocalStackEndpoint"
    $health = $null
    try { $health = Invoke-RestMethod -Uri ($LocalStackEndpoint.TrimEnd('/') + '/_localstack/health') -TimeoutSec 10 } catch {
        try { $health = Invoke-RestMethod -Uri ($LocalStackEndpoint.TrimEnd('/') + '/health') -TimeoutSec 10 } catch { throw "LocalStack health check failed: $($_.Exception.Message)" }
    }
    $healthText = $health | ConvertTo-Json -Depth 20
    $healthText | Set-Content (Join-Path $script:ArtifactRoot 'localstack-health.txt') -Encoding UTF8
    if ($healthText -notmatch '"s3"\s*:\s*"(available|running|active)"') { throw "LocalStack S3 is not available: $healthText" }
    if ($healthText -notmatch '"sts"\s*:\s*"(available|running|active)"') { throw "LocalStack STS is not available: $healthText" }
    $names = Invoke-Tool -File 'docker' -Arguments @('ps', '--format', '{{.Names}}')
    $container = @($names -split "`r?`n" | Where-Object { $_ -match 'localstack' } | Select-Object -First 1)
    if (-not $container) { throw 'A running LocalStack container was not found; start LocalStack Ultimate externally first.' }
    $script:LocalStackContainer = $container
    $identity = Invoke-Tool -File 'docker' -Arguments @('exec', $script:LocalStackContainer, 'awslocal', 'sts', 'get-caller-identity')
    $identity | Set-Content (Join-Path $script:ArtifactRoot 'localstack-identity.txt') -Encoding UTF8
    if ($identity -notmatch '000000000000') { throw "Unexpected LocalStack identity: $identity" }
}

function New-Clusters {
    $suffix = $script:RunId.ToLower().Replace('-', '')
    $script:ManagementCluster = "pcp-e2e-$suffix-management"
    $script:TargetACluster = "pcp-e2e-$suffix-target-a"
    $script:TargetBCluster = "pcp-e2e-$suffix-target-b"
    foreach ($cluster in @($script:ManagementCluster, $script:TargetACluster, $script:TargetBCluster)) {
        Write-Stage "Creating Kind cluster $cluster"
        Invoke-Tool -File 'kind' -Arguments @('create', 'cluster', '--name', $cluster, '--wait', '120s') -LogName "$cluster-create.txt" | Out-Null
        $script:OwnedClusters.Add($cluster)
    }
    $script:ManagementContext = "kind-$($script:ManagementCluster)"
    $script:TargetAContext = "kind-$($script:TargetACluster)"
    $script:TargetBContext = "kind-$($script:TargetBCluster)"
    $script:ManagementKubeconfig = Join-Path $script:StateRoot 'management.kubeconfig'
    $script:TargetKubeconfigA = Join-Path $script:StateRoot 'target-a.kubeconfig'
    $script:TargetKubeconfigB = Join-Path $script:StateRoot 'target-b.kubeconfig'
    foreach ($pair in @(
        @($script:ManagementCluster, $script:ManagementKubeconfig),
        @($script:TargetACluster, $script:TargetKubeconfigA),
        @($script:TargetBCluster, $script:TargetKubeconfigB)
    )) {
        $raw = Invoke-Tool -File 'kind' -Arguments @('get', 'kubeconfig', '--name', $pair[0])
        $raw | Set-Content -Path $pair[1] -Encoding UTF8
        $script:TempPaths.Add($pair[1])
    }
}

function Build-Images {
    $tag = "local-$($script:RunId.ToLower())"
    $script:ManagerImage = "platform-control-plane:$tag"
    $script:RunnerImage = "platform-terraform-runner:$tag"
    Write-Stage "Building $($script:ManagerImage) and $($script:RunnerImage)"
    Invoke-Tool -File 'docker' -Arguments @('build', '-t', $script:ManagerImage, '-f', 'Dockerfile', '.') -LogName 'manager-build.txt' | Out-Null
    Invoke-Tool -File 'docker' -Arguments @('build', '-t', $script:RunnerImage, '-f', 'runner.Dockerfile', '.') -LogName 'runner-build.txt' | Out-Null
    Prepare-GitImage
    foreach ($cluster in $script:OwnedClusters) {
        Invoke-Tool -File 'kind' -Arguments @('load', 'docker-image', $script:ManagerImage, '--name', $cluster) -LogName "$cluster-load-manager.txt" | Out-Null
        Invoke-Tool -File 'kind' -Arguments @('load', 'docker-image', $script:RunnerImage, '--name', $cluster) -LogName "$cluster-load-runner.txt" | Out-Null
        Invoke-Tool -File 'kind' -Arguments @('load', 'docker-image', 'alpine/git:2.45.2', '--name', $cluster) -LogName "$cluster-load-git.txt" | Out-Null
    }
}

function Prepare-GitImage {
    try { $script:OriginalGitImageID = (Invoke-Tool -File 'docker' -Arguments @('image', 'inspect', '--format', '{{.Id}}', 'alpine/git:2.45.2')).Trim() } catch { $script:OriginalGitImageID = '' }
    $script:OwnedGitImage = "pcp-stage1-git:$($script:RunId.ToLower())"
    $dockerfile = Join-Path $script:StateRoot 'git.Dockerfile'
    @'
FROM alpine:3.20
RUN apk add --no-cache git ca-certificates
ENTRYPOINT ["git"]
'@ | Set-Content -Path $dockerfile -Encoding UTF8
    Invoke-Tool -File 'docker' -Arguments @('pull', '--platform', 'linux/amd64', 'alpine:3.20') -LogName 'git-base-pull.txt' | Out-Null
    Invoke-Tool -File 'docker' -Arguments @('build', '--platform', 'linux/amd64', '-t', $script:OwnedGitImage, '-f', $dockerfile, $script:StateRoot) -LogName 'git-image-build.txt' | Out-Null
    Invoke-Tool -File 'docker' -Arguments @('tag', $script:OwnedGitImage, 'alpine/git:2.45.2') | Out-Null
}

function Merge-TargetKubeconfig {
    $managerKubeconfigA = Join-Path $script:StateRoot 'target-a-manager.kubeconfig'
    $managerKubeconfigB = Join-Path $script:StateRoot 'target-b-manager.kubeconfig'
    Copy-Item -LiteralPath $script:TargetKubeconfigA -Destination $managerKubeconfigA -Force
    Copy-Item -LiteralPath $script:TargetKubeconfigB -Destination $managerKubeconfigB -Force
    $script:TempPaths.Add($managerKubeconfigA)
    $script:TempPaths.Add($managerKubeconfigB)
    $serverMap = @{
        $managerKubeconfigA = "https://$($script:TargetACluster)-control-plane:6443"
        $managerKubeconfigB = "https://$($script:TargetBCluster)-control-plane:6443"
    }
    foreach ($file in $serverMap.Keys) {
        $content = Get-Content -Raw -Path $file
        $content = $content -replace '(?m)(server:\s*)https://127\.0\.0\.1:\d+', ('$1' + $serverMap[$file])
        $content | Set-Content -Path $file -Encoding UTF8
    }
    $combined = Join-Path $script:StateRoot 'target-combined.kubeconfig'
    $previous = $env:KUBECONFIG
    try {
        $env:KUBECONFIG = "$managerKubeconfigA;$managerKubeconfigB"
        $raw = Invoke-Tool -File 'kubectl' -Arguments @('config', 'view', '--flatten', '--raw')
        $raw | Set-Content -Path $combined -Encoding UTF8
    } finally { $env:KUBECONFIG = $previous }
    $script:TempPaths.Add($combined)
    return $combined
}

function Install-Manager {
    Write-Stage 'Installing CRDs, RBAC, and controller into management Kind'
    Invoke-Kubectl -Arguments @('apply', '-f', (Join-Path $script:RepoRoot 'config/crd/bases')) | Out-Null
    Invoke-Kubectl -Arguments @('apply', '-f', (Join-Path $script:RepoRoot 'config/manager/namespace.yaml')) | Out-Null
    Invoke-Kubectl -Arguments @('apply', '-f', (Join-Path $script:RepoRoot 'config/rbac')) | Out-Null
    Invoke-Kubectl -Arguments @('apply', '-f', (Join-Path $script:RepoRoot 'config/manager/manager.yaml')) | Out-Null
    $combined = Merge-TargetKubeconfig
    $bytes = [IO.File]::ReadAllBytes($combined)
    $encoded = [Convert]::ToBase64String($bytes)
    $secret = [ordered]@{ apiVersion = 'v1'; kind = 'Secret'; metadata = [ordered]@{ name = 'pcp-target-kubeconfig'; namespace = 'platform-system' }; type = 'Opaque'; data = [ordered]@{ config = $encoded } }
    Write-KubeObject -Object $secret
    Invoke-Kubectl -Arguments @('set', 'image', 'deployment/platform-control-plane', '-n', 'platform-system', "manager=$($script:ManagerImage)") | Out-Null
    Invoke-Kubectl -Arguments @('set', 'env', 'deployment/platform-control-plane', '-n', 'platform-system',
        'PCP_ENABLE_TERRAFORM_EXECUTION=true',
        "PCP_ARTIFACT_ENDPOINT=$($LocalStackEndpoint.Replace('localhost', 'host.docker.internal'))",
        'PCP_ARTIFACT_REGION=us-east-1',
        "PCP_ARTIFACT_BUCKET=$($script:ArtifactBucket)",
        "PCP_TARGET_CONTEXTS=$($script:TargetAContext),$($script:TargetBContext)",
        'PCP_KUBECONFIG=/var/run/pcp/kubeconfig/config',
        'PCP_ENABLE_AWS_EKS=false') | Out-Null
    $patch = @(
        [ordered]@{ op = 'add'; path = '/spec/template/spec/volumes'; value = @([ordered]@{ name = 'target-kubeconfig'; secret = [ordered]@{ secretName = 'pcp-target-kubeconfig'; defaultMode = 420 } }) },
        [ordered]@{ op = 'add'; path = '/spec/template/spec/containers/0/volumeMounts'; value = @([ordered]@{ name = 'target-kubeconfig'; mountPath = '/var/run/pcp/kubeconfig'; readOnly = $true }) }
    ) | ConvertTo-Json -Depth 20 -Compress
    $patchPath = Join-Path $script:StateRoot 'manager-volume-patch.json'
    $patch | Set-Content -Path $patchPath -Encoding UTF8
    $script:TempPaths.Add($patchPath)
    Invoke-Kubectl -Arguments @('patch', 'deployment/platform-control-plane', '-n', 'platform-system', '--type=json', '--patch-file', $patchPath) | Out-Null
    Invoke-Kubectl -Arguments @('rollout', 'status', 'deployment/platform-control-plane', '-n', 'platform-system', '--timeout=180s') | Out-Null
    Wait-Until -Name 'manager readiness' -TimeoutSeconds 180 -Condition {
        $pods = Get-KubeJson -Arguments @('get', 'pods', '-n', 'platform-system', '-l', 'app.kubernetes.io/name=platform-control-plane')
        $items = @($pods.items)
        $readyItems = @($items | Where-Object {
            if ($_.status.phase -ne 'Running') { return $false }
            $readyContainers = @($_.status.containerStatuses | Where-Object { $_.ready })
            return $readyContainers.Count -gt 0
        })
        if ($items.Count -ge 2 -and $readyItems.Count -ge 2) { return $true }
        return $null
    } | Out-Null
    $ready = Invoke-Kubectl -Arguments @('get', 'deployment/platform-control-plane', '-n', 'platform-system', '-o', 'jsonpath={.status.readyReplicas}')
    if ($ready.Trim() -ne '2') { throw "manager deployment is not fully ready: $ready" }
}

function New-GitSource {
    param([string]$TargetContext, [string]$BucketName, [string]$Name)
    $root = Join-Path $script:StateRoot $Name
    $work = Join-Path $root 'work'
    $bare = Join-Path $script:StateRoot "$Name.git"
    New-Item -ItemType Directory -Force -Path $work, (Split-Path $bare) | Out-Null
    Copy-Item -Path (Join-Path $script:RepoRoot 'test/fixtures/terraform/localstack-basic/*') -Destination $work -Recurse -Force
    $variables = Join-Path $work 'variables.tf'
    $content = Get-Content -Raw -Path $variables
    $content = $content -replace 'default = "pcp-e2e-lifecycle-bucket"', ('default = "' + $BucketName + '"')
    $content = $content -replace 'default = "kind-pcp-target-local"', ('default = "' + $TargetContext + '"')
    $content = $content -replace 'default = "kubeconfig:kind-pcp-target-local"', ('default = "kubeconfig:' + $TargetContext + '"')
    $content | Set-Content -Path $variables -Encoding UTF8
    Push-Location $work
    try {
        Invoke-Tool -File 'git' -Arguments @('init', '--initial-branch=main') | Out-Null
        Invoke-Tool -File 'git' -Arguments @('config', 'user.email', 'e2e@local.invalid') | Out-Null
        Invoke-Tool -File 'git' -Arguments @('config', 'user.name', 'Stage1 E2E') | Out-Null
        Invoke-Tool -File 'git' -Arguments @('add', '.') | Out-Null
        Invoke-Tool -File 'git' -Arguments @('commit', '-m', 'stage1-e2e-fixture') | Out-Null
        $revision = (Invoke-Tool -File 'git' -Arguments @('rev-parse', 'HEAD')).Trim()
        Invoke-Tool -File 'git' -Arguments @('clone', '--bare', $work, $bare) | Out-Null
        Push-Location $bare
        try { Invoke-Tool -File 'git' -Arguments @('update-server-info') | Out-Null } finally { Pop-Location }
    } finally { Pop-Location }
    return [pscustomobject]@{ Name = $Name; Root = $root; Work = $work; Bare = $bare; Revision = $revision; TargetContext = $TargetContext; Bucket = $BucketName }
}

function Start-SourceServer {
    $port = Get-Random -Minimum 18081 -Maximum 28999
    $root = $script:StateRoot
    $stdout = Join-Path $script:LogRoot 'git-http.stdout.log'
    $stderr = Join-Path $script:LogRoot 'git-http.stderr.log'
    $process = Start-Process -FilePath 'python' -ArgumentList @('-m', 'http.server', "$port", '--bind', '0.0.0.0') -WorkingDirectory $root -RedirectStandardOutput $stdout -RedirectStandardError $stderr -WindowStyle Hidden -PassThru
    $script:StartedProcesses.Add($process)
    Wait-Until -Name 'temporary Git HTTP server' -TimeoutSeconds 30 -Condition {
        try { Invoke-Tool -File 'git' -Arguments @('ls-remote', "http://127.0.0.1:$port/$($script:SourceA.Name).git") | Out-Null; return $true } catch {}
        return $null
    } | Out-Null
    foreach ($source in @($script:SourceA, $script:SourceB) | Where-Object { $null -ne $_ }) {
        $source | Add-Member -NotePropertyName URL -NotePropertyValue "http://host.docker.internal:$port/$($source.Name).git"
    }
}

function New-BackendConfig {
    param([string]$Name, [string]$Key)
    $script:BackendNames.Add($Name)
    $endpoint = $LocalStackEndpoint.Replace('localhost', 'host.docker.internal')
    $hcl = @"
bucket = "$($script:ArtifactBucket)"
key = "$Key"
region = "us-east-1"
endpoints = { s3 = "$endpoint" }
force_path_style = true
skip_credentials_validation = true
skip_region_validation = true
skip_requesting_account_id = true
skip_metadata_api_check = true
use_lockfile = true
"@
    $object = [ordered]@{ apiVersion = 'v1'; kind = 'ConfigMap'; metadata = [ordered]@{ name = $Name; namespace = 'platform-system' }; data = [ordered]@{ 'backend.hcl' = $hcl } }
    Write-KubeObject -Object $object
}

function New-RuntimeServiceObject {
    param([string]$Name, [string]$Revision = '')
    $metadata = [ordered]@{ name = $Name; namespace = 'default'; labels = [ordered]@{ 'platform.example.io/managed-by' = 'platform-control-plane' } }
    if ($Revision) { $metadata.annotations = [ordered]@{ 'platform.example.io/e2e-revision' = $Revision } }
    return [ordered]@{ apiVersion = 'v1'; kind = 'Service'; metadata = $metadata; spec = [ordered]@{ selector = [ordered]@{ 'stage1-e2e' = $Name }; ports = @([ordered]@{ name = 'http'; port = 80; targetPort = 80 }) } }
}

function New-EnvironmentClass {
    param([string]$Name, $Source, [string]$BackendConfigName, [string]$RuntimeName, [string]$RunnerServiceAccountName = 'terraform-runner', [string]$RuntimeRevision = '')
    $runtime = [ordered]@{ version = 'v1'; resource = 'services'; readinessPolicy = 'RequireReady'; deletionPolicy = 'Prune'; ownershipID = $RuntimeName; object = (New-RuntimeServiceObject -Name $RuntimeName -Revision $RuntimeRevision) }
    $class = [ordered]@{
        apiVersion = 'platform.example.io/v1alpha1'; kind = 'EnvironmentClass'; metadata = [ordered]@{ name = $Name }
        spec = [ordered]@{
            version = 'stage1-e2e-v1'
            source = [ordered]@{ url = $Source.URL; revision = $Source.Revision; path = '.' }
            backend = [ordered]@{ type = 's3'; configRef = [ordered]@{ name = $BackendConfigName }; authRef = [ordered]@{ serviceAccountName = $RunnerServiceAccountName }; lockTimeout = '2m' }
            executor = [ordered]@{ terraformVersion = '1.14.0'; image = $script:RunnerImage; workDir = '/workspace/terraform'; executionTimeout = '12m' }
            runnerProfile = [ordered]@{ serviceAccountName = $RunnerServiceAccountName; image = $script:RunnerImage; imageDigest = 'local-stage1'; terraformVersion = '1.14.0' }
            runtimeProfile = [ordered]@{ runtimeObjects = @($runtime) }
            target = [ordered]@{ provider = 'kind'; account = 'local'; region = 'local'; clusterName = $Source.TargetContext; clusterID = $Source.TargetContext; incarnationID = $Source.TargetContext; connectionProfileRef = 'kubeconfig:' + $Source.TargetContext }
            allowedRegions = @('local')
            capacityBounds = [ordered]@{ minNodeCount = 1; maxNodeCount = 10; maxEnvironments = 10; maxConcurrentPlans = 2; maxConcurrentApplies = 1 }
            approvalPolicy = 'Manual'
        }
    }
    Write-KubeObject -Object $class
}

function New-Environment {
    param([string]$Name, [string]$ClassName)
    $script:EnvironmentNames.Add($Name)
    $object = [ordered]@{ apiVersion = 'platform.example.io/v1alpha1'; kind = 'PlatformEnvironment'; metadata = [ordered]@{ name = $Name; namespace = 'platform-system' }; spec = [ordered]@{ classRef = [ordered]@{ name = $ClassName }; capacity = [ordered]@{ nodeCount = 1 }; desiredState = 'Present' } }
    Write-KubeObject -Object $object
}

function Get-StackForEnvironment {
    param([string]$EnvironmentName)
    return (Get-KubeJson -Arguments @('get', 'infrastacks', '-n', 'platform-system') | Select-Object -ExpandProperty items | Where-Object { $_.metadata.ownerReferences.name -contains $EnvironmentName } | Select-Object -First 1)
}

function Get-PlanForStack {
    param([string]$StackName, [string]$PlanMode = 'Reconcile', [string]$ExcludeName = '')
    $list = Get-KubeJson -Arguments @('get', 'terraformruns', '-n', 'platform-system')
    return @($list.items) | Where-Object { $_.spec.stackRef.name -eq $StackName -and $_.spec.operation -eq 'Plan' -and $_.spec.planMode -eq $PlanMode -and $_.metadata.name -ne $ExcludeName } | Sort-Object { $_.metadata.creationTimestamp } -Descending | Select-Object -First 1
}

function Wait-Plan {
    param([string]$StackName, [string]$PlanMode = 'Reconcile', [string]$ExcludeName = '')
    $plan = Wait-Until -Name "Terraform $PlanMode Plan" -TimeoutSeconds 900 -Condition {
        $candidate = Get-PlanForStack -StackName $StackName -PlanMode $PlanMode -ExcludeName $ExcludeName
        if ($candidate -and $candidate.status.executionOutcome -in @('ChangesPresent', 'NoChange', 'Succeeded', 'SucceededPostMutation')) { return $candidate }
        if ($candidate -and $candidate.status.executionOutcome -in @('Failed', 'Indeterminate', 'Rejected')) { throw "Terraform plan failed: $($candidate.status.executionOutcome) $($candidate.status.conditions | ConvertTo-Json -Compress)" }
        return $null
    }
    if ($plan.status.executionOutcome -notin @('ChangesPresent', 'NoChange')) { throw "Expected terminal Plan outcome, got $($plan.status.executionOutcome)" }
    return $plan
}

function New-Approval {
    param($Plan, [string]$Name)
    if ($Plan.status.executionOutcome -ne 'ChangesPresent') { throw "Approval requested for non-mutating plan $($Plan.metadata.name)" }
    $approval = [ordered]@{ apiVersion = 'platform.example.io/v1alpha1'; kind = 'ChangeApproval'; metadata = [ordered]@{ name = $Name; namespace = 'platform-system' }; spec = [ordered]@{ planRunRef = [ordered]@{ name = $Plan.metadata.name }; planRunUID = $Plan.metadata.uid; planDigest = $Plan.status.planDigest; executionContextDigest = $Plan.spec.executionContextDigest; effectivePlanInputDigest = $Plan.status.effectivePlanInputDigest; planReportRef = $Plan.status.planReportRef; planReportDigest = $Plan.status.planReportDigest } }
    Write-KubeObject -Object $approval
    $approvalName = $Name
    return (Wait-Until -Name "approval/$approvalName" -TimeoutSeconds 120 -Condition { $a = Get-KubeJson -Arguments @('get', 'changeapproval', $approvalName, '-n', 'platform-system'); if ($a -and $a.spec -and $a.spec.planRunUID) { return $a }; return $null })
}

function Wait-Apply {
    param([string]$StackName, [string]$PlanUID)
    return Wait-Until -Name 'Terraform Apply' -TimeoutSeconds 900 -Condition {
        $runs = Get-KubeJson -Arguments @('get', 'terraformruns', '-n', 'platform-system')
        $apply = @($runs.items) | Where-Object { $_.spec.stackRef.name -eq $StackName -and $_.spec.operation -eq 'Apply' -and $_.spec.planRunUID -eq $PlanUID } | Sort-Object { $_.metadata.creationTimestamp } -Descending | Select-Object -First 1
        if ($apply -and $apply.status.executionOutcome -eq 'Succeeded') { return $apply }
        if ($apply -and $apply.status.executionOutcome -in @('Failed', 'Indeterminate', 'Rejected')) { throw "Terraform Apply failed: $($apply.status.executionOutcome) $($apply.status.conditions | ConvertTo-Json -Compress)" }
        return $null
    }
}

function Read-ArtifactJson {
    param([Parameter(Mandatory)][string]$Key)
    $remotePath = '/tmp/pcp-stage1-terminal-' + ([guid]::NewGuid().ToString('N')) + '.json'
    try {
        Invoke-Tool -File 'docker' -Arguments @('exec', $script:LocalStackContainer, 'awslocal', 's3api', 'get-object', '--bucket', $script:ArtifactBucket, '--key', $Key, $remotePath) | Out-Null
        $raw = Invoke-Tool -File 'docker' -Arguments @('exec', $script:LocalStackContainer, 'cat', $remotePath)
        Invoke-Tool -File 'docker' -Arguments @('exec', $script:LocalStackContainer, 'rm', '-f', $remotePath) -AllowedExitCodes @(0, 1) | Out-Null
        if ([string]::IsNullOrWhiteSpace($raw)) { return $null }
        return $raw | ConvertFrom-Json
    } catch {
        try { Invoke-Tool -File 'docker' -Arguments @('exec', $script:LocalStackContainer, 'rm', '-f', $remotePath) -AllowedExitCodes @(0, 1) | Out-Null } catch {}
        return $null
    }
}

function Wait-DestroyApply {
    param([string]$StackName, [string]$PlanUID)
    $script:ObservedDestroyApplyUID = ''
    return Wait-Until -Name 'Terraform Destroy Apply' -TimeoutSeconds 900 -Condition {
        $runs = Get-KubeJson -Arguments @('get', 'terraformruns', '-n', 'platform-system')
        $apply = @($runs.items) | Where-Object { $_.spec.stackRef.name -eq $StackName -and $_.spec.operation -eq 'Apply' -and $_.spec.planRunUID -eq $PlanUID } | Sort-Object { $_.metadata.creationTimestamp } -Descending | Select-Object -First 1
        if ($apply) {
            $script:ObservedDestroyApplyUID = [string]$apply.metadata.uid
            if ($apply.status.executionOutcome -eq 'Succeeded') { return $apply }
            if ($apply.status.executionOutcome -in @('Failed', 'Indeterminate', 'Rejected')) { throw "Terraform Destroy Apply failed: $($apply.status.executionOutcome) $($apply.status.conditions | ConvertTo-Json -Compress)" }
        }
        if ($script:ObservedDestroyApplyUID) {
            $terminal = Read-ArtifactJson -Key ("runs/{0}/terminal-result.json" -f $script:ObservedDestroyApplyUID)
            if ($terminal) {
                if ($terminal.executionOutcome -eq 'Succeeded') { return [pscustomobject]@{ status = [pscustomobject]@{ executionOutcome = 'Succeeded' }; terminalResult = $terminal } }
                if ($terminal.executionOutcome -in @('Failed', 'Indeterminate', 'Rejected')) { throw "Terraform Destroy terminal result failed: $($terminal.executionOutcome) $($terminal.error)" }
            }
        }
        return $null
    }
}

function Wait-PlanAndApprove {
    param([string]$StackName, [string]$ApprovalName, [string]$PlanMode = 'Reconcile', [string]$ExcludeName = '')
    $plan = Wait-Plan -StackName $StackName -PlanMode $PlanMode -ExcludeName $ExcludeName
    if ($plan.status.executionOutcome -eq 'NoChange') { return [pscustomobject]@{ Plan = $plan; Approval = $null; Apply = $null } }
    $approval = New-Approval -Plan $plan -Name $ApprovalName
    $apply = Wait-Apply -StackName $StackName -PlanUID $plan.metadata.uid
    return [pscustomobject]@{ Plan = $plan; Approval = $approval; Apply = $apply }
}

function Assert-TargetService {
    param([string]$Context, [string]$Name, [bool]$Present)
    $exists = $true
    try { Invoke-Kubectl -Kubeconfig $(if ($Context -eq $script:TargetAContext) { $script:TargetKubeconfigA } else { $script:TargetKubeconfigB }) -Arguments @('get', 'service', $Name, '-n', 'default') | Out-Null } catch { $exists = $false }
    if ($exists -ne $Present) { throw "target service $Name on $Context presence=$exists expected=$Present" }
}

function Wait-TargetService {
    param([string]$Context, [string]$Name, [bool]$Present, [int]$TimeoutSeconds = 120)
    $targetContext = $Context
    $serviceName = $Name
    $expectedPresence = $Present
    Wait-Until -Name "target service $serviceName on $targetContext" -TimeoutSeconds $TimeoutSeconds -Condition {
        try { Assert-TargetService -Context $targetContext -Name $serviceName -Present $expectedPresence; return $true } catch { return $null }
    } | Out-Null
}

function Assert-LocalBucket {
    param([string]$Bucket, [bool]$Present)
    $code = 0
    $text = Invoke-Tool -File 'docker' -Arguments @('exec', $script:LocalStackContainer, 'awslocal', 's3api', 'head-bucket', '--bucket', $Bucket) -AllowedExitCodes @(0, 1, 2, 255)
    $code = $LASTEXITCODE
    if (($code -eq 0) -ne $Present) { throw "LocalStack bucket $Bucket presence=$($code -eq 0) expected=$Present`n$text" }
}

function Restart-Manager {
    param([string]$ActivePlanName = '')
    if ($ActivePlanName) {
        $job = Get-KubeJson -Arguments @('get', 'job', $ActivePlanName, '-n', 'platform-system')
        if ($job.status.active -ne 1) { throw "Terraform Plan Job $ActivePlanName is not active at manager restart" }
    }
    $leases = Get-KubeJson -Arguments @('get', 'leases', '-n', 'platform-system')
    $leaderLease = @($leases.items | Where-Object { $_.metadata.name -eq 'platform-control-plane.platform.example.io' }) | Select-Object -First 1
    if (-not $leaderLease -or -not $leaderLease.spec.holderIdentity) { throw 'manager leader lease not found for restart recovery' }
    $previousHolder = [string]$leaderLease.spec.holderIdentity
    $pod = ($previousHolder -split '_', 2)[0]
    if ($pod -notlike 'platform-control-plane-*') { throw "unexpected manager leader identity: $previousHolder" }
    if ($ActivePlanName) { Write-Stage "Restarting leader manager Pod $pod while Terraform Plan Job $ActivePlanName is active" }
    else { Write-Stage "Restarting leader manager Pod $pod after EnvironmentReady to verify reconstruction" }
    Invoke-Kubectl -Arguments @('delete', 'pod', $pod, '-n', 'platform-system', '--wait=false') | Out-Null
    Wait-Until -Name 'manager restart recovery' -TimeoutSeconds 180 -Condition {
        $currentLease = Get-KubeJson -Arguments @('get', 'lease', 'platform-control-plane.platform.example.io', '-n', 'platform-system')
        $deployment = Get-KubeJson -Arguments @('get', 'deployment', 'platform-control-plane', '-n', 'platform-system')
        if ($currentLease.spec.holderIdentity -and $currentLease.spec.holderIdentity -ne $previousHolder -and $deployment.status.readyReplicas -eq 2 -and $deployment.status.availableReplicas -eq 2) { return $deployment }
        return $null
    } | Out-Null
}

function Delete-EnvironmentLifecycle {
    param([string]$EnvironmentName, [string]$StackName, [string]$Bucket, [string]$RuntimeName, [string]$TargetContext = $script:TargetAContext)
    Write-Stage "Deleting $EnvironmentName and completing destroy lifecycle"
    Invoke-Kubectl -Arguments @('delete', 'platformenvironment', $EnvironmentName, '-n', 'platform-system', '--wait=false') | Out-Null
    Write-Stage "Waiting for ResourceSet prune $RuntimeName"
    Wait-Until -Name "ResourceSet prune $RuntimeName" -TimeoutSeconds 420 -Condition { try { Assert-TargetService -Context $TargetContext -Name $RuntimeName -Present $false; return $true } catch { return $null } } | Out-Null
    Write-Stage "Waiting for Terraform Destroy Plan"
    $destroy = Wait-Plan -StackName $StackName -PlanMode 'Destroy'
    if ($destroy.status.executionOutcome -eq 'ChangesPresent') {
        Write-Stage "Approving and waiting for Terraform Destroy Apply"
        $approval = New-Approval -Plan $destroy -Name "$EnvironmentName-destroy-approval"
        $apply = Wait-DestroyApply -StackName $StackName -PlanUID $destroy.metadata.uid
        if ($apply.status.executionOutcome -ne 'Succeeded') { throw "destroy Apply not succeeded" }
    }
    Write-Stage "Waiting for LocalStack managed bucket removal"
    Wait-Until -Name "LocalStack destroy $Bucket" -TimeoutSeconds 600 -Condition { try { Assert-LocalBucket -Bucket $Bucket -Present $false; return $true } catch { return $null } } | Out-Null
    Write-Stage "Waiting for PlatformEnvironment finalizer removal"
    Wait-Until -Name "environment finalizer $EnvironmentName" -TimeoutSeconds 300 -Condition { $exists = $true; try { Get-KubeJson -Arguments @('get', 'platformenvironment', $EnvironmentName, '-n', 'platform-system') | Out-Null } catch { $exists = $false }; if (-not $exists) { return $true }; return $null } | Out-Null
}

function Invoke-FullLifecycle {
    param([switch]$DoRecovery)
    $suffix = $script:RunId.ToLower().Replace('-', '')
    $envName = "stage1-$suffix"
    $className = "stage1-class-$suffix"
    $updatedClassName = "stage1-class-v2-$suffix"
    $runtimeName = "stage1-svc-$suffix"
    $backendName = "stage1-backend-$suffix"
    $bucket = "pcp-e2e-$($suffix.Substring(0, [Math]::Min(45, $suffix.Length)))"
    $script:ArtifactPrefix = "stage1/$suffix"
    $script:OwnedBuckets.Add($bucket)
    New-BackendConfig -Name $backendName -Key "$($script:ArtifactPrefix)/$envName.tfstate"
    $script:SourceA = New-GitSource -TargetContext $script:TargetAContext -BucketName $bucket -Name 'source-a'
    Start-SourceServer
    New-EnvironmentClass -Name $className -Source $script:SourceA -BackendConfigName $backendName -RuntimeName $runtimeName
    New-EnvironmentClass -Name $updatedClassName -Source $script:SourceA -BackendConfigName $backendName -RuntimeName $runtimeName -RuntimeRevision 'v2'
    New-Environment -Name $envName -ClassName $className
    $stack = Wait-Until -Name 'InfraStack creation' -TimeoutSeconds 180 -Condition { $s = Get-StackForEnvironment -EnvironmentName $envName; if ($s) { return $s }; return $null }
    if ($stack.spec.source.revision -ne $script:SourceA.Revision) { throw 'InfraStack did not resolve EnvironmentClass source revision' }
    if ($DoRecovery) {
        $activePlan = Wait-Until -Name 'active Terraform Plan Job' -TimeoutSeconds 180 -Condition {
            $candidate = Get-PlanForStack -StackName $stack.metadata.name -PlanMode 'Reconcile'
            if (-not $candidate) { return $null }
            $job = Get-KubeJson -Arguments @('get', 'job', $candidate.metadata.name, '-n', 'platform-system')
            if ($job.status.active -eq 1) { return $candidate }
            return $null
        }
        Restart-Manager -ActivePlanName $activePlan.metadata.name
    }
    $initialPlan = Wait-Plan -StackName $stack.metadata.name
    if ($initialPlan.status.executionOutcome -ne 'ChangesPresent') { throw "expected initial ChangesPresent, got $($initialPlan.status.executionOutcome)" }
    if ($DoRecovery) {
        $jobs = Get-KubeJson -Arguments @('get', 'jobs', '-n', 'platform-system', '-l', "platform.example.io/terraform-run-uid=$($initialPlan.metadata.uid)")
        if (@($jobs.items).Count -ne 1) { throw "manager restart created duplicate Plan Jobs for $($initialPlan.metadata.name)" }
    }
    $applyCountBeforeApproval = @(Get-KubeJson -Arguments @('get', 'terraformruns', '-n', 'platform-system') | Select-Object -ExpandProperty items | Where-Object { $_.spec.stackRef.name -eq $stack.metadata.name -and $_.spec.operation -eq 'Apply' }).Count
    if ($applyCountBeforeApproval -ne 0) { throw 'Apply was created before ChangeApproval' }
    $approval = New-Approval -Plan $initialPlan -Name "$envName-approval"
    $apply = Wait-Apply -StackName $stack.metadata.name -PlanUID $initialPlan.metadata.uid
    if ($apply.spec.planRef -ne $initialPlan.status.planRef -or $apply.spec.planDigest -ne $initialPlan.status.planDigest) { throw 'Apply did not retain exact saved plan binding' }
    Wait-Condition -Resource 'infrastack' -Name $stack.metadata.name -Reason 'InfrastructureReady' -TimeoutSeconds 900 | Out-Null
    Assert-LocalBucket -Bucket $bucket -Present $true
    Wait-TargetService -Context $script:TargetAContext -Name $runtimeName -Present $true
    Wait-Condition -Resource 'resourceset' -Name "$envName-resources" -Reason 'RuntimeReady' -TimeoutSeconds 300 | Out-Null
    Wait-Condition -Resource 'platformenvironment' -Name $envName -Reason 'EnvironmentReady' -TimeoutSeconds 300 | Out-Null
    Add-Result -Name 'FULL_LIFECYCLE' -Status 'PASS' -Evidence "create=$envName plan=$($initialPlan.status.executionOutcome) apply=$($apply.status.executionOutcome) runtime=$runtimeName bucket=$bucket"
    if ($DoRecovery) {
        $readyStack = Get-KubeJson -Arguments @('get', 'infrastack', $stack.metadata.name, '-n', 'platform-system')
        $readySet = Get-KubeJson -Arguments @('get', 'resourceset', "$envName-resources", '-n', 'platform-system')
        $discoveryDigest = [string]$readyStack.status.targetDiscoveryDigest
        $inventoryUID = [string](@($readySet.status.inventory)[0].uid)
        if (-not $discoveryDigest -or -not $inventoryUID) { throw 'Ready environment lacks discovery or runtime inventory evidence before manager restart' }
        Restart-Manager
        Wait-Condition -Resource 'platformenvironment' -Name $envName -Reason 'EnvironmentReady' -TimeoutSeconds 300 | Out-Null
        $recoveredStack = Get-KubeJson -Arguments @('get', 'infrastack', $stack.metadata.name, '-n', 'platform-system')
        $recoveredSet = Get-KubeJson -Arguments @('get', 'resourceset', "$envName-resources", '-n', 'platform-system')
        if ($recoveredStack.status.targetDiscoveryDigest -ne $discoveryDigest -or [string](@($recoveredSet.status.inventory)[0].uid) -ne $inventoryUID) { throw 'manager restart changed trusted discovery or runtime inventory' }
        $applyRuns = Get-KubeJson -Arguments @('get', 'terraformruns', '-n', 'platform-system')
        $applyCount = @($applyRuns.items | Where-Object { $_.spec.stackRef.name -eq $stack.metadata.name -and $_.spec.operation -eq 'Apply' }).Count
        if ($applyCount -ne 1) { throw "manager restart caused duplicate Apply count=$applyCount" }
        Get-KubeJson -Arguments @('get', 'changeapproval', "$envName-approval", '-n', 'platform-system') | Out-Null
        Assert-TargetService -Context $script:TargetAContext -Name $runtimeName -Present $true
        Add-Result -Name 'RESTART_RECOVERY' -Status 'PASS' -Evidence 'leader restarted during active Plan and after Ready; one Plan Job, one Apply, stable discovery/inventory/approval/runtime'
    }
    $beforeService = Get-KubeJson -Kubeconfig $script:TargetKubeconfigA -Arguments @('get', 'service', $runtimeName, '-n', 'default')
    $beforeSet = Get-KubeJson -Arguments @('get', 'resourceset', "$envName-resources", '-n', 'platform-system')
    $classPatch = @{ spec = @{ classRef = @{ name = $updatedClassName } } } | ConvertTo-Json -Depth 10 -Compress
    $classPatchPath = Join-Path $script:StateRoot 'platformenvironment-class-update-patch.json'
    $classPatch | Set-Content -Path $classPatchPath -Encoding UTF8
    $script:TempPaths.Add($classPatchPath)
    Invoke-Kubectl -Arguments @('patch', 'platformenvironment', $envName, '-n', 'platform-system', '--type=merge', '--patch-file', $classPatchPath) | Out-Null
    Wait-Until -Name 'target Runtime Service v2 SSA update' -TimeoutSeconds 300 -Condition {
        $service = Get-KubeJson -Kubeconfig $script:TargetKubeconfigA -Arguments @('get', 'service', $runtimeName, '-n', 'default')
        if ($service.metadata.annotations.'platform.example.io/e2e-revision' -eq 'v2') { return $service }
        return $null
    } | Out-Null
    Wait-Condition -Resource 'resourceset' -Name "$envName-resources" -Reason 'RuntimeReady' -TimeoutSeconds 300 | Out-Null
    Wait-Condition -Resource 'platformenvironment' -Name $envName -Reason 'EnvironmentReady' -TimeoutSeconds 300 | Out-Null
    $targetService = Get-KubeJson -Kubeconfig $script:TargetKubeconfigA -Arguments @('get', 'service', $runtimeName, '-n', 'default')
    $afterSet = Get-KubeJson -Arguments @('get', 'resourceset', "$envName-resources", '-n', 'platform-system')
    if ($targetService.metadata.uid -ne $beforeService.metadata.uid -or [string](@($afterSet.status.inventory)[0].uid) -ne [string](@($beforeSet.status.inventory)[0].uid)) { throw 'runtime v2 update replaced the Service or inventory identity' }
    Assert-LocalBucket -Bucket $bucket -Present $true
    $runsAfterRuntimeUpdate = Get-KubeJson -Arguments @('get', 'terraformruns', '-n', 'platform-system')
    $applyCountAfterRuntimeUpdate = @($runsAfterRuntimeUpdate.items | Where-Object { $_.spec.stackRef.name -eq $stack.metadata.name -and $_.spec.operation -eq 'Apply' }).Count
    if ($applyCountAfterRuntimeUpdate -ne 1) { throw "runtime-only update created unexpected Terraform Apply count=$applyCountAfterRuntimeUpdate" }
    Add-Result -Name 'UPDATE_RUNTIME' -Status 'PASS' -Evidence 'v2 class changed target Service annotation in place; Service/inventory UID stable; LocalStack bucket retained; no extra Apply'
    $planBeforeCapacityUpdate = Get-PlanForStack -StackName $stack.metadata.name
    $excludePlanName = if ($planBeforeCapacityUpdate) { $planBeforeCapacityUpdate.metadata.name } else { $initialPlan.metadata.name }
    $patch = @{ spec = @{ capacity = @{ nodeCount = 2 } } } | ConvertTo-Json -Depth 10 -Compress
    $patchPath = Join-Path $script:StateRoot 'platformenvironment-update-patch.json'
    $patch | Set-Content -Path $patchPath -Encoding UTF8
    $script:TempPaths.Add($patchPath)
    Invoke-Kubectl -Arguments @('patch', 'platformenvironment', $envName, '-n', 'platform-system', '--type=merge', '--patch-file', $patchPath) | Out-Null
    $updatedPlan = Wait-Plan -StackName $stack.metadata.name -ExcludeName $excludePlanName
    if ($updatedPlan.status.executionOutcome -eq 'ChangesPresent') { throw 'safe capacity update unexpectedly required Apply for fixture with no capacity variable' }
    if ($updatedPlan.status.executionOutcome -ne 'NoChange') { throw "safe update expected NoChange, got $($updatedPlan.status.executionOutcome)" }
    $applyCountAfterUpdate = @(Get-KubeJson -Arguments @('get', 'terraformruns', '-n', 'platform-system') | Select-Object -ExpandProperty items | Where-Object { $_.spec.stackRef.name -eq $stack.metadata.name -and $_.spec.operation -eq 'Apply' }).Count
    if ($applyCountAfterUpdate -ne 1) { throw "NoChange update created an unexpected Apply count=$applyCountAfterUpdate" }
    Wait-Condition -Resource 'platformenvironment' -Name $envName -Reason 'EnvironmentReady' -TimeoutSeconds 300 | Out-Null
    Delete-EnvironmentLifecycle -EnvironmentName $envName -StackName $stack.metadata.name -Bucket $bucket -RuntimeName $runtimeName
    Add-Result -Name 'UPDATE_DELETE' -Status 'PASS' -Evidence "runtime updated in place, capacity reconciliation=NoChange, destroy=completed bucket=$bucket"
}

function Invoke-MultiTarget {
    $suffix = $script:RunId.ToLower().Replace('-', '')
    $bucket = "pcp-e2e-multi-$($suffix.Substring(0, [Math]::Min(38, $suffix.Length)))"
    $script:OwnedBuckets.Add("$bucket-a")
    $script:OwnedBuckets.Add("$bucket-b")
    $backendA = "multi-backend-a-$($suffix.Substring(0, 12))"; $backendB = "multi-backend-b-$($suffix.Substring(0, 12))"
    New-BackendConfig -Name $backendA -Key "multi/$suffix/a.tfstate"; New-BackendConfig -Name $backendB -Key "multi/$suffix/b.tfstate"
    $script:SourceA = New-GitSource -TargetContext $script:TargetAContext -BucketName "$bucket-a" -Name 'multi-a'
    $script:SourceB = New-GitSource -TargetContext $script:TargetBContext -BucketName "$bucket-b" -Name 'multi-b'
    Start-SourceServer
    $classA = "multi-class-a-$($suffix.Substring(0, 12))"; $classB = "multi-class-b-$($suffix.Substring(0, 12))"
    $envA = "multi-env-a-$($suffix.Substring(0, 12))"; $envB = "multi-env-b-$($suffix.Substring(0, 12))"
    $svcA = "multi-svc-a-$($suffix.Substring(0, 12))"; $svcB = "multi-svc-b-$($suffix.Substring(0, 12))"
    New-EnvironmentClass -Name $classA -Source $script:SourceA -BackendConfigName $backendA -RuntimeName $svcA -RunnerServiceAccountName "pcp-runner-a-$($suffix.Substring(0, 12))"
    New-EnvironmentClass -Name $classB -Source $script:SourceB -BackendConfigName $backendB -RuntimeName $svcB -RunnerServiceAccountName "pcp-runner-b-$($suffix.Substring(0, 12))"
    New-Environment -Name $envA -ClassName $classA; New-Environment -Name $envB -ClassName $classB
    $stackA = Wait-Until -Name 'multi target A stack' -TimeoutSeconds 240 -Condition { $s = Get-StackForEnvironment -EnvironmentName $envA; if ($s) { return $s }; return $null }
    $stackB = Wait-Until -Name 'multi target B stack' -TimeoutSeconds 240 -Condition { $s = Get-StackForEnvironment -EnvironmentName $envB; if ($s) { return $s }; return $null }
    $planA = Wait-PlanAndApprove -StackName $stackA.metadata.name -ApprovalName "$envA-approval"; $planB = Wait-PlanAndApprove -StackName $stackB.metadata.name -ApprovalName "$envB-approval"
    Wait-Condition -Resource 'platformenvironment' -Name $envA -Reason 'EnvironmentReady' -TimeoutSeconds 900 | Out-Null; Wait-Condition -Resource 'platformenvironment' -Name $envB -Reason 'EnvironmentReady' -TimeoutSeconds 900 | Out-Null
    Wait-TargetService -Context $script:TargetAContext -Name $svcA -Present $true; Wait-TargetService -Context $script:TargetBContext -Name $svcB -Present $true
    Assert-TargetService -Context $script:TargetBContext -Name $svcA -Present $false; Assert-TargetService -Context $script:TargetAContext -Name $svcB -Present $false
    Add-Result -Name 'MULTI_TARGET' -Status 'PASS' -Evidence "A=$($script:TargetAContext)/$svcA B=$($script:TargetBContext)/$svcB"
    Delete-EnvironmentLifecycle -EnvironmentName $envA -StackName $stackA.metadata.name -Bucket "$bucket-a" -RuntimeName $svcA -TargetContext $script:TargetAContext
    Delete-EnvironmentLifecycle -EnvironmentName $envB -StackName $stackB.metadata.name -Bucket "$bucket-b" -RuntimeName $svcB -TargetContext $script:TargetBContext
}

function New-FailClosedResourceSet {
    param([string]$Name, [hashtable]$Target, [hashtable]$Identity, [hashtable]$Profile, [bool]$IncludeDiscovery)
    $runtime = [ordered]@{ version = 'v1'; resource = 'services'; readinessPolicy = 'RequireReady'; deletionPolicy = 'Prune'; ownershipID = $Name; object = (New-RuntimeServiceObject -Name "$Name-side-effect") }
    $spec = [ordered]@{ target = $Target; trustedRuntimeTargetIdentity = $Identity; trustedTargetConnectionProfile = $Profile; runtimeMutationAllowed = $true; mutationFence = $false; resources = @($runtime) }
    if ($IncludeDiscovery) { $spec.targetDiscoveryRef = 'e2e/missing-discovery'; $spec.targetDiscoveryDigest = 'sha256:missing' }
    Write-KubeObject -Object ([ordered]@{ apiVersion = 'platform.example.io/v1alpha1'; kind = 'ResourceSet'; metadata = [ordered]@{ name = $Name; namespace = 'platform-system' }; spec = $spec })
}

function Invoke-FailClosed {
    $wrongName = "failclosed-wrong-$($script:RunId.ToLower().Replace('-', '').Substring(0, 16))"
    $target = @{ provider = 'kind'; account = 'local'; region = 'local'; clusterName = 'kind-unregistered-target'; clusterID = 'kind-unregistered-target'; incarnationID = 'kind-unregistered-target' }
    $identity = @{ provider = 'kind'; accountID = 'local'; region = 'local'; clusterName = 'kind-unregistered-target'; incarnationID = 'kind-unregistered-target' }
    $profile = @{ endpoint = 'kubeconfig:kind-unregistered-target'; authMode = 'kind-context'; kubeContext = 'kind-unregistered-target'; networkRouteProfile = 'local-kind' }
    New-FailClosedResourceSet -Name $wrongName -Target $target -Identity $identity -Profile $profile -IncludeDiscovery $true
    $wrong = Wait-Until -Name 'wrong target fail closed' -TimeoutSeconds 180 -Condition { $o = Get-KubeJson -Arguments @('get', 'resourceset', $wrongName, '-n', 'platform-system'); $c = Get-Condition $o; if ($c -and $c.reason -eq 'RuntimeTargetRejected') { return $o }; return $null }
    Assert-TargetService -Context $script:TargetAContext -Name "$wrongName-side-effect" -Present $false; Assert-TargetService -Context $script:TargetBContext -Name "$wrongName-side-effect" -Present $false
    Remove-KubeObject -Arguments @('delete', 'resourceset', $wrongName, '-n', 'platform-system')
    $invalidName = "failclosed-invalid-$($script:RunId.ToLower().Replace('-', '').Substring(0, 16))"
    $target.clusterName = $script:TargetAContext; $target.clusterID = $script:TargetAContext; $target.incarnationID = $script:TargetAContext
    $identity.clusterName = $script:TargetAContext; $identity.incarnationID = $script:TargetAContext
    $profile.endpoint = "kubeconfig:$($script:TargetAContext)"; $profile.kubeContext = $script:TargetAContext
    New-FailClosedResourceSet -Name $invalidName -Target $target -Identity $identity -Profile $profile -IncludeDiscovery $false
    $invalid = Wait-Until -Name 'invalid discovery fail closed' -TimeoutSeconds 180 -Condition { $o = Get-KubeJson -Arguments @('get', 'resourceset', $invalidName, '-n', 'platform-system'); $c = Get-Condition $o; if ($c -and $c.reason -eq 'RuntimeTargetRejected') { return $o }; return $null }
    Assert-TargetService -Context $script:TargetAContext -Name "$invalidName-side-effect" -Present $false; Assert-TargetService -Context $script:TargetBContext -Name "$invalidName-side-effect" -Present $false
    Remove-KubeObject -Arguments @('delete', 'resourceset', $invalidName, '-n', 'platform-system')
    Add-Result -Name 'FAIL_CLOSED' -Status 'PASS' -Evidence 'unapproved Plan, unregistered target, and incomplete trusted discovery produced no runtime mutation'
}

function Save-Snapshot {
    try { Invoke-Kubectl -Arguments @('get', 'platformenvironments,infrastacks,terraformruns,resourcesets,changeapprovals', '-A', '-o', 'wide') -LogName 'management-resources.txt' | Out-Null } catch {}
    try { Invoke-Kubectl -Arguments @('get', 'pods,jobs', '-n', 'platform-system', '-o', 'wide') -LogName 'management-workloads.txt' | Out-Null } catch {}
    try { Invoke-Kubectl -Kubeconfig $script:TargetKubeconfigA -Arguments @('get', 'all', '-A', '-o', 'wide') -LogName 'target-a-resources.txt' | Out-Null } catch {}
    try { Invoke-Kubectl -Kubeconfig $script:TargetKubeconfigB -Arguments @('get', 'all', '-A', '-o', 'wide') -LogName 'target-b-resources.txt' | Out-Null } catch {}
    try { Invoke-Kubectl -Arguments @('logs', 'deployment/platform-control-plane', '-n', 'platform-system', '--all-containers=true', '--tail=300') -LogName 'manager.log' | Out-Null } catch {}
}

function Cleanup {
    Write-Stage 'Cleaning only resources owned by this E2E run'
    foreach ($process in $script:StartedProcesses) { try { if (-not $process.HasExited) { Stop-Process -Id $process.Id -Force } } catch {} }
    foreach ($bucket in $script:OwnedBuckets) { try { Invoke-Tool -File 'docker' -Arguments @('exec', $script:LocalStackContainer, 'awslocal', 's3', 'rb', "s3://$bucket", '--force') -AllowedExitCodes @(0, 1, 2) | Out-Null } catch {} }
    foreach ($cluster in $script:OwnedClusters) { try { Invoke-Tool -File 'kind' -Arguments @('delete', 'cluster', '--name', $cluster) -AllowedExitCodes @(0, 1) | Out-Null } catch {} }
    if ($script:OriginalGitImageID) { try { Invoke-Tool -File 'docker' -Arguments @('tag', $script:OriginalGitImageID, 'alpine/git:2.45.2') | Out-Null } catch {} }
    if ($script:OwnedGitImage) { try { Invoke-Tool -File 'docker' -Arguments @('image', 'rm', $script:OwnedGitImage) -AllowedExitCodes @(0, 1) | Out-Null } catch {} }
    if (-not $KeepArtifacts -and (Test-Path $script:StateRoot)) { Remove-Item -LiteralPath $script:StateRoot -Recurse -Force -ErrorAction SilentlyContinue }
}

function Verify-Cleanup {
    foreach ($process in $script:StartedProcesses) {
        $process.Refresh()
        if (-not $process.HasExited) { throw "E2E temporary process remains: $($process.Id)" }
    }
    if ($script:OwnedClusters.Count -gt 0) {
        $clusters = Invoke-Tool -File 'kind' -Arguments @('get', 'clusters')
        $remaining = @($clusters -split "`r?`n" | Where-Object { $script:OwnedClusters.Contains($_) })
        if ($remaining.Count -gt 0) { throw "E2E Kind clusters remain: $($remaining -join ', ')" }
    }
    if ($script:OwnedBuckets.Count -gt 0 -and $script:LocalStackContainer) {
        $buckets = Invoke-Tool -File 'docker' -Arguments @('exec', $script:LocalStackContainer, 'awslocal', 's3api', 'list-buckets', '--query', 'Buckets[].Name', '--output', 'text')
        $remaining = @($script:OwnedBuckets | Where-Object { $buckets -split '\s+' -contains $_ })
        if ($remaining.Count -gt 0) { throw "E2E LocalStack buckets remain: $($remaining -join ', ')" }
    }
    if (-not $KeepArtifacts -and (Test-Path $script:StateRoot)) { throw "E2E temporary state remains: $($script:StateRoot)" }
}

function Write-Summary {
    $summary = @(
        "run_id=$($script:RunId)",
        "suite=$Suite",
        "localstack_endpoint=$LocalStackEndpoint",
        "real_aws_used=false",
        "results=",
        ($script:Results | ForEach-Object { "$($_.Name)=$($_.Status) $($_.Evidence)" })
    )
    $summary | Set-Content (Join-Path $script:ArtifactRoot 'summary.txt') -Encoding UTF8
}

function Invoke-Clean {
    if (Get-Command kind -ErrorAction SilentlyContinue) {
        $clusters = Invoke-Tool -File 'kind' -Arguments @('get', 'clusters')
        foreach ($cluster in @($clusters -split "`r?`n" | Where-Object { $_ -like 'pcp-e2e-*' -and $_ })) {
            $script:OwnedClusters.Add($cluster)
            Invoke-Tool -File 'kind' -Arguments @('delete', 'cluster', '--name', $cluster) -AllowedExitCodes @(0, 1) | Out-Null
        }
    }
}

try {
    if ($Suite -eq 'clean') {
        Invoke-Clean
    } else {
        Test-Dependencies -RequireDocker
        Test-LocalStack
        New-Clusters
        Build-Images
        $artifactSuffix = $script:RunId.ToLower().Replace('-', '')
        $script:ArtifactBucket = "pcp-e2e-artifacts-$($artifactSuffix.Substring(0, [Math]::Min(32, $artifactSuffix.Length)))"
        $script:OwnedBuckets.Add($script:ArtifactBucket)
        Invoke-Tool -File 'docker' -Arguments @('exec', $script:LocalStackContainer, 'awslocal', 's3api', 'create-bucket', '--bucket', $script:ArtifactBucket) | Out-Null
        Install-Manager
        if ($Suite -in @('all', 'lifecycle', 'recovery')) { Invoke-FullLifecycle -DoRecovery:($Suite -in @('all', 'recovery')) }
        if ($Suite -in @('all', 'multitarget')) { Invoke-MultiTarget }
        if ($Suite -in @('all', 'failclosed')) { Invoke-FailClosed }
        $script:SuiteCompleted = $true
    }
} catch {
    Write-Host $_.Exception.Message -ForegroundColor Red
    Add-Result -Name 'STAGE1_E2E' -Status 'FAIL' -Evidence $_.Exception.Message
    Save-Snapshot
    $script:ExitCode = 1
} finally {
    Cleanup
    try {
        Verify-Cleanup
        Add-Result -Name 'CLEANUP' -Status 'PASS' -Evidence 'owned Kind clusters, LocalStack buckets, and temporary state verified absent'
    } catch {
        Add-Result -Name 'CLEANUP' -Status 'FAIL' -Evidence $_.Exception.Message
        $script:ExitCode = 1
    }
    if ($script:SuiteCompleted) {
        if ($script:ExitCode -eq 0) {
            Add-Result -Name 'STAGE1_E2E' -Status 'PASS' -Evidence 'all requested local Stage 1 lanes and cleanup completed'
        } else {
            Add-Result -Name 'STAGE1_E2E' -Status 'FAIL' -Evidence 'cleanup verification failed'
        }
    }
    Write-Summary
}

if ($script:Results | Where-Object { $_.Status -ne 'PASS' }) { exit 1 }
exit 0
