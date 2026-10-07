[CmdletBinding()]
param(
    [ValidateSet('all', 'lifecycle', 'recovery', 'multitarget', 'failclosed', 'platform', 'browser', 'clean')]
    [string]$Suite = 'all',
    [string]$LocalStackEndpoint = $(if ($env:PCP_LOCALSTACK_ENDPOINT) { $env:PCP_LOCALSTACK_ENDPOINT } else { 'http://localhost:4566' }),
    [switch]$KeepArtifacts,
    [Alias('Stage2Session')][switch]$BrowserSession,
    [ValidateSet('deterministic', 'ollama')][string]$AIProvider = 'deterministic',
    [string]$OllamaEndpoint = 'http://127.0.0.1:11434',
    [string]$OllamaModel = 'phi4-mini:latest',
    [ValidateRange(1, 60)][int]$PollSeconds = 2,
    [ValidateRange(30, 3600)][int]$WaitTimeoutSeconds = 900,
    [ValidateRange(30, 86400)][int]$BrowserSessionTimeoutSeconds = 3600,
    [ValidateRange(0, 65535)][int]$ApiPort = 0,
    [ValidateRange(1, 16)][int]$BuildParallelism = 2,
    [string]$TerraformDnsServer = '',
    [switch]$RequireExistingLocalStack,
    [string]$LocalStackImage = 'localstack/localstack:4.14.0'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if ($AIProvider -eq 'ollama') {
    if ([string]::IsNullOrWhiteSpace($OllamaEndpoint)) { throw '-OllamaEndpoint is required when -AIProvider ollama is selected' }
    $parsedOllamaEndpoint = $null
    if (-not [Uri]::TryCreate($OllamaEndpoint.Trim(), [UriKind]::Absolute, [ref]$parsedOllamaEndpoint)) {
        throw "Invalid -OllamaEndpoint '$OllamaEndpoint'; expected a loopback HTTP URL with an explicit port"
    }
    $explicitPort = if ($parsedOllamaEndpoint.HostNameType -eq [UriHostNameType]::IPv6) {
        $parsedOllamaEndpoint.Authority -match '^\[[^\]]+\]:\d+$'
    } else {
        $parsedOllamaEndpoint.Authority -match '^[^:]+:\d+$'
    }
    if ($parsedOllamaEndpoint.Scheme -ne 'http' -or -not $parsedOllamaEndpoint.IsLoopback -or -not $explicitPort -or
        $parsedOllamaEndpoint.UserInfo -or $parsedOllamaEndpoint.Query -or $parsedOllamaEndpoint.Fragment -or
        $parsedOllamaEndpoint.AbsolutePath -notin @('', '/')) {
        throw "Invalid -OllamaEndpoint '$OllamaEndpoint'; expected a loopback HTTP URL with an explicit port and no path, credentials, query, or fragment"
    }
    if ([string]::IsNullOrWhiteSpace($OllamaModel)) { throw '-OllamaModel is required when -AIProvider ollama is selected' }
    $OllamaEndpoint = $parsedOllamaEndpoint.GetLeftPart([UriPartial]::Authority)
    $OllamaModel = $OllamaModel.Trim()
}

$script:RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$script:RunId = ('{0}-{1}' -f (Get-Date).ToUniversalTime().ToString('yyyyMMddHHmmss'), ([guid]::NewGuid().ToString('N').Substring(0, 8)))
$script:StateRoot = Join-Path $script:RepoRoot ('tmp/pcp-local-e2e-' + $script:RunId)
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
$script:ImageObservations = @{}
$script:RunTimer = [Diagnostics.Stopwatch]::StartNew()
$script:SuiteDurations = [ordered]@{}
$script:OwnedLocalStack = $false
$script:ApiProcess = $null
$script:ApiBase = ''
$script:AIProvider = $AIProvider
$script:OllamaEndpoint = if ($AIProvider -eq 'ollama') { $OllamaEndpoint } else { $null }
$script:OllamaModel = if ($AIProvider -eq 'ollama') { $OllamaModel } else { $null }
$script:LastObservedState = ''
$script:DiagnosticResource = $null
$script:OwnershipPath = Join-Path $script:ArtifactRoot 'ownership.json'
$script:CleanupCompleted = $false

New-Item -ItemType Directory -Force -Path $script:ArtifactRoot, $script:LogRoot, $script:StateRoot | Out-Null

function Write-Stage {
    param([string]$Message)
    Write-Host ('[STEP {0}] {1}' -f (Get-Date).ToString('HH:mm:ss'), $Message) -ForegroundColor Cyan
}

function Add-Result {
    param([string]$Name, [ValidateSet('PASS', 'FAIL', 'BLOCKED_LOCAL_ENVIRONMENT')][string]$Status, [string]$Evidence = '')
    $script:Results.Add([pscustomobject]@{ Name = $Name; Status = $Status; Evidence = $Evidence })
    Write-Host ('[{0}] {1} {2}' -f $Status, $Name, $Evidence) -ForegroundColor $(if ($Status -eq 'PASS') { 'Green' } else { 'Yellow' })
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
    $value = $raw | ConvertFrom-Json
    $script:LastObservedState = ($value | ConvertTo-Json -Depth 12 -Compress)
    if ($Arguments.Count -ge 3 -and $Arguments[0] -eq 'get' -and $Arguments[2] -notlike '-*') {
        $script:DiagnosticResource = @{ resource = $Arguments[1]; name = $Arguments[2]; kubeconfig = $Kubeconfig }
    }
    return $value
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
    Invoke-Kubectl -Arguments ($Arguments + @('--ignore-not-found=true', '--timeout=60s')) -Kubeconfig $Kubeconfig | Out-Null
}

function Wait-Until {
    param([Parameter(Mandatory)][Alias('Name')][string]$WaitDescription, [Parameter(Mandatory)][scriptblock]$Condition,
        [int]$TimeoutSeconds = $WaitTimeoutSeconds, [int]$PollSeconds = $script:PollSeconds)
    $timer = [Diagnostics.Stopwatch]::StartNew()
    $lastError = ''
    while ($timer.Elapsed.TotalSeconds -lt $TimeoutSeconds) {
        try {
            $value = & $Condition
            if ($null -ne $value -and $value -ne $false) {
                Write-Host ('[PASS] {0} ({1:N1}s)' -f $WaitDescription, $timer.Elapsed.TotalSeconds) -ForegroundColor Green
                return $value
            }
        } catch {
            if ($_.Exception.Data['LocalE2ETerminalFailure']) { throw }
            $lastError = $_.Exception.Message
        }
        Start-Sleep -Seconds $PollSeconds
    }
    # Keep console failures readable; Save-FailureDiagnostics retains the full
    # polling state in failure.json for investigation.
    $observedSummary = [string]$script:LastObservedState
    if ($observedSummary.Length -gt 600) { $observedSummary = $observedSummary.Substring(0, 600) + '... (full state: failure.json)' }
    $errorSummary = $lastError
    if ($errorSummary.Length -gt 300) { $errorSummary = $errorSummary.Substring(0, 300) + '...' }
    throw "Timed out after ${TimeoutSeconds}s waiting for $WaitDescription. Last error: $errorSummary. Last observed state: $observedSummary"
}

function Get-Condition {
    param($Object, [string]$Type = 'Ready')
    $status = Get-Field $Object 'status'
    return @(Get-Field $status 'conditions') | Where-Object { $_ -and $_.type -eq $Type } | Select-Object -First 1
}

function Get-Field {
    param($Object, [string]$Name)
    if ($null -eq $Object) { return $null }
    $property = $Object.PSObject.Properties[$Name]
    if ($property) { return $property.Value }
    return $null
}

function Stop-Regression {
    param([string]$Message)
    $failure = New-Object System.InvalidOperationException($Message)
    $failure.Data['LocalE2ETerminalFailure'] = $true
    throw $failure
}

function Save-Proof {
    param([string]$Name, $Value)
    $Value | ConvertTo-Json -Depth 100 | Set-Content -LiteralPath (Join-Path $script:ArtifactRoot "$Name.json") -Encoding UTF8
}

function Get-FreeLocalPort {
    $listener = New-Object Net.Sockets.TcpListener([Net.IPAddress]::Loopback, 0)
    try { $listener.Start(); return $listener.LocalEndpoint.Port } finally { $listener.Stop() }
}

function Get-ContainerEndpoint {
    $uri = New-Object UriBuilder($LocalStackEndpoint)
    $uri.Host = 'host.docker.internal'
    return $uri.Uri.AbsoluteUri.TrimEnd('/')
}

function Save-Ownership {
    $harness = Get-Process -Id $PID
    $processes = @($script:StartedProcesses | ForEach-Object {
        try { @{ id = $_.Id; startedAt = $_.StartTime.ToUniversalTime().ToString('o'); path = $_.Path } } catch {}
    })
    $record = [ordered]@{ owner = 'kpcp-local-e2e'; runId = $script:RunId;
        harnessPid = $PID; harnessStartedAt = $harness.StartTime.ToUniversalTime().ToString('o'); harnessPath = $harness.Path;
        stateRoot = $script:StateRoot; clusters = @($script:OwnedClusters.ToArray());
        buckets = @($script:OwnedBuckets.ToArray()); processes = $processes;
        localStackContainer = $script:LocalStackContainer; ownsLocalStack = $script:OwnedLocalStack;
        localStackEndpoint = $LocalStackEndpoint; ownedGitImage = $script:OwnedGitImage;
        originalGitImageID = $script:OriginalGitImageID; cleanupCompleted = $script:CleanupCompleted }
    # Replace atomically; interrupted writes cannot become usable cleanup records.
    $pending = $script:OwnershipPath + '.pending'
    $record | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $pending -Encoding UTF8
    Move-Item -LiteralPath $pending -Destination $script:OwnershipPath -Force
}

function ConvertTo-ProcessStartMilliseconds {
    param([Parameter(Mandatory)]$Value)
    if ($Value -is [DateTimeOffset]) {
        $timestamp = $Value.ToUniversalTime()
    } elseif ($Value -is [DateTime]) {
        $timestamp = ([DateTimeOffset]$Value).ToUniversalTime()
    } else {
        $timestamp = [DateTimeOffset]::Parse([string]$Value, [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::RoundtripKind).ToUniversalTime()
    }
    return $timestamp.ToUnixTimeMilliseconds()
}

function Get-RecordedProcessIdentity {
    param(
        [Parameter(Mandatory)][int]$ProcessId,
        [Parameter(Mandatory)]$StartedAt,
        [string]$Path = '',
        [switch]$AllowLegacyPowerShellPath
    )
    $process = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue
    if (-not $process) { return [pscustomobject]@{ Status = 'Exited'; Process = $null; Reason = '' } }
    if ($Path) {
        try {
            if ([IO.Path]::GetFullPath($process.Path) -ine [IO.Path]::GetFullPath($Path)) {
                return [pscustomobject]@{ Status = 'DifferentProcess'; Process = $process; Reason = "executable path '$($process.Path)' does not match '$Path'" }
            }
        } catch {
            return [pscustomobject]@{ Status = 'DifferentProcess'; Process = $process; Reason = "could not verify executable path for PID ${ProcessId}: $($_.Exception.Message)" }
        }
    } elseif ($AllowLegacyPowerShellPath -and $process.Name -notin @('pwsh', 'powershell')) {
        return [pscustomobject]@{ Status = 'DifferentProcess'; Process = $process; Reason = "legacy harness PID now belongs to '$($process.Name)'" }
    } elseif (-not $AllowLegacyPowerShellPath) {
        return [pscustomobject]@{ Status = 'DifferentProcess'; Process = $process; Reason = 'ownership record has no executable path' }
    }
    try {
        $liveStart = ConvertTo-ProcessStartMilliseconds -Value $process.StartTime
        $recordedStart = ConvertTo-ProcessStartMilliseconds -Value $StartedAt
    } catch {
        return [pscustomobject]@{ Status = 'DifferentProcess'; Process = $process; Reason = "could not normalize creation time: $($_.Exception.Message)" }
    }
    # Compare at millisecond precision with a small allowance for platform serialization rounding.
    # PID and executable identity remain mandatory, so this does not turn PID reuse into ownership.
    if ([Math]::Abs([double]$liveStart - [double]$recordedStart) -gt 10) {
        return [pscustomobject]@{ Status = 'DifferentProcess'; Process = $process; Reason = 'creation time does not match the ownership record' }
    }
    return [pscustomobject]@{ Status = 'Match'; Process = $process; Reason = '' }
}

function Start-PlatformApi {
    if ($script:ApiProcess -and -not $script:ApiProcess.HasExited) { return }
    Write-Stage 'Building and starting the real local platform-api on an isolated loopback port'
    $binary = Join-Path $script:StateRoot 'platform-api.exe'
    Invoke-Tool -File 'go' -Arguments @('build', '-trimpath', '-o', $binary, './cmd/platform-api') -LogName 'platform-api-build.txt' | Out-Null
    $port = if ($ApiPort) { $ApiPort } else { Get-FreeLocalPort }
    $script:ApiBase = "http://127.0.0.1:$port"
    $values = @{ KUBECONFIG = $script:ManagementKubeconfig; PCP_KUBECONFIG = $script:ManagementKubeconfig;
        PCP_PLATFORM_API_ADDR = "127.0.0.1:$port"; PCP_ENABLE_AWS_EKS = 'false'; AI_PROVIDER = $script:AIProvider;
        PCP_ARTIFACT_ENDPOINT = $LocalStackEndpoint; PCP_ARTIFACT_REGION = 'us-east-1'; PCP_ARTIFACT_BUCKET = $script:ArtifactBucket;
        PCP_ARTIFACT_KMS_KEY_ID = ''; AWS_ACCESS_KEY_ID = 'test'; AWS_SECRET_ACCESS_KEY = 'test'; AWS_SESSION_TOKEN = '' }
    if ($script:AIProvider -eq 'ollama') {
        $values.OLLAMA_ENDPOINT = $script:OllamaEndpoint
        $values.OLLAMA_MODEL = $script:OllamaModel
    }
    $previous = @{}
    try {
        foreach ($key in $values.Keys) { $previous[$key] = [Environment]::GetEnvironmentVariable($key, 'Process'); [Environment]::SetEnvironmentVariable($key, $values[$key], 'Process') }
        $script:ApiProcess = Start-Process -FilePath $binary -WorkingDirectory $script:RepoRoot -WindowStyle Hidden -PassThru `
            -RedirectStandardOutput (Join-Path $script:LogRoot 'platform-api.stdout.log') -RedirectStandardError (Join-Path $script:LogRoot 'platform-api.stderr.log')
        $script:StartedProcesses.Add($script:ApiProcess)
        Save-Ownership
    } finally {
        foreach ($key in $previous.Keys) { [Environment]::SetEnvironmentVariable($key, $previous[$key], 'Process') }
    }
    Wait-Until -Name 'platform-api health' -TimeoutSeconds 120 -Condition {
        if ($script:ApiProcess.HasExited) { Stop-Regression 'platform-api exited during startup; inspect its stderr/stdout logs' }
        $health = Invoke-Api -Path '/api/health'
        if ($health.status -eq 'ok' -and $health.artifactPreview -and $health.aiProvider -eq $script:AIProvider) { return $health }
        return $null
    } | Out-Null
    Save-Proof -Name 'platform-api-runtime' -Value @{ apiBase = $script:ApiBase; pid = $script:ApiProcess.Id; binary = $binary; kubeconfig = $script:ManagementKubeconfig; aiProvider = $script:AIProvider; ollamaEndpoint = $script:OllamaEndpoint; ollamaModel = $script:OllamaModel }
}

function Invoke-Api {
    param([Parameter(Mandatory)][string]$Path, [string]$Method = 'GET', $Body = $null, [int]$ExpectedStatus = 200)
    $parameters = @{ Uri = $script:ApiBase + $Path; Method = $Method; UseBasicParsing = $true; TimeoutSec = 20 }
    if ($null -ne $Body) { $parameters.ContentType = 'application/json'; $parameters.Body = [Text.Encoding]::UTF8.GetBytes(($Body | ConvertTo-Json -Depth 100 -Compress)) }
    $status = 0; $content = ''
    try {
        $response = Invoke-WebRequest @parameters
        $status = [int]$response.StatusCode; $content = $response.Content
    } catch {
        $response = $_.Exception.Response
        if ($null -eq $response) { throw }
        $status = [int]$response.StatusCode
        $content = [string](Get-Field $_.ErrorDetails 'Message')
        if ([string]::IsNullOrWhiteSpace($content) -and $response.PSObject.Methods['GetResponseStream']) {
            $reader = New-Object IO.StreamReader($response.GetResponseStream())
            try { $content = $reader.ReadToEnd() } finally { $reader.Dispose() }
        }
    }
    $script:LastObservedState = "HTTP $status $Method $Path $content"
    if ($status -ne $ExpectedStatus) { throw "API expected HTTP $ExpectedStatus; $($script:LastObservedState)" }
    if ([string]::IsNullOrWhiteSpace($content)) { return $null }
    # Keep JSON arrays as arrays, including empty arrays (no PowerShell wrapper counts).
    return ,($content | ConvertFrom-Json)
}

function Wait-Deletion {
    param([string]$Resource, [string]$Name)
    Wait-Until -Name "$Resource/$Name deletion" -TimeoutSeconds 300 -Condition {
        $object = Get-KubeJson -Arguments @('get', $Resource, $Name, '-n', 'platform-system', '--ignore-not-found=true')
        if ($null -eq $object) { return $true }
        return $null
    } | Out-Null
}

function Wait-StackApproval {
    param([string]$StackName)
    Wait-Until -Name "InfraStack/$StackName WaitingApproval" -TimeoutSeconds 120 -Condition {
        $stack = Get-KubeJson -Arguments @('get', 'infrastack', $StackName, '-n', 'platform-system')
        $condition = Get-Condition $stack
        if ($condition -and $condition.status -eq 'False' -and $condition.reason -eq 'WaitingApproval') { return $stack }
        return $null
    } | Out-Null
}

function Assert-NoApply {
    param([string]$StackName, [string]$PlanUID = '', [int]$Seconds = 0)
    $timer = [Diagnostics.Stopwatch]::StartNew()
    do {
        $runs = Get-KubeJson -Arguments @('get', 'terraformruns', '-n', 'platform-system')
        $applies = @($runs.items | Where-Object { $_.spec.stackRef.name -eq $StackName -and $_.spec.operation -eq 'Apply' -and (!$PlanUID -or (Get-Field $_.spec 'planRunUID') -eq $PlanUID) })
        if ($applies.Count -ne 0) { throw 'Apply was created without valid approval of this exact Plan' }
        if ($timer.Elapsed.TotalSeconds -lt $Seconds) { Start-Sleep -Seconds $PollSeconds }
    } while ($timer.Elapsed.TotalSeconds -lt $Seconds)
}

function Assert-MaterializedStack {
    param($Stack, [string]$EnvironmentName, [string]$ClassName, $Source, [string]$BackendName)
    $environment = Get-KubeJson -Arguments @('get', 'platformenvironment', $EnvironmentName, '-n', 'platform-system')
    $class = Get-KubeJson -Arguments @('get', 'environmentclass', $ClassName)
    if ($Stack.spec.source.url -ne $Source.URL -or $Stack.spec.source.revision -ne $Source.Revision -or $Stack.spec.source.path -ne '.' -or
        $Stack.spec.workspace -ne $EnvironmentName -or $Stack.spec.backend.configRef.name -ne $BackendName -or
        $Stack.spec.ownerEnvironmentUID -ne $environment.metadata.uid -or $Stack.metadata.ownerReferences.uid -notcontains $environment.metadata.uid -or
        $Stack.spec.runnerServiceAccountName -ne $class.spec.runnerProfile.serviceAccountName -or
        $Stack.spec.runtimeTargetIdentity.clusterName -ne $Source.TargetContext -or $Stack.spec.targetConnectionProfile.kubeContext -ne $Source.TargetContext -or
        $Stack.spec.capacity.nodeCount -ne 1 -or $Stack.spec.desiredState -ne 'Present') { throw 'EnvironmentClass materialization contract mismatch' }
    Save-Proof -Name "$EnvironmentName-materialization" -Value @{ environment = $environment; class = $class; stack = $Stack }
}

function Assert-Artifact {
    param([string]$Key, [string]$Digest)
    if ($Key -notmatch '^runs/[a-f0-9-]{36}/[^.][^\s]*$' -or $Key -match '\.\.' -or $Digest -notmatch '^sha256:[a-f0-9]{64}$') { throw 'Invalid artifact reference/digest in real run status' }
    $escaped = (($Key -split '/' | ForEach-Object { [Uri]::EscapeDataString($_) }) -join '/')
    $destination = Join-Path $script:StateRoot ([guid]::NewGuid().ToString('N') + '.artifact')
    Invoke-WebRequest -UseBasicParsing -Uri ($LocalStackEndpoint.TrimEnd('/') + '/' + $script:ArtifactBucket + '/' + $escaped) -OutFile $destination -TimeoutSec 30 | Out-Null
    $actual = 'sha256:' + (Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash.ToLower()
    if ($actual -ne $Digest) { throw "Artifact digest mismatch: $Key" }
}

function Assert-PlanEvidence {
    param($Plan)
    if ($Plan.spec.operation -ne 'Plan' -or -not $Plan.status.artifactsReady -or -not $Plan.status.evidenceCaptured -or
        -not $Plan.spec.executionContextDigest -or -not $Plan.status.effectivePlanInputDigest -or
        -not $Plan.spec.infrastructureExecutionIdentityDigest -or -not $Plan.spec.runtimeTargetIdentityDigest) { throw 'Plan identity/evidence incomplete' }
    foreach ($pair in @(@($Plan.status.planRef, $Plan.status.planDigest), @($Plan.status.sourceBundleRef, $Plan.status.sourceBundleDigest),
        @($Plan.status.planReportRef, $Plan.status.planReportDigest), @($Plan.status.resolvedBackendConfigRef, $Plan.status.resolvedBackendConfigDigest),
        @($Plan.status.terminalResultRef, $Plan.status.terminalResultDigest))) { Assert-Artifact -Key $pair[0] -Digest $pair[1] }
    $job = Get-KubeJson -Arguments @('get', 'job', $Plan.status.jobRef.name, '-n', 'platform-system')
    $terminal = Read-ArtifactJson -Key $Plan.status.terminalResultRef
    if ($job.status.succeeded -ne 1 -or $job.metadata.ownerReferences.uid -notcontains $Plan.metadata.uid -or
        $job.spec.template.spec.containers[0].image -ne $script:RunnerImage -or $terminal.terraformRunUID -ne $Plan.metadata.uid -or $terminal.jobUID -ne $job.metadata.uid -or
        $terminal.planDigest -ne $Plan.status.planDigest) { throw 'Plan runner Job/artifact identity mismatch' }
    Save-Proof -Name ($Plan.metadata.name + '-evidence') -Value @{ run = $Plan; job = $job; terminal = $terminal }
}

function Assert-ApprovalBinding {
    param($Approval, $Plan)
    if ($Approval.spec.planRunRef.name -ne $Plan.metadata.name -or $Approval.spec.planRunUID -ne $Plan.metadata.uid -or
        $Approval.spec.planDigest -ne $Plan.status.planDigest -or $Approval.spec.executionContextDigest -ne $Plan.spec.executionContextDigest -or
        $Approval.spec.effectivePlanInputDigest -ne $Plan.status.effectivePlanInputDigest -or $Approval.spec.planReportRef -ne $Plan.status.planReportRef -or
        $Approval.spec.planReportDigest -ne $Plan.status.planReportDigest) { throw 'Exact ChangeApproval binding mismatch' }
    Save-Proof -Name ($Approval.metadata.name + '-binding') -Value $Approval
}

function Assert-SavedPlanApply {
    param($Apply, $Plan, $Approval)
    if ($Apply.spec.operation -ne 'Apply' -or $Apply.spec.planRunUID -ne $Plan.metadata.uid -or
        $Apply.spec.planRef -ne $Plan.status.planRef -or $Apply.spec.planDigest -ne $Plan.status.planDigest -or
        $Apply.spec.source.type -ne 'RetainedBundle' -or $Apply.spec.source.ref -ne $Plan.status.sourceBundleRef -or
        $Apply.spec.source.digest -ne $Plan.status.sourceBundleDigest -or $Apply.spec.approvalRef.name -ne $Approval.metadata.name -or $Apply.spec.approvalUID -ne $Approval.metadata.uid) { throw 'Apply did not retain approved saved-plan/source/approval identity' }
    Assert-Artifact -Key $Apply.status.terminalResultRef -Digest $Apply.status.terminalResultDigest
    $terminal = Read-ArtifactJson -Key $Apply.status.terminalResultRef
    $job = Get-KubeJson -Arguments @('get', 'job', $Apply.status.jobRef.name, '-n', 'platform-system')
    $args = $job.spec.template.spec.containers[0].args
    if ($job.status.succeeded -ne 1 -or $job.metadata.ownerReferences.uid -notcontains $Apply.metadata.uid -or
        $args -notcontains '--operation=Apply' -or $args -notcontains ('--plan-ref=' + $Plan.status.planRef) -or
        $args -notcontains '--plan=/workspace/terraform/plan.binary' -or
        $terminal.operation -ne 'Apply' -or $terminal.planRef -ne $Plan.status.planRef -or
        $terminal.planDigest -ne $Plan.status.planDigest -or $terminal.terraformExitCode -ne 0) { throw 'Runner did not execute the approved saved Plan' }
    Save-Proof -Name ($Apply.metadata.name + '-saved-plan') -Value @{ run = $Apply; job = $job; terminal = $terminal }
    Save-ImageEvidence
}

function Assert-Convergence {
    param([string]$StackName, $Apply, [string]$TargetContext)
    $stack = Get-KubeJson -Arguments @('get', 'infrastack', $StackName, '-n', 'platform-system')
    Assert-Artifact -Key $stack.status.targetDiscoveryRef -Digest $stack.status.targetDiscoveryDigest
    $output = Read-ArtifactJson -Key $stack.status.targetDiscoveryRef
    if ($output.provider -ne 'kind' -or $output.accountId -ne 'local' -or $output.clusterName -ne $TargetContext -or $output.kubeContext -ne $TargetContext -or
        $output.sourceClosureDigest -ne $Apply.status.sourceBundleDigest -or $stack.status.lastConverged.terraformRunUID -ne $Apply.metadata.uid -or
        $stack.status.discoveredRuntimeTargetIdentity.clusterName -ne $TargetContext) { throw 'Terraform output/trusted discovery propagation mismatch' }
    Save-Proof -Name "$StackName-output-convergence" -Value @{ outputArtifact = $output; stack = $stack }
    Add-Result -Name 'TERRAFORM_OUTPUT' -Status 'PASS' -Evidence 'Allowlisted Terraform target_discovery output persisted, digest verified and admitted into InfraStack status'
}

function Invoke-Lane {
    param([string]$Name, [scriptblock]$Action)
    Write-Stage "Suite $Name"
    $timer = [Diagnostics.Stopwatch]::StartNew()
    try { & $Action; Add-Result -Name ('SUITE_' + $Name.ToUpper()) -Status 'PASS' -Evidence ('duration={0:N1}s' -f $timer.Elapsed.TotalSeconds) }
    catch { Add-Result -Name ('SUITE_' + $Name.ToUpper()) -Status 'FAIL' -Evidence $_.Exception.Message; throw }
    finally { $script:SuiteDurations[$Name] = [Math]::Round($timer.Elapsed.TotalSeconds, 1) }
}

function Invoke-Platform {
    Start-PlatformApi
    $suffix = $script:RunId.ToLower().Replace('-', '')
    $name = "platform-$suffix"
    $seedName = "platform-seed-$suffix"
    $className = "platform-class-$suffix"
    $backendName = "platform-backend-$suffix"
    $runtimeName = "platform-svc-$suffix"
    $templateName = "platform-template-$suffix"
    $bucket = "pcp-local-e2e-platform-$suffix"
    $script:OwnedBuckets.Add($bucket)
    Save-Ownership
    New-BackendConfig -Name $backendName -Key "platform/$suffix/$name.tfstate"
    $script:SourceA = New-GitSource -TargetContext $script:TargetAContext -BucketName $bucket -Name 'source-platform'
    $script:SourceB = $null
    Start-SourceServer
    New-EnvironmentClass -Name $seedName -Source $script:SourceA -BackendConfigName $backendName -RuntimeName $runtimeName
    $seed = Get-KubeJson -Arguments @('get', 'environmentclass', $seedName)
    $request = @{ name = $className; spec = $seed.spec }
    $validation = Invoke-Api -Path '/api/classes/validate' -Method POST -Body $request
    if (-not $validation.valid -or (Get-KubeJson -Arguments @('get', 'environmentclass', $className, '--ignore-not-found=true'))) { throw 'Class validation was invalid or persisted its dry-run object' }
    $createdClass = Invoke-Api -Path '/api/classes' -Method POST -Body $request -ExpectedStatus 201
    Wait-Condition -Resource environmentclass -Name $className -Reason ClassValidated -TimeoutSeconds 180 | Out-Null
    $class = Invoke-Api -Path "/api/classes/$className"
    $classes = Invoke-Api -Path '/api/classes'
    if (-not $class.ready -or $class.uid -ne $createdClass.uid -or @($classes | Where-Object name -eq $className).Count -ne 1) { throw 'EnvironmentClass API list/detail/Ready mismatch' }
    Save-Proof -Name 'platform-class-api' -Value @{ validation = $validation; created = $createdClass; detail = $class; list = $classes }
    Add-Result -Name 'PLATFORM_CLASS_API' -Status PASS -Evidence 'Dry-run, create, current Ready, list and detail use the real Kubernetes class'

    $templateRequest = @{ name = $templateName; title = 'Local E2E project template'; description = 'Disposable regression snapshot'; spec = $class.spec }
    $template = Invoke-Api -Path '/api/class-templates' -Method POST -Body $templateRequest -ExpectedStatus 201
    $duplicate = Invoke-Api -Path '/api/class-templates' -Method POST -Body $templateRequest -ExpectedStatus 409
    if ($duplicate.code -ne 'TemplateAlreadyExists') { throw 'Duplicate template contract mismatch' }
    $templates = Invoke-Api -Path '/api/class-templates'
    $storedTemplate = Get-KubeJson -Arguments @('get', 'configmap', "pcp-class-template-$templateName", '-n', 'default')
    if (@($templates | Where-Object { $_.name -eq $templateName -and $_.uid -eq $template.uid }).Count -ne 1 -or
        $storedTemplate.metadata.uid -ne $template.uid -or ($storedTemplate.data.'template.json' | ConvertFrom-Json).spec.source.revision -ne $script:SourceA.Revision) { throw 'Template ConfigMap persistence/list contract mismatch' }
    $rejectedDelete = Invoke-Api -Path "/api/class-templates/$templateName" -Method DELETE -Body @{ uid = $template.uid; confirmName = 'incorrect-name' } -ExpectedStatus 400
    if ($rejectedDelete.code -ne 'TemplateConfirmationRequired') { throw 'Template deletion name confirmation was weakened' }
    Save-Proof -Name 'platform-template-api' -Value @{ template = $template; listed = $templates; configMap = $storedTemplate; rejectedDelete = $rejectedDelete }
    Invoke-Api -Path "/api/class-templates/$templateName" -Method DELETE -Body @{ uid = $template.uid; confirmName = $templateName } | Out-Null
    $afterTemplates = Invoke-Api -Path '/api/class-templates'
    if (@($afterTemplates | Where-Object name -eq $templateName).Count -ne 0 -or
        (Get-KubeJson -Arguments @('get', 'configmap', "pcp-class-template-$templateName", '-n', 'default', '--ignore-not-found=true'))) { throw 'Template delete did not remove its identified ConfigMap' }
    Add-Result -Name 'PLATFORM_TEMPLATE_API' -Status PASS -Evidence 'Real ConfigMap create/read/duplicate rejection/exact-name delete; no class or environment created by template'

    $before = Invoke-Api -Path '/api/environments'
    if (@($before | Where-Object name -eq $name).Count -ne 0) { throw 'Environment exists before explicit API submission' }
    $draft = Invoke-Api -Path '/api/drafts' -Method POST -Body @{ description = 'Create a small development environment without cache'; name = $name; namespace = 'platform-system'; classRef = $className; region = 'local'; nodeCount = 1 }
    if (-not $draft.valid -or (Get-KubeJson -Arguments @('get', 'platformenvironment', $name, '-n', 'platform-system', '--ignore-not-found=true'))) { throw 'Draft generated invalid intent or created an environment without submit' }
    $created = Invoke-Api -Path '/api/environments' -Method POST -Body @{ name = $name; namespace = 'platform-system'; classRef = $className; region = 'local'; nodeCount = 1 } -ExpectedStatus 201
    $script:EnvironmentNames.Add($name)
    $stack = Wait-Until -Name 'API environment automatic InfraStack' -TimeoutSeconds 180 -Condition { Get-StackForEnvironment -EnvironmentName $name }
    Assert-MaterializedStack -Stack $stack -EnvironmentName $name -ClassName $className -Source $script:SourceA -BackendName $backendName
    $plan = Wait-Plan -StackName $stack.metadata.name
    Assert-PlanEvidence -Plan $plan
    Wait-StackApproval -StackName $stack.metadata.name
    Assert-NoApply -StackName $stack.metadata.name
    Assert-TargetService -Context $script:TargetAContext -Name $runtimeName -Present $false
    $view = Invoke-Api -Path "/api/environments/platform-system/$name"
    $listed = Invoke-Api -Path '/api/environments'
    if ($view.uid -ne $created.uid -or $view.infrastructureDetail.name -ne $stack.metadata.name -or $view.infrastructure -ne 'WaitingApproval' -or @($listed | Where-Object name -eq $name).Count -ne 1) { throw 'Environment read-model/API identity or approval-state mismatch' }
    $inUse = Invoke-Api -Path "/api/classes/$className" -Method DELETE -Body @{ uid = $class.uid; confirmName = $className } -ExpectedStatus 409
    if ($inUse.code -ne 'ClassInUse') { throw 'API allowed deletion of a referenced class' }
    Save-Proof -Name 'platform-environment-before-approval' -Value @{ created = $created; detail = $view; listed = $listed; draft = $draft }
    Add-Result -Name 'PLATFORM_READ_MODEL' -Status PASS -Evidence 'Explicit top-level API create, automatic InfraStack, WaitingApproval and no early Apply/runtime'

    $planPath = "/api/terraform-runs/platform-system/$($plan.metadata.name)"
    $visual = Invoke-Api -Path ($planPath + '/plan')
    $architecture = Invoke-Api -Path "/api/environments/platform-system/$name/architecture"
    if (-not $visual.available -or -not $architecture.available -or $visual.graph.terraformRunUID -ne $plan.metadata.uid -or
        $visual.graph.planDigest -ne $plan.status.planDigest -or @($visual.graph.nodes).Count -ne 1 -or $visual.graph.nodes[0].action -ne 'create' -or
        $visual.graph.nodes[0].address -ne 'aws_s3_bucket.lifecycle') { throw 'Real sanitized PlanGraph contract mismatch' }
    $safeAttributes = @('arn', 'bucket', 'id', 'region', 'tags')
    foreach ($node in $visual.graph.nodes) {
        foreach ($side in @('before', 'after')) {
            $properties = Get-Field $node $side
            if ($properties) {
                foreach ($property in $properties.PSObject.Properties.Name) { if ($property -notin $safeAttributes) { throw "Unexpected S3 attribute in sanitized graph: $property" } }
            }
        }
    }
    Save-Proof -Name 'platform-plan-visualization' -Value @{ plan = $visual; architecture = $architecture }
    Add-Result -Name 'PLATFORM_PLAN_GRAPH' -Status PASS -Evidence 'Actual immutable Plan UID/digest and allowlisted create node through run and architecture endpoints'

    $binding = @{ planRunUID = $plan.metadata.uid; planDigest = $plan.status.planDigest; executionContextDigest = $plan.spec.executionContextDigest;
        effectivePlanInputDigest = $plan.status.effectivePlanInputDigest; planReportRef = $plan.status.planReportRef; planReportDigest = $plan.status.planReportDigest }
    $badBinding = @{}; foreach ($key in $binding.Keys) { $badBinding[$key] = $binding[$key] }; $badBinding.planReportDigest = 'sha256:' + ('0' * 64)
    $stale = Invoke-Api -Path ($planPath + '/approve') -Method POST -Body $badBinding -ExpectedStatus 409
    if ($stale.code -ne 'StalePlan') { throw 'API accepted a stale Plan report digest' }
    Assert-NoApply -StackName $stack.metadata.name -Seconds 8
    $approved = Invoke-Api -Path ($planPath + '/approve') -Method POST -Body $binding -ExpectedStatus 201
    $approval = Get-KubeJson -Arguments @('get', 'changeapproval', $approved.name, '-n', 'platform-system')
    Assert-ApprovalBinding -Approval $approval -Plan $plan
    $apply = Wait-Apply -StackName $stack.metadata.name -PlanUID $plan.metadata.uid
    Assert-SavedPlanApply -Apply $apply -Plan $plan -Approval $approval
    Wait-Condition -Resource infrastack -Name $stack.metadata.name -Reason InfrastructureReady | Out-Null
    Assert-Convergence -StackName $stack.metadata.name -Apply $apply -TargetContext $script:TargetAContext
    Wait-Condition -Resource resourceset -Name "$name-resources" -Reason RuntimeReady | Out-Null
    Wait-Condition -Resource platformenvironment -Name $name -Reason EnvironmentReady | Out-Null
    Wait-TargetService -Context $script:TargetAContext -Name $runtimeName -Present $true
    Assert-TargetService -Context $script:TargetBContext -Name $runtimeName -Present $false
    $set = Get-KubeJson -Arguments @('get', 'resourceset', "$name-resources", '-n', 'platform-system')
    $ready = Invoke-Api -Path "/api/environments/platform-system/$name"
    if (-not $ready.ready -or $ready.runtimeDetail.inventoryItems -ne 1 -or $set.spec.target.clusterName -ne $script:TargetAContext -or
        -not $set.spec.runtimeMutationAllowed -or (Get-Field $set.spec 'mutationFence') -or @($set.status.inventory).Count -ne 1) { throw 'Runtime/ResourceSet/read model convergence mismatch' }
    Save-Proof -Name 'platform-ready-runtime' -Value @{ environment = $ready; resourceSet = $set; staleApprovalRejected = $stale; exactApproval = $approval }
    Add-Result -Name 'PLATFORM_APPROVAL_API' -Status PASS -Evidence 'Stale report digest rejected; exact ChangeApproval persisted; saved-plan Apply, ResourceSet and read model Ready'

    $staleUpdate = Invoke-Api -Path "/api/environments/platform-system/$name" -Method PUT -Body @{ uid = $ready.uid; generation = ($ready.generation - 1); nodeCount = 2 } -ExpectedStatus 409
    if ($staleUpdate.code -ne 'StaleEnvironment') { throw 'Stale update generation was accepted' }
    Invoke-Api -Path "/api/environments/platform-system/$name" -Method PUT -Body @{ uid = $ready.uid; generation = $ready.generation; nodeCount = 2 } | Out-Null
    $updated = Wait-Plan -StackName $stack.metadata.name -ExcludeName $plan.metadata.name
    if ($updated.status.executionOutcome -ne 'NoChange') { throw 'Capacity-only API update must be NoChange for the canonical fixture' }
    Wait-Condition -Resource platformenvironment -Name $name -Reason EnvironmentReady | Out-Null
    $updatedView = Invoke-Api -Path "/api/environments/platform-system/$name"
    $runs = Invoke-Api -Path "/api/environments/platform-system/$name/terraform-runs"
    if ($updatedView.nodeCount -ne 2 -or $updatedView.generation -le $ready.generation -or @($runs | Where-Object operation -eq Apply).Count -ne 1) { throw 'API capacity update duplicated Apply or lost desired generation' }
    Add-Result -Name 'PLATFORM_UPDATE_API' -Status PASS -Evidence 'Stale generation rejected; new capacity generation converged through NoChange without extra Apply (no infrastructure scaling claim)'
    Delete-EnvironmentLifecycle -EnvironmentName $name -StackName $stack.metadata.name -Bucket $bucket -RuntimeName $runtimeName -DeleteViaApi
    $notFound = Invoke-Api -Path "/api/environments/platform-system/$name" -ExpectedStatus 404
    if ($notFound.code -ne 'NotFound') { throw 'Deleted environment remained in the API' }
    Invoke-Api -Path "/api/classes/$className" -Method DELETE -Body @{ uid = $class.uid; confirmName = $className } -ExpectedStatus 202 | Out-Null
    Wait-Deletion -Resource environmentclass -Name $className
    Remove-KubeObject -Arguments @('delete', 'environmentclass', $seedName)
    Add-Result -Name 'PLATFORM_DELETE_API' -Status PASS -Evidence 'UID-bound delete, separate exact Destroy approval, finalizer removal, API NotFound and class cleanup'
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
    Write-Stage 'Checking tools used by this local regression lane'
    $names = @('git', 'python')
    if ($RequireDocker) { $names = @('docker', 'kind', 'kubectl') + $names }
    if ($Suite -in @('all', 'platform') -or $BrowserSession) { $names += 'go' }
    foreach ($name in $names) {
        if (-not (Get-Command $name -ErrorAction SilentlyContinue)) { throw "Required command not found: $name" }
    }
    if ($RequireDocker) { Invoke-Tool -File 'docker' -Arguments @('info') -LogName 'docker-info.txt' | Out-Null }
    if ($ApiPort -and ($Suite -in @('all', 'platform') -or $BrowserSession)) {
        $listener = New-Object Net.Sockets.TcpListener([Net.IPAddress]::Loopback, $ApiPort)
        try { $listener.Start() } catch { throw "Requested API port $ApiPort is busy; free it or omit -ApiPort for an isolated port" } finally { $listener.Stop() }
    }
    # Terraform is pinned inside runner.Dockerfile and executed by real Runner Jobs.
    # Neither a host Terraform installation nor Helm/standalone Kustomize is required.
}

function Test-LocalStack {
    $uri = [Uri]$LocalStackEndpoint
    if ($uri.Scheme -ne 'http' -or $uri.Host -notin @('localhost', '127.0.0.1', '::1') -or $uri.AbsolutePath -ne '/') {
        throw "LocalStack endpoint must be a local HTTP root URL, got $LocalStackEndpoint"
    }
    Write-Stage "Preparing LocalStack at $LocalStackEndpoint"
    $containers = Invoke-Tool -File 'docker' -Arguments @('ps', '--format', '{{.Names}}|{{.Image}}')
    $matches = @($containers -split "`r?`n" | Where-Object { $_ -match '\|(?:.*/)?localstack/localstack(?:-pro)?(?::|@|$)' } | ForEach-Object {
        $name = $_.Split('|')[0]
        $info = (Invoke-Tool -File 'docker' -Arguments @('inspect', $name) | ConvertFrom-Json)[0]
        $ports = Get-Field $info.NetworkSettings.Ports '4566/tcp'
        if (@($ports | Where-Object { $_.HostPort -eq [string]$uri.Port }).Count -gt 0) { $name }
    })
    if ($matches.Count -gt 1) { throw 'Multiple LocalStack containers publish the requested port' }
    if ($matches.Count -eq 1) {
        $script:LocalStackContainer = $matches[0]
        Write-Host "[INFO] Reusing external LocalStack $($script:LocalStackContainer); it will be preserved."
    } else {
        if ($RequireExistingLocalStack) { throw "No running external LocalStack publishes $LocalStackEndpoint; -RequireExistingLocalStack forbids automatic container startup" }
        $script:LocalStackContainer = "pcp-local-e2e-localstack-$($uri.Port)"
        $existing = Invoke-Tool -File docker -Arguments @('ps', '-a', '--format', '{{.Names}}')
        if ($existing -split "`r?`n" -contains $script:LocalStackContainer) { throw 'Reserved LocalStack container already exists but is not a running service on this endpoint; refusing to replace it' }
        # Record intent before creation, so failure during docker run still has owned cleanup.
        $script:OwnedLocalStack = $true
        Save-Ownership
        Invoke-Tool -File 'docker' -Arguments @('run', '-d', '--name', $script:LocalStackContainer,
            '--label', "platform.example.io/local-e2e-run=$($script:RunId)",
            '-p', "127.0.0.1:$($uri.Port):4566", '-e', 'SERVICES=s3,sts', $LocalStackImage) -LogName 'localstack-start.txt' | Out-Null
    }
    Wait-Until -Name 'LocalStack S3 and STS readiness' -TimeoutSeconds 180 -Condition {
        $container = (Invoke-Tool -File docker -Arguments @('inspect', $script:LocalStackContainer) | ConvertFrom-Json)[0]
        if (-not $container.State.Running) { Stop-Regression "LocalStack exited (code $($container.State.ExitCode)); inspect localstack.log or select a usable -LocalStackImage" }
        $health = Invoke-RestMethod -Uri ($LocalStackEndpoint.TrimEnd('/') + '/_localstack/health') -TimeoutSec 10
        $script:LastObservedState = $health | ConvertTo-Json -Depth 20 -Compress
        if ((Get-Field $health.services 's3') -in @('available', 'running', 'active') -and
            (Get-Field $health.services 'sts') -in @('available', 'running', 'active')) { return $health }
        return $null
    } | ConvertTo-Json -Depth 20 | Set-Content (Join-Path $script:ArtifactRoot 'localstack-health.json') -Encoding UTF8
    $identity = Invoke-Tool -File 'docker' -Arguments @('exec', $script:LocalStackContainer, 'awslocal', 'sts', 'get-caller-identity') | ConvertFrom-Json
    if ($identity.Account -ne '000000000000') { throw 'Unexpected LocalStack account; refusing cloud execution' }
    $identity | ConvertTo-Json | Set-Content (Join-Path $script:ArtifactRoot 'localstack-identity.json') -Encoding UTF8
    Save-Ownership
}

function New-Clusters {
    $suffix = $script:RunId.ToLower().Replace('-', '')
    $script:ManagementCluster = "pcp-local-e2e-$suffix-management"
    $script:TargetACluster = "pcp-local-e2e-$suffix-target-a"
    $script:TargetBCluster = "pcp-local-e2e-$suffix-target-b"
    foreach ($cluster in @($script:ManagementCluster, $script:TargetACluster, $script:TargetBCluster)) {
        Write-Stage "Creating Kind cluster $cluster"
        $script:OwnedClusters.Add($cluster)
        Save-Ownership
        Invoke-Tool -File 'kind' -Arguments @('create', 'cluster', '--name', $cluster, '--kubeconfig', (Join-Path $script:StateRoot 'bootstrap.kubeconfig'), '--wait', '120s') -LogName "$cluster-create.txt" | Out-Null
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

function Configure-TerraformDns {
    if (-not $TerraformDnsServer) { return }
    $resolver = $null
    if (-not [Net.IPAddress]::TryParse($TerraformDnsServer, [ref]$resolver)) { throw 'TerraformDnsServer must be a DNS server IP address' }
    if ($script:ManagementCluster -notin $script:OwnedClusters) { throw 'Refusing DNS configuration outside the owned management cluster' }
    Write-Stage "Configuring owned management DNS for Terraform dependency domains via $resolver"
    $config = Get-KubeJson -Arguments @('get', 'configmap', 'coredns', '-n', 'kube-system')
    $original = [string]$config.data.Corefile
    if ($original -match '(?m)^registry\.terraform\.io') { throw 'Terraform DNS zone already configured; refusing to replace it' }
    # Only dependency domains use the explicit resolver. Cluster/service DNS and
    # host.docker.internal continue using the original Kind configuration.
    $zones = @"
registry.terraform.io:53 releases.hashicorp.com:53 {
    errors
    cache 30
    forward . $resolver {
        health_check 5s
    }
}

"@
    $patchFile = Join-Path $script:StateRoot 'terraform-dns-patch.json'
    @{ data = @{ Corefile = ($zones + $original) } } | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $patchFile -Encoding UTF8
    Invoke-Kubectl -Arguments @('patch', 'configmap', 'coredns', '-n', 'kube-system', '--type=merge', '--patch-file', $patchFile) -LogName 'terraform-dns-config.txt' | Out-Null
    Invoke-Kubectl -Arguments @('rollout', 'restart', 'deployment/coredns', '-n', 'kube-system') | Out-Null
    Invoke-Kubectl -Arguments @('rollout', 'status', 'deployment/coredns', '-n', 'kube-system', '--timeout=120s') -LogName 'terraform-dns-rollout.txt' | Out-Null
    $probeName = 'dns-probe-' + $script:RunId.Replace('-', '')
    $probe = @{ apiVersion = 'v1'; kind = 'Pod'; metadata = @{ name = $probeName; namespace = 'kube-system' };
        spec = @{ restartPolicy = 'Never'; containers = @(@{ name = 'dns'; image = 'alpine/git:2.45.2'; imagePullPolicy = 'Never';
            command = @('/bin/sh', '-c', 'nslookup registry.terraform.io && nslookup releases.hashicorp.com && nslookup host.docker.internal && nslookup kubernetes.default.svc.cluster.local') }) } }
    Write-KubeObject -Object $probe
    $completed = Wait-Until -Name 'Terraform dependency and local service DNS probe' -TimeoutSeconds 90 -Condition {
        $pod = Get-KubeJson -Arguments @('get', 'pod', $probeName, '-n', 'kube-system')
        if ($pod.status.phase -eq 'Failed') {
            Invoke-Kubectl -Arguments @('logs', $probeName, '-n', 'kube-system') -LogName 'terraform-dns-probe.log' | Out-Null
            Stop-Regression 'Terraform DNS preflight failed; see terraform-dns-probe.log'
        }
        if ($pod.status.phase -eq 'Succeeded') { return $pod }
        return $null
    }
    Invoke-Kubectl -Arguments @('logs', $probeName, '-n', 'kube-system') -LogName 'terraform-dns-probe.log' | Out-Null
    Save-Proof -Name 'terraform-dns-profile' -Value @{ resolver = $resolver.ToString(); domains = @('registry.terraform.io', 'releases.hashicorp.com'); originalCorefile = $original; configuredCorefile = ($zones + $original); probe = $completed }
    Remove-KubeObject -Arguments @('delete', 'pod', $probeName, '-n', 'kube-system')
    Add-Result -Name 'TERRAFORM_DNS_PREFLIGHT' -Status PASS -Evidence 'Explicit dependency-domain forwarding; real Pod verified both public dependencies, host.docker.internal and Kubernetes service DNS'
}

function Build-Images {
    Save-SourceState
    $tag = "local-$($script:RunId.ToLower())"
    $script:ManagerImage = "platform-control-plane:$tag"
    $script:RunnerImage = "platform-terraform-runner:$tag"
    Write-Stage "Building $($script:ManagerImage) and $($script:RunnerImage) (Go parallelism=$BuildParallelism)"
    # Derive disposable build files from the canonical Dockerfiles. Only compiler
    # resource/cache settings change; source, build flags, runtime image and entrypoint stay
    # canonical. This avoids exhausting a workstation while three Kind nodes run.
    $buildFiles = @{}
    $canonicalIgnore = Get-Content -LiteralPath (Join-Path $script:RepoRoot '.dockerignore') -Raw
    foreach ($canonical in @('Dockerfile', 'runner.Dockerfile')) {
        $content = Get-Content -LiteralPath (Join-Path $script:RepoRoot $canonical) -Raw
        $matches = [regex]::Matches($content, '\bgo build\b')
        if ($matches.Count -ne 1) { throw "Expected one canonical Go build command in $canonical" }
        $generated = Join-Path $script:StateRoot ('bounded-' + $canonical)
        # A unique compiler-step input forces Go build to execute for every run,
        # while allowing Go's content-validated dependency caches to persist.
        $boundedContent = [regex]::Replace($content, '\bgo build\b', "PCP_LOCAL_E2E_BUILD_RUN_ID=$($script:RunId) GOMEMLIMIT=512MiB go build -p $BuildParallelism")
        $boundedContent = [regex]::Replace($boundedContent, '(?m)^RUN (?=[^\r\n]*\bgo (?:mod download|build)\b)', 'RUN --mount=type=cache,id=kpcp-local-e2e-go-mod,target=/go/pkg/mod --mount=type=cache,id=kpcp-local-e2e-go-build,target=/root/.cache/go-build ')
        [IO.File]::WriteAllText($generated, $boundedContent, (New-Object Text.UTF8Encoding($false)))
        # Local Windows binaries are generated outputs, never container inputs.
        # Keep every canonical exclusion and add only this E2E-specific exclusion.
        [IO.File]::WriteAllText(($generated + '.dockerignore'), ($canonicalIgnore.TrimEnd() + "`n/bin/`n"), (New-Object Text.UTF8Encoding($false)))
        $buildFiles[$canonical] = $generated
    }
    Save-Proof -Name 'build-profile' -Value @{ goBuildParallelism = $BuildParallelism; compilerMemoryLimit = '512MiB'; canonicalDockerfiles = @('Dockerfile', 'runner.Dockerfile'); additionalContextExclusions = @('/bin/'); goCacheMounts = $true; compilerStepRunId = $script:RunId; noCache = $false }
    Invoke-Tool -File 'docker' -Arguments @('build', '-t', $script:ManagerImage, '-f', $buildFiles['Dockerfile'], '.') -LogName 'manager-build.txt' | Out-Null
    Invoke-Tool -File 'docker' -Arguments @('build', '-t', $script:RunnerImage, '-f', $buildFiles['runner.Dockerfile'], '.') -LogName 'runner-build.txt' | Out-Null
    $version = Invoke-Tool -File 'docker' -Arguments @('run', '--rm', '--entrypoint', 'terraform', $script:RunnerImage, 'version', '-json') -LogName 'runner-terraform-version.json' | ConvertFrom-Json
    if ($version.terraform_version -ne '1.14.0') { throw 'Built runner has an unexpected Terraform version' }
    Prepare-GitImage
    foreach ($cluster in $script:OwnedClusters) {
        Invoke-Tool -File 'kind' -Arguments @('load', 'docker-image', $script:ManagerImage, '--name', $cluster) -LogName "$cluster-load-manager.txt" | Out-Null
        Invoke-Tool -File 'kind' -Arguments @('load', 'docker-image', $script:RunnerImage, '--name', $cluster) -LogName "$cluster-load-runner.txt" | Out-Null
        Invoke-Tool -File 'kind' -Arguments @('load', 'docker-image', 'alpine/git:2.45.2', '--name', $cluster) -LogName "$cluster-load-git.txt" | Out-Null
    }
}

function Save-SourceState {
    $paths = (Invoke-Tool -File 'git' -Arguments @('-c', 'core.quotePath=false', 'ls-files', '--cached', '--others', '--exclude-standard')).Trim() -split "`r?`n" | Where-Object { $_ -and $_ -notlike 'docs/*' -and $_ -ne 'README.md' -and (Test-Path -LiteralPath (Join-Path $script:RepoRoot $_) -PathType Leaf) } | Sort-Object
    $files = @($paths | ForEach-Object { [ordered]@{ path = $_; sha256 = (Get-FileHash -LiteralPath (Join-Path $script:RepoRoot $_) -Algorithm SHA256).Hash.ToLower() } })
    $canonical = ($files | ForEach-Object { "$($_.path)=$($_.sha256)" }) -join "`n"
    $hash = [Security.Cryptography.SHA256]::Create()
    try { $fingerprint = ([BitConverter]::ToString($hash.ComputeHash([Text.Encoding]::UTF8.GetBytes($canonical)))).Replace('-', '').ToLower() } finally { $hash.Dispose() }
    [ordered]@{ head = (Invoke-Tool -File 'git' -Arguments @('rev-parse', 'HEAD')).Trim(); workingTree = (Invoke-Tool -File 'git' -Arguments @('status', '--porcelain')); runtimeSourceSHA256 = $fingerprint; files = $files } | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $script:ArtifactRoot 'source-state.json') -Encoding UTF8
}

function Save-ImageEvidence {
    $pods = Get-KubeJson -Arguments @('get', 'pods', '-n', 'platform-system')
    foreach ($pod in $pods.items) {
        foreach ($container in $pod.spec.containers) {
            if ($container.image -notin @($script:ManagerImage, $script:RunnerImage)) { continue }
            $statuses = $pod.status.PSObject.Properties['containerStatuses']
            if (-not $statuses) { continue }
            $status = @($statuses.Value | Where-Object { $_.name -eq $container.name }) | Select-Object -First 1
            if (-not $status -or -not $status.imageID) { continue }
            $script:ImageObservations["$($pod.metadata.uid)/$($container.name)"] = [ordered]@{ pod = $pod.metadata.name; podUID = $pod.metadata.uid; node = $pod.spec.nodeName; container = $container.name; requestedImage = $container.image; runningImageID = $status.imageID }
        }
    }
    $images = @(@{ component = 'controller'; tag = $script:ManagerImage; dockerfile = 'Dockerfile' }, @{ component = 'terraform-runner'; tag = $script:RunnerImage; dockerfile = 'runner.Dockerfile' }) | ForEach-Object { [ordered]@{ component = $_.component; tag = $_.tag; dockerfile = $_.dockerfile; builtImageID = (Invoke-Tool -File 'docker' -Arguments @('image', 'inspect', '--format', '{{.Id}}', $_.tag)).Trim() } }
    [ordered]@{ runId = $script:RunId; images = @($images); observedPods = @($script:ImageObservations.Values) } | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $script:ArtifactRoot 'image-evidence.json') -Encoding UTF8
}

function Wait-BrowserSession {
    Start-PlatformApi
    $suffix = $script:RunId.ToLower().Replace('-', '')
    $name = 'browser-' + $suffix.Substring(0, 16)
    $bucket = 'pcp-local-e2e-browser-' + $suffix
    $script:OwnedBuckets.Add($bucket)
    Save-Ownership
    $script:SourceA = New-GitSource -TargetContext $script:TargetAContext -BucketName $bucket -Name 'source-browser'
    $script:SourceB = $null
    Start-SourceServer
    $source = $script:SourceA
    New-BackendConfig -Name "$name-backend" -Key "browser/$suffix/environment.tfstate"
    New-EnvironmentClass -Name "$name-class" -Source $source -BackendConfigName "$name-backend" -RuntimeName "$name-svc"
    $hold = Join-Path $script:ArtifactRoot 'browser-hold.flag'
    'Remove only this hold file to release this run after browser acceptance.' | Set-Content -LiteralPath $hold -Encoding UTF8
    $metadata = [ordered]@{ runId = $script:RunId; managementKubeconfig = $script:ManagementKubeconfig;
        targetKubeconfig = $script:TargetKubeconfigA; artifactBucket = $script:ArtifactBucket;
        managedBucket = $bucket; className = "$name-class"; serviceName = "$name-svc";
        runtimeNamespace = 'default'; environmentName = $name; stateRoot = $script:StateRoot;
        holdFile = $hold; apiBase = $script:ApiBase; apiPid = $script:ApiProcess.Id;
        aiProvider = $script:AIProvider; ollamaEndpoint = $script:OllamaEndpoint; ollamaModel = $script:OllamaModel;
        expiresAt = (Get-Date).ToUniversalTime().AddSeconds($BrowserSessionTimeoutSeconds).ToString('o') }
    $metadata | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $script:ArtifactRoot 'browser-session.json') -Encoding UTF8
    # Legacy parameter and metadata readers remain usable during migration.
    $metadata | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $script:ArtifactRoot 'stage2-session.json') -Encoding UTF8
    Write-Summary
    Write-Stage "BROWSER_SESSION_READY $($script:ArtifactRoot) API=$($script:ApiBase)"
    Wait-Until -Name 'explicit browser session release' -TimeoutSeconds $BrowserSessionTimeoutSeconds -PollSeconds 10 -Condition {
        Save-ImageEvidence
        $holdPresent = Test-Path -LiteralPath $hold
        $script:LastObservedState = @{ holdFile = $hold; holdPresent = $holdPresent; apiPid = $script:ApiProcess.Id; apiExited = $script:ApiProcess.HasExited } | ConvertTo-Json -Compress
        if (-not $holdPresent) { return $true }
        return $null
    } | Out-Null
    Add-Result -Name 'BROWSER_SESSION' -Status 'PASS' -Evidence 'Ready class and live API supplied; explicit hold-file release observed'
}

function Prepare-GitImage {
    try { $script:OriginalGitImageID = (Invoke-Tool -File 'docker' -Arguments @('image', 'inspect', '--format', '{{.Id}}', 'alpine/git:2.45.2')).Trim() } catch { $script:OriginalGitImageID = '' }
    $script:OwnedGitImage = "pcp-local-e2e-git:$($script:RunId.ToLower())"
    Save-Ownership
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
        "PCP_ARTIFACT_ENDPOINT=$((Get-ContainerEndpoint))",
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
    Save-ImageEvidence
}

function New-GitSource {
    param([string]$TargetContext, [string]$BucketName, [string]$Name)
    $root = Join-Path (Join-Path $script:StateRoot 'git') $Name
    $work = Join-Path $root 'work'
    $bare = Join-Path (Join-Path $script:StateRoot 'git') "$Name.git"
    New-Item -ItemType Directory -Force -Path $work, (Split-Path $bare) | Out-Null
    Copy-Item -Path (Join-Path $script:RepoRoot 'test/fixtures/terraform/localstack-basic/*') -Destination $work -Recurse -Force
    $variables = Join-Path $work 'variables.tf'
    $content = Get-Content -Raw -Path $variables
    $content = $content -replace 'default = "pcp-e2e-lifecycle-bucket"', ('default = "' + $BucketName + '"')
    $content = $content -replace 'default = "kind-pcp-target-local"', ('default = "' + $TargetContext + '"')
    $content = $content -replace 'default = "kubeconfig:kind-pcp-target-local"', ('default = "kubeconfig:' + $TargetContext + '"')
    $content = $content.Replace('http://host.docker.internal:4566', (Get-ContainerEndpoint))
    $content | Set-Content -Path $variables -Encoding UTF8
    Push-Location $work
    try {
        Invoke-Tool -File 'git' -Arguments @('init', '--initial-branch=main') | Out-Null
        Invoke-Tool -File 'git' -Arguments @('config', 'user.email', 'e2e@local.invalid') | Out-Null
        Invoke-Tool -File 'git' -Arguments @('config', 'user.name', 'KPCP Local E2E') | Out-Null
        Invoke-Tool -File 'git' -Arguments @('add', '.') | Out-Null
        Invoke-Tool -File 'git' -Arguments @('commit', '-m', 'local-e2e-fixture') | Out-Null
        $revision = (Invoke-Tool -File 'git' -Arguments @('rev-parse', 'HEAD')).Trim()
        Invoke-Tool -File 'git' -Arguments @('clone', '--bare', $work, $bare) | Out-Null
        Push-Location $bare
        try { Invoke-Tool -File 'git' -Arguments @('update-server-info') | Out-Null } finally { Pop-Location }
    } finally { Pop-Location }
    return [pscustomobject]@{ Name = $Name; Root = $root; Work = $work; Bare = $bare; Revision = $revision; TargetContext = $TargetContext; Bucket = $BucketName }
}

function Start-SourceServer {
    $port = Get-FreeLocalPort
    $root = Join-Path $script:StateRoot 'git'
    $stdout = Join-Path $script:LogRoot 'git-http.stdout.log'
    $stderr = Join-Path $script:LogRoot 'git-http.stderr.log'
    $process = Start-Process -FilePath 'python' -ArgumentList @('-m', 'http.server', "$port", '--bind', '0.0.0.0') -WorkingDirectory $root -RedirectStandardOutput $stdout -RedirectStandardError $stderr -WindowStyle Hidden -PassThru
    $script:StartedProcesses.Add($process)
    Save-Ownership
    Wait-Until -Name 'temporary Git HTTP server' -TimeoutSeconds 30 -Condition {
        try { Invoke-Tool -File 'git' -Arguments @('ls-remote', "http://127.0.0.1:$port/$($script:SourceA.Name).git") | Out-Null; return $true } catch {}
        return $null
    } | Out-Null
    foreach ($source in @($script:SourceA, $script:SourceB) | Where-Object { $null -ne $_ }) {
        $source | Add-Member -Force -NotePropertyName URL -NotePropertyValue "http://host.docker.internal:$port/$($source.Name).git"
    }
}

function New-BackendConfig {
    param([string]$Name, [string]$Key)
    $script:BackendNames.Add($Name)
    $endpoint = (Get-ContainerEndpoint)
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
    return [ordered]@{ apiVersion = 'v1'; kind = 'Service'; metadata = $metadata; spec = [ordered]@{ selector = [ordered]@{ 'local-e2e' = $Name }; ports = @([ordered]@{ name = 'http'; port = 80; targetPort = 80 }) } }
}

function New-EnvironmentClass {
    param([string]$Name, $Source, [string]$BackendConfigName, [string]$RuntimeName, [string]$RunnerServiceAccountName = 'terraform-runner', [string]$RuntimeRevision = '')
    $runtime = [ordered]@{ version = 'v1'; resource = 'services'; readinessPolicy = 'RequireReady'; deletionPolicy = 'Prune'; ownershipID = $RuntimeName; object = (New-RuntimeServiceObject -Name $RuntimeName -Revision $RuntimeRevision) }
    $class = [ordered]@{
        apiVersion = 'platform.example.io/v1alpha1'; kind = 'EnvironmentClass'; metadata = [ordered]@{ name = $Name }
        spec = [ordered]@{
            version = 'local-e2e-v1'
            source = [ordered]@{ url = $Source.URL; revision = $Source.Revision; path = '.' }
            backend = [ordered]@{ type = 's3'; configRef = [ordered]@{ name = $BackendConfigName }; authRef = [ordered]@{ serviceAccountName = $RunnerServiceAccountName }; lockTimeout = '2m' }
            executor = [ordered]@{ terraformVersion = '1.14.0'; image = $script:RunnerImage; workDir = '/workspace/terraform'; executionTimeout = '12m' }
            runnerProfile = [ordered]@{ serviceAccountName = $RunnerServiceAccountName; image = $script:RunnerImage; imageDigest = 'local-e2e'; terraformVersion = '1.14.0' }
            runtimeProfile = [ordered]@{ runtimeObjects = @($runtime) }
            target = [ordered]@{ provider = 'kind'; account = 'local'; region = 'local'; clusterName = $Source.TargetContext; clusterID = $Source.TargetContext; incarnationID = $Source.TargetContext; connectionProfileRef = 'kubeconfig:' + $Source.TargetContext }
            allowedRegions = @('local')
            capacityBounds = [ordered]@{ minNodeCount = 1; maxNodeCount = 10; maxEnvironments = 10; maxConcurrentPlans = 2; maxConcurrentApplies = 1 }
            approvalPolicy = 'Manual'
        }
    }
    Write-KubeObject -Object $class
    Wait-Condition -Resource 'environmentclass' -Name $Name -Reason 'ClassValidated' -TimeoutSeconds 180 | Out-Null
}

function New-Environment {
    param([string]$Name, [string]$ClassName)
    $script:EnvironmentNames.Add($Name)
    $object = [ordered]@{ apiVersion = 'platform.example.io/v1alpha1'; kind = 'PlatformEnvironment'; metadata = [ordered]@{ name = $Name; namespace = 'platform-system' }; spec = [ordered]@{ classRef = [ordered]@{ name = $ClassName }; capacity = [ordered]@{ nodeCount = 1 }; desiredState = 'Present' } }
    Write-KubeObject -Object $object
}

function Get-StackForEnvironment {
    param([string]$EnvironmentName)
    $stack = Get-KubeJson -Arguments @('get', 'infrastacks', '-n', 'platform-system') | Select-Object -ExpandProperty items | Where-Object { $_.metadata.ownerReferences.name -contains $EnvironmentName } | Select-Object -First 1
    if ($stack) { $script:DiagnosticResource = @{ resource = 'infrastack'; name = $stack.metadata.name; kubeconfig = $script:ManagementKubeconfig } }
    return $stack
}

function Get-PlanForStack {
    param([string]$StackName, [string]$PlanMode = 'Reconcile', [string]$ExcludeName = '')
    $list = Get-KubeJson -Arguments @('get', 'terraformruns', '-n', 'platform-system')
    return @($list.items) | Where-Object { $_.spec.stackRef.name -eq $StackName -and $_.spec.operation -eq 'Plan' -and $_.spec.planMode -eq $PlanMode -and $_.metadata.name -ne $ExcludeName } | Sort-Object { $_.metadata.creationTimestamp } -Descending | Select-Object -First 1
}

function Wait-Plan {
    param([string]$StackName, [string]$PlanMode = 'Reconcile', [string]$ExcludeName = '')
    $plan = Wait-Until -Name "Terraform $PlanMode Plan" -TimeoutSeconds $WaitTimeoutSeconds -Condition {
        $candidate = Get-PlanForStack -StackName $StackName -PlanMode $PlanMode -ExcludeName $ExcludeName
        if ($candidate -and (Get-Field $candidate.status 'executionOutcome') -in @('ChangesPresent', 'NoChange') -and (Get-Field $candidate.status 'artifactsReady') -and (Get-Field $candidate.status 'evidenceCaptured') -and (Get-Condition $candidate).status -eq 'True') { return $candidate }
        if ($candidate -and $candidate.status.executionOutcome -in @('Failed', 'Indeterminate', 'Rejected')) { Stop-Regression "Terraform plan failed: $($candidate.status.executionOutcome) $($candidate.status.conditions | ConvertTo-Json -Compress)" }
        return $null
    }
    if ($plan.status.executionOutcome -notin @('ChangesPresent', 'NoChange')) { throw "Expected terminal Plan outcome, got $($plan.status.executionOutcome)" }
    Save-ImageEvidence
    return $plan
}

function New-Approval {
    param($Plan, [string]$Name, [switch]$WrongUID)
    if ($Plan.status.executionOutcome -ne 'ChangesPresent') { throw "Approval requested for non-mutating plan $($Plan.metadata.name)" }
    $approval = [ordered]@{ apiVersion = 'platform.example.io/v1alpha1'; kind = 'ChangeApproval'; metadata = [ordered]@{ name = $Name; namespace = 'platform-system' }; spec = [ordered]@{ planRunRef = [ordered]@{ name = $Plan.metadata.name }; planRunUID = $Plan.metadata.uid; planDigest = $Plan.status.planDigest; executionContextDigest = $Plan.spec.executionContextDigest; effectivePlanInputDigest = $Plan.status.effectivePlanInputDigest; planReportRef = $Plan.status.planReportRef; planReportDigest = $Plan.status.planReportDigest } }
    if ($WrongUID) { $approval.spec.planRunUID = [guid]::NewGuid().ToString() }
    Write-KubeObject -Object $approval
    $approvalName = $Name
    return (Wait-Until -Name "approval/$approvalName" -TimeoutSeconds 120 -Condition { $a = Get-KubeJson -Arguments @('get', 'changeapproval', $approvalName, '-n', 'platform-system'); if ($a -and $a.spec -and $a.spec.planRunUID) { return $a }; return $null })
}

function Wait-Apply {
    param([string]$StackName, [string]$PlanUID)
    return Wait-Until -Name 'Terraform Apply' -TimeoutSeconds $WaitTimeoutSeconds -Condition {
        $runs = Get-KubeJson -Arguments @('get', 'terraformruns', '-n', 'platform-system')
        $apply = @($runs.items) | Where-Object { $_.spec.stackRef.name -eq $StackName -and $_.spec.operation -eq 'Apply' -and $_.spec.planRunUID -eq $PlanUID } | Sort-Object { $_.metadata.creationTimestamp } -Descending | Select-Object -First 1
        if ($apply -and $apply.status.executionOutcome -eq 'Succeeded') { return $apply }
        if ($apply -and $apply.status.executionOutcome -in @('Failed', 'Indeterminate', 'Rejected')) { Stop-Regression "Terraform Apply failed: $($apply.status.executionOutcome) $($apply.status.conditions | ConvertTo-Json -Compress)" }
        return $null
    }
}

function Read-ArtifactJson {
    param([Parameter(Mandatory)][string]$Key)
    $remotePath = '/tmp/pcp-local-e2e-terminal-' + ([guid]::NewGuid().ToString('N')) + '.json'
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
    return Wait-Until -Name 'Terraform Destroy Apply' -TimeoutSeconds $WaitTimeoutSeconds -Condition {
        $runs = Get-KubeJson -Arguments @('get', 'terraformruns', '-n', 'platform-system')
        $apply = @($runs.items) | Where-Object { $_.spec.stackRef.name -eq $StackName -and $_.spec.operation -eq 'Apply' -and $_.spec.planRunUID -eq $PlanUID } | Sort-Object { $_.metadata.creationTimestamp } -Descending | Select-Object -First 1
        if ($apply) {
            $script:ObservedDestroyApplyUID = [string]$apply.metadata.uid
            if ($apply.status.executionOutcome -eq 'Succeeded') { return $apply }
            if ($apply.status.executionOutcome -in @('Failed', 'Indeterminate', 'Rejected')) { Stop-Regression "Terraform Destroy Apply failed: $($apply.status.executionOutcome) $($apply.status.conditions | ConvertTo-Json -Compress)" }
        }
        if ($script:ObservedDestroyApplyUID) {
            $terminal = Read-ArtifactJson -Key ("runs/{0}/terminal-result.json" -f $script:ObservedDestroyApplyUID)
            if ($terminal) {
                if ($terminal.executionOutcome -eq 'Succeeded') { return [pscustomobject]@{ status = [pscustomobject]@{ executionOutcome = 'Succeeded' }; terminalResult = $terminal } }
                if ($terminal.executionOutcome -in @('Failed', 'Indeterminate', 'Rejected')) { Stop-Regression "Terraform Destroy terminal result failed: $($terminal.executionOutcome) $($terminal.error)" }
            }
        }
        return $null
    }
}

function Wait-PlanAndApprove {
    param([string]$StackName, [string]$ApprovalName, [string]$PlanMode = 'Reconcile', [string]$ExcludeName = '')
    $plan = Wait-Plan -StackName $StackName -PlanMode $PlanMode -ExcludeName $ExcludeName
    Assert-PlanEvidence -Plan $plan
    if ($plan.status.executionOutcome -eq 'NoChange') { return [pscustomobject]@{ Plan = $plan; Approval = $null; Apply = $null } }
    $approval = New-Approval -Plan $plan -Name $ApprovalName
    $apply = Wait-Apply -StackName $StackName -PlanUID $plan.metadata.uid
    Assert-ApprovalBinding -Approval $approval -Plan $plan
    Assert-SavedPlanApply -Apply $apply -Plan $plan -Approval $approval
    return [pscustomobject]@{ Plan = $plan; Approval = $approval; Apply = $apply }
}

function Assert-TargetService {
    param([string]$Context, [string]$Name, [bool]$Present)
    $config = if ($Context -eq $script:TargetAContext) { $script:TargetKubeconfigA } elseif ($Context -eq $script:TargetBContext) { $script:TargetKubeconfigB } else { throw "Unknown owned target context: $Context" }
    $service = Get-KubeJson -Kubeconfig $config -Arguments @('get', 'service', $Name, '-n', 'default', '--ignore-not-found=true')
    if (($null -ne $service) -ne $Present) { throw "target service $Name on $Context presence=$($null -ne $service) expected=$Present" }
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
    $text = Invoke-Tool -File 'docker' -Arguments @('exec', $script:LocalStackContainer, 'awslocal', 's3api', 'head-bucket', '--bucket', $Bucket) -AllowedExitCodes @(0, 1, 2, 255)
    $code = $LASTEXITCODE
    if ($Present -and $code -ne 0) { throw "LocalStack bucket $Bucket missing: $text" }
    if (-not $Present -and ($code -eq 0 -or $text -notmatch '404|NoSuchBucket|Not Found')) { throw "LocalStack bucket $Bucket did not return a confirmed not-found result: $text" }
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
    else { Write-Stage "Restarting leader manager Pod $pod to verify reconstruction from persisted state" }
    Invoke-Kubectl -Arguments @('delete', 'pod', $pod, '-n', 'platform-system', '--wait=false') | Out-Null
    Wait-Until -Name 'manager restart recovery' -TimeoutSeconds 180 -Condition {
        $currentLease = Get-KubeJson -Arguments @('get', 'lease', 'platform-control-plane.platform.example.io', '-n', 'platform-system')
        $deployment = Get-KubeJson -Arguments @('get', 'deployment', 'platform-control-plane', '-n', 'platform-system')
        if ($currentLease.spec.holderIdentity -and $currentLease.spec.holderIdentity -ne $previousHolder -and $deployment.status.readyReplicas -eq 2 -and $deployment.status.availableReplicas -eq 2) { return $deployment }
        return $null
    } | Out-Null
}

function Delete-EnvironmentLifecycle {
    param([string]$EnvironmentName, [string]$StackName, [string]$Bucket, [string]$RuntimeName, [string]$TargetContext = $script:TargetAContext, [switch]$DeleteViaApi)
    Write-Stage "Deleting $EnvironmentName and completing destroy lifecycle"
    $ownedRuns = Get-KubeJson -Arguments @('get', 'terraformruns', '-n', 'platform-system')
    $ownedRunNames = @($ownedRuns.items | Where-Object { $_.spec.stackRef.name -eq $StackName } | ForEach-Object { $_.metadata.name })
    if ($DeleteViaApi) {
        $environment = Get-KubeJson -Arguments @('get', 'platformenvironment', $EnvironmentName, '-n', 'platform-system')
        Invoke-Api -Path "/api/environments/platform-system/$EnvironmentName" -Method DELETE -Body @{ uid = $environment.metadata.uid } -ExpectedStatus 202 | Out-Null
    } else { Invoke-Kubectl -Arguments @('delete', 'platformenvironment', $EnvironmentName, '-n', 'platform-system', '--wait=false') | Out-Null }
    Write-Stage "Waiting for ResourceSet prune $RuntimeName"
    Wait-Until -Name "ResourceSet prune $RuntimeName" -TimeoutSeconds 420 -Condition { try { Assert-TargetService -Context $TargetContext -Name $RuntimeName -Present $false; return $true } catch { return $null } } | Out-Null
    Write-Stage "Waiting for Terraform Destroy Plan"
    $destroy = Wait-Plan -StackName $StackName -PlanMode 'Destroy'
    Assert-PlanEvidence -Plan $destroy
    $deleting = Get-KubeJson -Arguments @('get', 'platformenvironment', $EnvironmentName, '-n', 'platform-system')
    if (-not $deleting.metadata.deletionTimestamp -or @($deleting.metadata.finalizers).Count -eq 0) { throw 'Deletion was not held by finalizers' }
    Assert-NoApply -StackName $StackName -PlanUID $destroy.metadata.uid -Seconds 8
    if ($destroy.status.executionOutcome -eq 'ChangesPresent') {
        Write-Stage "Approving and waiting for Terraform Destroy Apply"
        $approval = New-Approval -Plan $destroy -Name "$EnvironmentName-destroy-approval"
        Assert-ApprovalBinding -Approval $approval -Plan $destroy
        $apply = Wait-DestroyApply -StackName $StackName -PlanUID $destroy.metadata.uid
        $terminal = if ($apply.PSObject.Properties['terminalResult']) { $apply.terminalResult } else { Read-ArtifactJson -Key $apply.status.terminalResultRef }
        if (-not $terminal -or $terminal.planDigest -ne $destroy.status.planDigest -or $terminal.planRef -ne $destroy.status.planRef -or $terminal.terraformExitCode -ne 0) { throw 'Destroy did not consume the independently approved saved plan' }
        Save-Proof -Name "$EnvironmentName-destroy-terminal" -Value $terminal
        if ($apply.status.executionOutcome -ne 'Succeeded') { throw "destroy Apply not succeeded" }
    }
    Write-Stage "Waiting for LocalStack managed bucket removal"
    Wait-Until -Name "LocalStack destroy $Bucket" -TimeoutSeconds 600 -Condition { try { Assert-LocalBucket -Bucket $Bucket -Present $false; return $true } catch { return $null } } | Out-Null
    Write-Stage "Waiting for PlatformEnvironment finalizer removal"
    Wait-Deletion -Resource 'platformenvironment' -Name $EnvironmentName
    Wait-Deletion -Resource 'infrastack' -Name $StackName
    Wait-Deletion -Resource 'resourceset' -Name "$EnvironmentName-resources"
    Wait-Until -Name 'owned TerraformRuns and Jobs garbage collection' -TimeoutSeconds 180 -Condition {
        $runs = Get-KubeJson -Arguments @('get', 'terraformruns', '-n', 'platform-system')
        $jobs = Get-KubeJson -Arguments @('get', 'jobs', '-n', 'platform-system')
        if (@($runs.items | Where-Object { $_.spec.stackRef.name -eq $StackName }).Count -eq 0 -and
            @($jobs.items | Where-Object { (Get-Field $_.metadata.labels 'platform.example.io/terraform-run') -in $ownedRunNames -or $_.metadata.name -like ($StackName + '-destroy-*') }).Count -eq 0) { return $true }
        return $null
    } | Out-Null
}

function Invoke-FullLifecycle {
    param([switch]$DoRecovery)
    $suffix = $script:RunId.ToLower().Replace('-', '')
    $envName = "local-env-$suffix"
    $className = "local-class-$suffix"
    $updatedClassName = "local-class-v2-$suffix"
    $runtimeName = "local-svc-$suffix"
    $backendName = "local-backend-$suffix"
    $bucket = "pcp-local-e2e-$($suffix.Substring(0, [Math]::Min(45, $suffix.Length)))"
    $script:ArtifactPrefix = "local/$suffix"
    $script:OwnedBuckets.Add($bucket)
    Save-Ownership
    New-BackendConfig -Name $backendName -Key "$($script:ArtifactPrefix)/$envName.tfstate"
    $script:SourceA = New-GitSource -TargetContext $script:TargetAContext -BucketName $bucket -Name 'source-a'
    Start-SourceServer
    New-EnvironmentClass -Name $className -Source $script:SourceA -BackendConfigName $backendName -RuntimeName $runtimeName
    New-EnvironmentClass -Name $updatedClassName -Source $script:SourceA -BackendConfigName $backendName -RuntimeName $runtimeName -RuntimeRevision 'v2'
    New-Environment -Name $envName -ClassName $className
    $stack = Wait-Until -Name 'InfraStack creation' -TimeoutSeconds 180 -Condition { $s = Get-StackForEnvironment -EnvironmentName $envName; if ($s) { return $s }; return $null }
    Assert-MaterializedStack -Stack $stack -EnvironmentName $envName -ClassName $className -Source $script:SourceA -BackendName $backendName
    Add-Result -Name 'AUTO_INFRASTACK' -Status 'PASS' -Evidence 'PlatformEnvironment controller materialized source/workspace/backend/identity/capacity; no manual InfraStack'
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
    Assert-PlanEvidence -Plan $initialPlan
    if ($DoRecovery) {
        Restart-Manager
        $recoveredPlan = Get-KubeJson -Arguments @('get', 'terraformrun', $initialPlan.metadata.name, '-n', 'platform-system')
        $planJobs = Get-KubeJson -Arguments @('get', 'jobs', '-n', 'platform-system', '-l', "platform.example.io/terraform-run-uid=$($initialPlan.metadata.uid)")
        if ($recoveredPlan.metadata.uid -ne $initialPlan.metadata.uid -or $recoveredPlan.status.planDigest -ne $initialPlan.status.planDigest -or
            $recoveredPlan.status.sourceBundleDigest -ne $initialPlan.status.sourceBundleDigest -or @($planJobs.items).Count -ne 1) { throw 'Restart after completed Plan changed evidence or duplicated execution' }
        Save-Proof -Name 'recovery-completed-plan' -Value @{ before = $initialPlan; after = $recoveredPlan; jobs = $planJobs }
        Add-Result -Name 'RECOVERY_AFTER_PLAN' -Status PASS -Evidence 'Completed Plan UID/digest/source and single runner Job persisted across manager leader restart'
    }
    Wait-StackApproval -StackName $stack.metadata.name
    Assert-TargetService -Context $script:TargetAContext -Name $runtimeName -Present $false
    $invalidApproval = New-Approval -Plan $initialPlan -Name "$envName-invalid" -WrongUID
    Assert-NoApply -StackName $stack.metadata.name -Seconds 8
    Assert-TargetService -Context $script:TargetAContext -Name $runtimeName -Present $false
    $approval = New-Approval -Plan $initialPlan -Name "$envName-approval"
    Assert-ApprovalBinding -Approval $approval -Plan $initialPlan
    Add-Result -Name 'INVALID_APPROVAL_FENCE' -Status 'PASS' -Evidence 'Wrong Plan UID persisted but unlocked no Apply or runtime mutation'
    $apply = Wait-Apply -StackName $stack.metadata.name -PlanUID $initialPlan.metadata.uid
    Assert-SavedPlanApply -Apply $apply -Plan $initialPlan -Approval $approval
    Wait-Condition -Resource 'infrastack' -Name $stack.metadata.name -Reason 'InfrastructureReady' -TimeoutSeconds $WaitTimeoutSeconds | Out-Null
    Assert-LocalBucket -Bucket $bucket -Present $true
    Assert-Convergence -StackName $stack.metadata.name -Apply $apply -TargetContext $script:TargetAContext
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
        Add-Result -Name 'RESTART_RECOVERY' -Status 'PASS' -Evidence 'Leader restarted during active Plan, after completed Plan and after Ready; one Plan Job, one Apply, stable discovery/inventory/approval/runtime'
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
    $bucket = "pcp-local-e2e-multi-$($suffix.Substring(0, [Math]::Min(38, $suffix.Length)))"
    $script:OwnedBuckets.Add("$bucket-a")
    $script:OwnedBuckets.Add("$bucket-b")
    Save-Ownership
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
    Wait-Condition -Resource 'platformenvironment' -Name $envA -Reason 'EnvironmentReady' -TimeoutSeconds $WaitTimeoutSeconds | Out-Null; Wait-Condition -Resource 'platformenvironment' -Name $envB -Reason 'EnvironmentReady' -TimeoutSeconds $WaitTimeoutSeconds | Out-Null
    Wait-TargetService -Context $script:TargetAContext -Name $svcA -Present $true; Wait-TargetService -Context $script:TargetBContext -Name $svcB -Present $true
    Assert-TargetService -Context $script:TargetBContext -Name $svcA -Present $false; Assert-TargetService -Context $script:TargetAContext -Name $svcB -Present $false
    Add-Result -Name 'MULTI_TARGET' -Status 'PASS' -Evidence "A=$($script:TargetAContext)/$svcA B=$($script:TargetBContext)/$svcB"
    Delete-EnvironmentLifecycle -EnvironmentName $envA -StackName $stackA.metadata.name -Bucket "$bucket-a" -RuntimeName $svcA -TargetContext $script:TargetAContext
    Delete-EnvironmentLifecycle -EnvironmentName $envB -StackName $stackB.metadata.name -Bucket "$bucket-b" -RuntimeName $svcB -TargetContext $script:TargetBContext
}

function New-FailClosedResourceSet {
    param([string]$Name, [hashtable]$Target, [hashtable]$Identity, [hashtable]$Profile, [bool]$IncludeDiscovery, [switch]$MutationFence)
    $runtime = [ordered]@{ version = 'v1'; resource = 'services'; readinessPolicy = 'RequireReady'; deletionPolicy = 'Prune'; ownershipID = $Name; object = (New-RuntimeServiceObject -Name "$Name-side-effect") }
    $spec = [ordered]@{ target = $Target; trustedRuntimeTargetIdentity = $Identity; trustedTargetConnectionProfile = $Profile; runtimeMutationAllowed = $true; mutationFence = [bool]$MutationFence; resources = @($runtime) }
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
    $fencedName = "failclosed-fenced-$($script:RunId.Replace('-', ''))"
    New-FailClosedResourceSet -Name $fencedName -Target $target -Identity $identity -Profile $profile -IncludeDiscovery $false -MutationFence
    $fenced = Wait-Until -Name 'durable runtime mutation fence' -TimeoutSeconds 180 -Condition {
        $object = Get-KubeJson -Arguments @('get', 'resourceset', $fencedName, '-n', 'platform-system')
        $condition = Get-Condition $object
        if ($condition -and $condition.reason -eq 'RuntimeMutationBlocked' -and $object.status.mutationBlocked) { return $object }
        return $null
    }
    Assert-TargetService -Context $script:TargetAContext -Name "$fencedName-side-effect" -Present $false
    Assert-TargetService -Context $script:TargetBContext -Name "$fencedName-side-effect" -Present $false
    Save-Proof -Name 'failclosed-runtime' -Value @{ wrongTarget = $wrong; missingDiscovery = $invalid; mutationFence = $fenced }
    Remove-KubeObject -Arguments @('delete', 'resourceset', $fencedName, '-n', 'platform-system')
    Add-Result -Name 'FAIL_CLOSED' -Status 'PASS' -Evidence 'Unregistered target, incomplete trusted discovery and durable mutation fence produced no runtime mutation; approval rejection is covered by lifecycle/platform'
}

function Save-Snapshot {
    try { Invoke-Kubectl -Arguments @('get', 'platformenvironments,infrastacks,terraformruns,resourcesets,changeapprovals', '-A', '-o', 'wide') -LogName 'management-resources.txt' | Out-Null } catch {}
    try { Invoke-Kubectl -Arguments @('get', 'pods,jobs', '-n', 'platform-system', '-o', 'wide') -LogName 'management-workloads.txt' | Out-Null } catch {}
    try { Invoke-Kubectl -Kubeconfig $script:TargetKubeconfigA -Arguments @('get', 'all', '-A', '-o', 'wide') -LogName 'target-a-resources.txt' | Out-Null } catch {}
    try { Invoke-Kubectl -Kubeconfig $script:TargetKubeconfigB -Arguments @('get', 'all', '-A', '-o', 'wide') -LogName 'target-b-resources.txt' | Out-Null } catch {}
    try { Invoke-Kubectl -Arguments @('logs', 'deployment/platform-control-plane', '-n', 'platform-system', '--all-containers=true', '--tail=300') -LogName 'manager.log' | Out-Null } catch {}
}

function Save-FailureDiagnostics {
    param([string]$Failure, $FailureRecord)
    # Summaries retain the original regression diagnostics. Detailed dumps are
    # restricted to the last observed resource, owned environments and Jobs.
    Save-Snapshot
    # Memory pressure can also prevent diagnostic allocation. It must not hide
    # the original failure or prevent finally from attempting owned cleanup.
    try {
        Save-Proof -Name 'failure' -Value @{ message = $Failure; lastObserved = $script:LastObservedState;
            focusedResource = $script:DiagnosticResource; exceptionType = $FailureRecord.Exception.GetType().FullName;
            scriptStackTrace = $FailureRecord.ScriptStackTrace; position = $FailureRecord.InvocationInfo.PositionMessage }
    } catch {}
    if ($env:OS -eq 'Windows_NT') {
        try {
            $os = Get-CimInstance Win32_OperatingSystem
            Save-Proof -Name 'failure-host-memory' -Value @{ totalPhysicalKiB = $os.TotalVisibleMemorySize;
                freePhysicalKiB = $os.FreePhysicalMemory; totalVirtualKiB = $os.TotalVirtualMemorySize; freeVirtualKiB = $os.FreeVirtualMemory }
        } catch {}
    }
    if ($script:DiagnosticResource) {
        $focus = $script:DiagnosticResource
        foreach ($mode in @('get', 'describe')) {
            try {
                $args = @($mode, $focus.resource, $focus.name, '-n', 'platform-system', '--request-timeout=10s')
                if ($mode -eq 'get') { $args += @('-o', 'yaml') }
                Invoke-Kubectl -Arguments $args -Kubeconfig $focus.kubeconfig -LogName "failure-focused-$mode.txt" | Out-Null
            } catch {}
        }
    }
    if ($script:ManagementKubeconfig -and (Test-Path -LiteralPath $script:ManagementKubeconfig)) {
        try { Invoke-Kubectl -Arguments @('get', 'events', '-n', 'platform-system', '--sort-by=.lastTimestamp', '--request-timeout=10s') -LogName 'failure-events.txt' | Out-Null } catch {}
        try {
            $jobs = Get-KubeJson -Arguments @('get', 'jobs', '-n', 'platform-system', '--request-timeout=10s')
            foreach ($job in @($jobs.items | Where-Object { (Get-Field $_.status 'active') -or (Get-Field $_.status 'failed') } | Select-Object -Last 3)) {
                Invoke-Kubectl -Arguments @('describe', 'job', $job.metadata.name, '-n', 'platform-system', '--request-timeout=10s') -LogName ("failure-job-$($job.metadata.name).txt") | Out-Null
                Invoke-Kubectl -Arguments @('logs', "job/$($job.metadata.name)", '-n', 'platform-system', '--all-containers=true', '--tail=200', '--request-timeout=10s') -LogName ("failure-runner-$($job.metadata.name).log") -AllowedExitCodes @(0, 1) | Out-Null
            }
        } catch {}
    }
    if ($script:LocalStackContainer -and $Failure -match '(?i)LocalStack|artifact|S3|bucket|STS') {
        try { Invoke-Tool -File docker -Arguments @('logs', '--tail', '150', $script:LocalStackContainer) -LogName 'failure-localstack.log' | Out-Null } catch {}
    }
    if ($script:ManagementKubeconfig -and $Failure -match '(?i)no such host|DNS|registry\.terraform\.io') {
        try { Invoke-Kubectl -Arguments @('logs', 'deployment/coredns', '-n', 'kube-system', '--tail=100', '--request-timeout=10s') -LogName 'failure-coredns.log' | Out-Null } catch {}
        try { Invoke-Tool -File docker -Arguments @('exec', ($script:ManagementCluster + '-control-plane'), 'cat', '/etc/resolv.conf') -LogName 'failure-node-resolv.conf' | Out-Null } catch {}
    }
}

function Cleanup {
    Write-Stage 'Cleaning only ownership-recorded resources from this run'
    Save-Ownership
    $record = Get-Content -LiteralPath $script:OwnershipPath -Raw | ConvertFrom-Json
    Remove-RecordedRun -Record $record -KeepState:$KeepArtifacts
}

function Assert-OwnershipRecord {
    param($Record)
    if ($Record.owner -ne 'kpcp-local-e2e' -or $Record.runId -notmatch '^\d{14}-[a-f0-9]{8}$') { throw 'Invalid local E2E ownership record' }
    $expectedRoot = [IO.Path]::GetFullPath((Join-Path $script:RepoRoot ('tmp/pcp-local-e2e-' + $Record.runId)))
    if ([IO.Path]::GetFullPath($Record.stateRoot) -ne $expectedRoot) { throw 'Ownership state path is outside its exact workspace run directory' }
    $suffix = $Record.runId.Replace('-', '')
    foreach ($cluster in $Record.clusters) { if ($cluster -notin @("pcp-local-e2e-$suffix-management", "pcp-local-e2e-$suffix-target-a", "pcp-local-e2e-$suffix-target-b")) { throw "Refusing unrelated cluster: $cluster" } }
    foreach ($bucket in $Record.buckets) { if ($bucket -notmatch '^pcp-local-e2e-[a-z0-9-]+$' -or $bucket -notlike "*$suffix*" -or $bucket.Length -gt 63) { throw "Refusing unrelated bucket: $bucket" } }
    if ($Record.ownedGitImage -and $Record.ownedGitImage -ne "pcp-local-e2e-git:$($Record.runId)") { throw 'Invalid owned Git image' }
    if ($Record.originalGitImageID -and $Record.originalGitImageID -notmatch '^sha256:[a-f0-9]{64}$') { throw 'Invalid original Git image identity' }
    if ($Record.ownsLocalStack) {
        $uri = [Uri]$Record.localStackEndpoint
        if ($uri.Host -notin @('localhost', '127.0.0.1', '::1') -or $Record.localStackContainer -ne "pcp-local-e2e-localstack-$($uri.Port)") { throw 'Invalid owned LocalStack container' }
    }
}

function Remove-RecordedRun {
    param($Record, [switch]$KeepState)
    Assert-OwnershipRecord $Record
    $errors = New-Object System.Collections.Generic.List[string]
    foreach ($entry in $Record.processes) {
        try {
            $identity = Get-RecordedProcessIdentity -ProcessId $entry.id -StartedAt $entry.startedAt -Path $entry.path
            if ($identity.Status -eq 'Exited') { continue }
            if ($identity.Status -ne 'Match') {
                Write-Stage "Preserving PID $($entry.id); ownership identity no longer matches: $($identity.Reason)"
                continue
            }
            Stop-Process -Id $identity.Process.Id -ErrorAction Stop
            $deadline = [DateTime]::UtcNow.AddSeconds(10)
            do {
                Start-Sleep -Milliseconds 100
                $identity = Get-RecordedProcessIdentity -ProcessId $entry.id -StartedAt $entry.startedAt -Path $entry.path
            } while ($identity.Status -eq 'Match' -and [DateTime]::UtcNow -lt $deadline)
            if ($identity.Status -eq 'Match') { throw "Run-owned process PID $($entry.id) is still running at '$($entry.path)' after stop" }
        } catch { $errors.Add("process PID $($entry.id) ('$($entry.path)'): $($_.Exception.Message)") }
    }
    foreach ($bucket in $Record.buckets) {
        try { Invoke-Tool -File docker -Arguments @('exec', $Record.localStackContainer, 'awslocal', 's3', 'rb', "s3://$bucket", '--force') -AllowedExitCodes @(0, 1, 2, 255) | Out-Null } catch { $errors.Add($_.Exception.Message) }
    }
    foreach ($cluster in $Record.clusters) {
        try { Invoke-Tool -File kind -Arguments @('delete', 'cluster', '--name', $cluster) | Out-Null } catch { $errors.Add($_.Exception.Message) }
    }
    if ($Record.ownedGitImage) {
        try {
            $ownedID = Invoke-Tool -File docker -Arguments @('image', 'inspect', '--format', '{{.Id}}', $Record.ownedGitImage) -AllowedExitCodes @(0, 1)
            $aliasID = Invoke-Tool -File docker -Arguments @('image', 'inspect', '--format', '{{.Id}}', 'alpine/git:2.45.2') -AllowedExitCodes @(0, 1)
            if ($ownedID -match '^sha256:' -and $ownedID.Trim() -eq $aliasID.Trim()) {
                if ($Record.originalGitImageID) { Invoke-Tool -File docker -Arguments @('tag', $Record.originalGitImageID, 'alpine/git:2.45.2') | Out-Null }
                else { Invoke-Tool -File docker -Arguments @('image', 'rm', 'alpine/git:2.45.2') | Out-Null }
            }
            Invoke-Tool -File docker -Arguments @('image', 'rm', $Record.ownedGitImage) -AllowedExitCodes @(0, 1) | Out-Null
        } catch { $errors.Add($_.Exception.Message) }
    }
    if ($Record.ownsLocalStack) {
        try {
            $infoText = Invoke-Tool -File docker -Arguments @('inspect', $Record.localStackContainer) -AllowedExitCodes @(0, 1)
            if ($LASTEXITCODE -eq 0) {
                $info = ($infoText | ConvertFrom-Json)[0]
                if ((Get-Field $info.Config.Labels 'platform.example.io/local-e2e-run') -ne $Record.runId) { throw 'Refusing LocalStack cleanup: ownership label does not match' }
                Invoke-Tool -File docker -Arguments @('rm', '-f', $Record.localStackContainer) | Out-Null
            }
        } catch { $errors.Add($_.Exception.Message) }
    }
    if (-not $KeepState -and (Test-Path -LiteralPath $Record.stateRoot)) {
        try {
            # Assert-OwnershipRecord checks the final absolute target before recursion.
            $resolved = (Resolve-Path -LiteralPath $Record.stateRoot).Path
            if ($resolved -ne [IO.Path]::GetFullPath($Record.stateRoot) -or (Get-Item -LiteralPath $resolved).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Refusing cleanup through a reparse point or unexpected resolved state path' }
            Remove-Item -LiteralPath $resolved -Recurse -Force -ErrorAction Stop
        } catch { $errors.Add($_.Exception.Message) }
    }
    if ($errors.Count) { throw ($errors -join '; ') }
}

function Verify-Cleanup {
    foreach ($process in $script:StartedProcesses) {
        $process.Refresh()
        if (-not $process.HasExited) { throw "Temporary process remains: PID $($process.Id), executable '$($process.Path)'" }
    }
    $clusters = if ($script:OwnedClusters.Count) { Invoke-Tool -File kind -Arguments @('get', 'clusters') } else { '' }
    if (@($script:OwnedClusters | Where-Object { $clusters -split "`r?`n" -contains $_ }).Count) { throw 'Run-owned Kind clusters remain' }
    if ($script:OwnedBuckets.Count -and -not $script:OwnedLocalStack) {
        $buckets = Invoke-Tool -File docker -Arguments @('exec', $script:LocalStackContainer, 'awslocal', 's3api', 'list-buckets', '--query', 'Buckets[].Name', '--output', 'json') | ConvertFrom-Json
        if (@($script:OwnedBuckets | Where-Object { $_ -in $buckets }).Count) { throw 'Run-owned LocalStack buckets remain' }
    }
    if ($script:OwnedLocalStack) {
        $containers = Invoke-Tool -File docker -Arguments @('ps', '-a', '--format', '{{.Names}}')
        if ($containers -split "`r?`n" -contains $script:LocalStackContainer) { throw 'Run-owned LocalStack container remains' }
    }
    if (-not $KeepArtifacts -and (Test-Path -LiteralPath $script:StateRoot)) { throw 'Run-owned temporary state remains' }
}

function Write-Summary {
    $summary = @("run_id=$($script:RunId)", "suite=$Suite", "localstack_endpoint=$LocalStackEndpoint", 'real_aws_used=false',
        ('elapsed_seconds={0:N1}' -f $script:RunTimer.Elapsed.TotalSeconds),
        'E2E_COVERAGE_REDUCED=NO', 'API_CONTRACT_CHANGED=NO', 'CONTROL_PLANE_BEHAVIOR_CHANGED=NO',
        'APPROVAL_SEMANTICS_CHANGED=NO', 'TERRAFORM_EXECUTION_SEMANTICS_CHANGED=NO', 'REAL_AWS_USED=NO', 'results=',
        ($script:Results | ForEach-Object { ("$($_.Name)=$($_.Status) $($_.Evidence)").TrimEnd() }))
    $summary | Set-Content -LiteralPath (Join-Path $script:ArtifactRoot 'summary.txt') -Encoding UTF8
    Save-Proof -Name 'summary' -Value @{ runId = $script:RunId; suite = $Suite; elapsedSeconds = [Math]::Round($script:RunTimer.Elapsed.TotalSeconds, 1); suiteDurations = $script:SuiteDurations; results = @($script:Results.ToArray()); realAWSUsed = $false }
}

function Invoke-Clean {
    foreach ($name in @('docker', 'kind', 'kubectl')) { if (-not (Get-Command $name -ErrorAction SilentlyContinue)) { throw "Required cleanup tool missing: $name" } }
    $root = Join-Path $script:RepoRoot 'artifacts/e2e'
    $records = @(Get-ChildItem -LiteralPath $root -Filter ownership.json -File -Recurse)
    $count = 0
    foreach ($file in $records) {
        if ($file.FullName -eq $script:OwnershipPath) { continue }
        $record = Get-Content -LiteralPath $file.FullName -Raw | ConvertFrom-Json
        Assert-OwnershipRecord $record
        if ($file.Directory.Name -ne $record.runId) { throw 'Ownership file directory does not match run ID' }
        if ($record.cleanupCompleted -and -not (Test-Path -LiteralPath $record.stateRoot)) { continue }
        $ownerIdentity = Get-RecordedProcessIdentity -ProcessId $record.harnessPid -StartedAt $record.harnessStartedAt -Path (Get-Field $record 'harnessPath') -AllowLegacyPowerShellPath
        if ($ownerIdentity.Status -eq 'Match') {
            Write-Host "[WARN] Skipping active E2E run $($record.runId); release its browser hold file instead."
            continue
        }
        Write-Stage "Cleaning recorded inactive run $($record.runId)"
        Remove-RecordedRun -Record $record
        foreach ($entry in $record.processes) {
            $identity = Get-RecordedProcessIdentity -ProcessId $entry.id -StartedAt $entry.startedAt -Path $entry.path
            if ($identity.Status -eq 'Match') { throw "Recorded process remains: PID $($entry.id), executable '$($entry.path)'" }
        }
        if ($record.ownsLocalStack) {
            $containers = Invoke-Tool -File docker -Arguments @('ps', '-a', '--format', '{{.Names}}')
            if ($containers -split "`r?`n" -contains $record.localStackContainer) { throw 'Recorded LocalStack container remains' }
        }
        $clusters = Invoke-Tool -File kind -Arguments @('get', 'clusters')
        if (@($record.clusters | Where-Object { $clusters -split "`r?`n" -contains $_ }).Count -or (Test-Path -LiteralPath $record.stateRoot)) { throw 'Recorded run cleanup verification failed' }
        if ($record.buckets.Count -and -not $record.ownsLocalStack) {
            $buckets = Invoke-Tool -File docker -Arguments @('exec', $record.localStackContainer, 'awslocal', 's3api', 'list-buckets', '--query', 'Buckets[].Name', '--output', 'json') | ConvertFrom-Json
            if (@($record.buckets | Where-Object { $_ -in $buckets }).Count) { throw 'Recorded buckets remain' }
        }
        $record.cleanupCompleted = $true
        $record | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $file.FullName -Encoding UTF8
        $count++
    }
    Add-Result -Name 'CLEAN_MODE' -Status PASS -Evidence "Cleaned $count recorded inactive runs; external resources and live E2E runs preserved"
}

Save-Ownership
Push-Location $script:RepoRoot
try {
    if ($BrowserSession -and $Suite -eq 'clean') { throw 'BrowserSession cannot be combined with clean' }
    if ($Suite -eq 'browser' -and -not $BrowserSession) { throw 'Suite browser requires -BrowserSession' }
    if ($Suite -eq 'clean') {
        Invoke-Lane -Name clean -Action { Invoke-Clean }
    } else {
        Invoke-Lane -Name bootstrap -Action {
            Test-Dependencies -RequireDocker
            Test-LocalStack
            New-Clusters
            Build-Images
            Configure-TerraformDns
            $artifactSuffix = $script:RunId.ToLower().Replace('-', '')
            $script:ArtifactBucket = "pcp-local-e2e-artifacts-$artifactSuffix"
            $script:OwnedBuckets.Add($script:ArtifactBucket)
            Save-Ownership
            Invoke-Tool -File docker -Arguments @('exec', $script:LocalStackContainer, 'awslocal', 's3api', 'create-bucket', '--bucket', $script:ArtifactBucket) | Out-Null
            Install-Manager
        }
        if ($Suite -in @('all', 'lifecycle', 'recovery')) {
            $lane = if ($Suite -eq 'lifecycle') { 'lifecycle' } else { 'recovery' }
            Invoke-Lane -Name $lane -Action { Invoke-FullLifecycle -DoRecovery:($Suite -in @('all', 'recovery')) }
            if ($Suite -eq 'all') { Add-Result -Name SUITE_LIFECYCLE -Status PASS -Evidence 'Executed within the recovery lifecycle, including update and independent Destroy' }
        }
        if ($Suite -in @('all', 'multitarget')) { Invoke-Lane -Name multitarget -Action { Invoke-MultiTarget } }
        if ($Suite -in @('all', 'failclosed')) { Invoke-Lane -Name failclosed -Action { Invoke-FailClosed } }
        if ($Suite -in @('all', 'platform')) { Invoke-Lane -Name platform -Action { Invoke-Platform } }
        if ($BrowserSession) { Invoke-Lane -Name browser -Action { Wait-BrowserSession } }
    }
    $script:SuiteCompleted = $true
} catch {
    $failureRecord = $_
    Write-Host ('[FAIL] ' + $_.Exception.Message) -ForegroundColor Red
    Add-Result -Name LOCAL_E2E_EXECUTION -Status FAIL -Evidence $_.Exception.Message
    if ($_.Exception -is [OutOfMemoryException] -or $_.Exception.Message -match '(?i)paging file is too small|分页文件太小') {
        Add-Result -Name LOCAL_ENVIRONMENT -Status BLOCKED_LOCAL_ENVIRONMENT -Evidence 'Host memory allocation failed; this run does not establish a product failure'
    }
    Save-FailureDiagnostics -Failure $failureRecord.Exception.Message -FailureRecord $failureRecord
    $script:ExitCode = 1
} finally {
    try {
        Cleanup
        Verify-Cleanup
        $script:CleanupCompleted = $true
        Save-Ownership
        $stateEvidence = if ($KeepArtifacts) { 'temporary state retained by -KeepArtifacts; remove later with -Suite clean' } else { 'temporary state verified absent' }
        Add-Result -Name CLEANUP -Status PASS -Evidence "Owned processes, clusters, buckets and container (if created) verified absent; $stateEvidence"
    } catch {
        Add-Result -Name CLEANUP -Status FAIL -Evidence $_.Exception.Message
        $script:ExitCode = 1
    }
    $overall = if ($script:SuiteCompleted -and $script:ExitCode -eq 0) { 'PASS' } else { 'FAIL' }
    Add-Result -Name LOCAL_E2E -Status $overall -Evidence ('duration={0:N1}s; requested lanes and cleanup' -f $script:RunTimer.Elapsed.TotalSeconds)
    Write-Summary
    Pop-Location
}
exit $script:ExitCode
