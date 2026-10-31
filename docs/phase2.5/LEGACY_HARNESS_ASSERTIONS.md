# Legacy harness extraction

Historical source: `9d149cc82a3cec21998ae4a780a444c631cad6b4:e2e/Run-LocalE2E.ps1` (1678 physical lines, SHA-256 `aa7c90fcc9b631780424a7715f5b283da1f3ccabb47077fdd4ee350a49530e32`). Retrieve with `git show 9d149cc82a3cec21998ae4a780a444c631cad6b4:e2e/Run-LocalE2E.ps1`. This is an assertion inventory, not executable replacement coverage.

## Execution requirements and ownership contract

The removed harness used PowerShell, Docker, Kind, kubectl, Git and Python; one management and two target clusters; two manager replicas/leader election; pinned runner Terraform 1.14.0 and AWS provider 5.100.0; local HTTP S3/STS with dummy credentials; a Git commit fixture server; separate backend state keys and workspaces; current-generation Ready checks; and time-bounded observations. It optionally patched owned-cluster DNS and temporarily aliased a Git image. These infrastructure preparation operations must stay outside the Go Operator and successor test lifecycle.

A supplied-cluster successor must record run ownership before creation; use exact resource UID, namespace and run labels for cleanup; leave external clusters/emulators/buckets and prior evidence untouched; never remove finalizers or destroy unrelated state. Failed or unapproved lifecycle cleanup must remain pending with retained evidence. The old recovery cleanup validated canonical paths, run IDs, bucket names, process path/start time and image IDs before deleting only run-owned items; PID reuse and live-run checks prevented unrelated process kills. Do not transplant host/container cleanup into a new framework. Capture source/artifact/image identity and scoped diagnostics; redact secrets and preserve the primary failure if diagnostic capture fails.

## Function inventory

- L93: `Write-Stage`
- L98: `Add-Result`
- L104: `Invoke-Tool`
- L127: `Invoke-Kubectl`
- L138: `Get-KubeJson`
- L150: `Write-KubeObject`
- L158: `Remove-KubeObject`
- L163: `Wait-Until`
- L190: `Get-Condition`
- L196: `Get-Field`
- L204: `Stop-Regression`
- L211: `Save-Proof`
- L216: `Get-FreeLocalPort`
- L221: `Get-ContainerEndpoint`
- L227: `Save-Ownership`
- L245: `ConvertTo-ProcessStartMilliseconds`
- L257: `Get-RecordedProcessIdentity`
- L293: `Start-PlatformApi`
- L327: `Invoke-Api`
- L352: `Wait-Deletion`
- L361: `Wait-StackApproval`
- L371: `Assert-NoApply`
- L382: `Assert-MaterializedStack`
- L395: `Assert-Artifact`
- L405: `Assert-PlanEvidence`
- L421: `Assert-ApprovalBinding`
- L430: `Assert-SavedPlanApply`
- L449: `Assert-Convergence`
- L461: `Invoke-Lane`
- L470: `Invoke-Platform`
- L598: `Wait-Condition`
- L612: `Test-Dependencies`
- L630: `Test-LocalStack`
- L674: `New-Clusters`
- L702: `Configure-TerraformDns`
- L748: `Build-Images`
- L787: `Save-SourceState`
- L796: `Save-ImageEvidence`
- L812: `Wait-BrowserSession`
- L849: `Prepare-GitImage`
- L864: `Merge-TargetKubeconfig`
- L891: `Install-Manager`
- L936: `New-GitSource`
- L965: `Start-SourceServer`
- L982: `New-BackendConfig`
- L1002: `New-RuntimeServiceObject`
- L1009: `New-EnvironmentClass`
- L1031: `New-Environment`
- L1038: `Get-StackForEnvironment`
- L1045: `Get-PlanForStack`
- L1051: `Wait-Plan`
- L1064: `New-Approval`
- L1074: `Wait-Apply`
- L1085: `Read-ArtifactJson`
- L1100: `Wait-DestroyApply`
- L1122: `Wait-PlanAndApprove`
- L1134: `Assert-TargetService`
- L1141: `Wait-TargetService`
- L1151: `Assert-LocalBucket`
- L1159: `Restart-Manager`
- L1182: `Delete-EnvironmentLifecycle`
- L1224: `Invoke-FullLifecycle`
- L1347: `Invoke-MultiTarget`
- L1375: `New-FailClosedResourceSet`
- L1383: `Invoke-FailClosed`
- L1415: `Save-Snapshot`
- L1423: `Save-FailureDiagnostics`
- L1471: `Cleanup`
- L1478: `Assert-OwnershipRecord`
- L1494: `Remove-RecordedRun`
- L1553: `Verify-Cleanup`
- L1571: `Write-Summary`
- L1581: `Invoke-Clean`

## Every explicit failure assertion

| Original line | Function | Original assertion |
|---|---|---|
| 25 | `initialization` | `if ([string]::IsNullOrWhiteSpace($OllamaEndpoint)) { throw '-OllamaEndpoint is required when -AIProvider ollama is selected' }` |
| 28 | `initialization` | `throw "Invalid -OllamaEndpoint '$OllamaEndpoint'; expected a loopback HTTP URL with an explicit port"` |
| 38 | `initialization` | `throw "Invalid -OllamaEndpoint '$OllamaEndpoint'; expected a loopback HTTP URL with an explicit port and no path, credentials, query, or fragment"` |
| 40 | `initialization` | `if ([string]::IsNullOrWhiteSpace($OllamaModel)) { throw '-OllamaModel is required when -AIProvider ollama is selected' }` |
| 122 | `Invoke-Tool` | `throw "Command failed ($exitCode): $File $($Arguments -join ' ')&#96;n$text"` |
| 176 | `Wait-Until` | `if ($_.Exception.Data['LocalE2ETerminalFailure']) { throw }` |
| 187 | `Wait-Until` | `throw "Timed out after ${TimeoutSeconds}s waiting for $WaitDescription. Last error: $errorSummary. Last observed state: $observedSummary"` |
| 204 | `Stop-Regression` | `function Stop-Regression {` |
| 208 | `Stop-Regression` | `throw $failure` |
| 319 | `Start-PlatformApi` | `if ($script:ApiProcess.HasExited) { Stop-Regression 'platform-api exited during startup; inspect its stderr/stdout logs' }` |
| 337 | `Invoke-Api` | `if ($null -eq $response) { throw }` |
| 346 | `Invoke-Api` | `if ($status -ne $ExpectedStatus) { throw "API expected HTTP $ExpectedStatus; $($script:LastObservedState)" }` |
| 377 | `Assert-NoApply` | `if ($applies.Count -ne 0) { throw 'Apply was created without valid approval of this exact Plan' }` |
| 391 | `Assert-MaterializedStack` | `$Stack.spec.capacity.nodeCount -ne 1 -or $Stack.spec.desiredState -ne 'Present') { throw 'EnvironmentClass materialization contract mismatch' }` |
| 397 | `Assert-Artifact` | `if ($Key -notmatch '^runs/[a-f0-9-]{36}/[^.][^\s]*$' -or $Key -match '\.\.' -or $Digest -notmatch '^sha256:[a-f0-9]{64}$') { throw 'Invalid artifact reference/digest in real run status' }` |
| 402 | `Assert-Artifact` | `if ($actual -ne $Digest) { throw "Artifact digest mismatch: $Key" }` |
| 409 | `Assert-PlanEvidence` | `-not $Plan.spec.infrastructureExecutionIdentityDigest -or -not $Plan.spec.runtimeTargetIdentityDigest) { throw 'Plan identity/evidence incomplete' }` |
| 417 | `Assert-PlanEvidence` | `$terminal.planDigest -ne $Plan.status.planDigest) { throw 'Plan runner Job/artifact identity mismatch' }` |
| 426 | `Assert-ApprovalBinding` | `$Approval.spec.planReportDigest -ne $Plan.status.planReportDigest) { throw 'Exact ChangeApproval binding mismatch' }` |
| 435 | `Assert-SavedPlanApply` | `$Apply.spec.source.digest -ne $Plan.status.sourceBundleDigest -or $Apply.spec.approvalRef.name -ne $Approval.metadata.name -or $Apply.spec.approvalUID -ne $Approval.metadata.uid) { throw 'Apply did not retain approved saved-plan/source/approval identity' }` |
| 444 | `Assert-SavedPlanApply` | `$terminal.planDigest -ne $Plan.status.planDigest -or $terminal.terraformExitCode -ne 0) { throw 'Runner did not execute the approved saved Plan' }` |
| 456 | `Assert-Convergence` | `$stack.status.discoveredRuntimeTargetIdentity.clusterName -ne $TargetContext) { throw 'Terraform output/trusted discovery propagation mismatch' }` |
| 466 | `Invoke-Lane` | `catch { Add-Result -Name ('SUITE_' + $Name.ToUpper()) -Status 'FAIL' -Evidence $_.Exception.Message; throw }` |
| 490 | `Invoke-Platform` | `if (-not $validation.valid -or (Get-KubeJson -Arguments @('get', 'environmentclass', $className, '--ignore-not-found=true'))) { throw 'Class validation was invalid or persisted its dry-run object' }` |
| 495 | `Invoke-Platform` | `if (-not $class.ready -or $class.uid -ne $createdClass.uid -or @($classes &#124; Where-Object name -eq $className).Count -ne 1) { throw 'EnvironmentClass API list/detail/Ready mismatch' }` |
| 502 | `Invoke-Platform` | `if ($duplicate.code -ne 'TemplateAlreadyExists') { throw 'Duplicate template contract mismatch' }` |
| 506 | `Invoke-Platform` | `$storedTemplate.metadata.uid -ne $template.uid -or ($storedTemplate.data.'template.json' &#124; ConvertFrom-Json).spec.source.revision -ne $script:SourceA.Revision) { throw 'Template ConfigMap persistence/list contract mismatch' }` |
| 508 | `Invoke-Platform` | `if ($rejectedDelete.code -ne 'TemplateConfirmationRequired') { throw 'Template deletion name confirmation was weakened' }` |
| 513 | `Invoke-Platform` | `(Get-KubeJson -Arguments @('get', 'configmap', "pcp-class-template-$templateName", '-n', 'default', '--ignore-not-found=true'))) { throw 'Template delete did not remove its identified ConfigMap' }` |
| 517 | `Invoke-Platform` | `if (@($before &#124; Where-Object name -eq $name).Count -ne 0) { throw 'Environment exists before explicit API submission' }` |
| 519 | `Invoke-Platform` | `if (-not $draft.valid -or (Get-KubeJson -Arguments @('get', 'platformenvironment', $name, '-n', 'platform-system', '--ignore-not-found=true'))) { throw 'Draft generated invalid intent or created an environment without submit' }` |
| 531 | `Invoke-Platform` | `if ($view.uid -ne $created.uid -or $view.infrastructureDetail.name -ne $stack.metadata.name -or $view.infrastructure -ne 'WaitingApproval' -or @($listed &#124; Where-Object name -eq $name).Count -ne 1) { throw 'Environment read-model/API identity or approval-state mismatch' }` |
| 533 | `Invoke-Platform` | `if ($inUse.code -ne 'ClassInUse') { throw 'API allowed deletion of a referenced class' }` |
| 542 | `Invoke-Platform` | `$visual.graph.nodes[0].address -ne 'aws_s3_bucket.lifecycle') { throw 'Real sanitized PlanGraph contract mismatch' }` |
| 548 | `Invoke-Platform` | `foreach ($property in $properties.PSObject.Properties.Name) { if ($property -notin $safeAttributes) { throw "Unexpected S3 attribute in sanitized graph: $property" } }` |
| 559 | `Invoke-Platform` | `if ($stale.code -ne 'StalePlan') { throw 'API accepted a stale Plan report digest' }` |
| 575 | `Invoke-Platform` | `-not $set.spec.runtimeMutationAllowed -or (Get-Field $set.spec 'mutationFence') -or @($set.status.inventory).Count -ne 1) { throw 'Runtime/ResourceSet/read model convergence mismatch' }` |
| 580 | `Invoke-Platform` | `if ($staleUpdate.code -ne 'StaleEnvironment') { throw 'Stale update generation was accepted' }` |
| 583 | `Invoke-Platform` | `if ($updated.status.executionOutcome -ne 'NoChange') { throw 'Capacity-only API update must be NoChange for the canonical fixture' }` |
| 587 | `Invoke-Platform` | `if ($updatedView.nodeCount -ne 2 -or $updatedView.generation -le $ready.generation -or @($runs &#124; Where-Object operation -eq Apply).Count -ne 1) { throw 'API capacity update duplicated Apply or lost desired generation' }` |
| 591 | `Invoke-Platform` | `if ($notFound.code -ne 'NotFound') { throw 'Deleted environment remained in the API' }` |
| 619 | `Test-Dependencies` | `if (-not (Get-Command $name -ErrorAction SilentlyContinue)) { throw "Required command not found: $name" }` |
| 624 | `Test-Dependencies` | `try { $listener.Start() } catch { throw "Requested API port $ApiPort is busy; free it or omit -ApiPort for an isolated port" } finally { $listener.Stop() }` |
| 633 | `Test-LocalStack` | `throw "LocalStack endpoint must be a local HTTP root URL, got $LocalStackEndpoint"` |
| 643 | `Test-LocalStack` | `if ($matches.Count -gt 1) { throw 'Multiple LocalStack containers publish the requested port' }` |
| 648 | `Test-LocalStack` | `if ($RequireExistingLocalStack) { throw "No running external LocalStack publishes $LocalStackEndpoint; -RequireExistingLocalStack forbids automatic container startup" }` |
| 651 | `Test-LocalStack` | `if ($existing -split "&#96;r?&#96;n" -contains $script:LocalStackContainer) { throw 'Reserved LocalStack container already exists but is not a running service on this endpoint; refusing to replace it' }` |
| 661 | `Test-LocalStack` | `if (-not $container.State.Running) { Stop-Regression "LocalStack exited (code $($container.State.ExitCode)); inspect localstack.log or select a usable -LocalStackImage" }` |
| 669 | `Test-LocalStack` | `if ($identity.Account -ne '000000000000') { throw 'Unexpected LocalStack account; refusing cloud execution' }` |
| 705 | `Configure-TerraformDns` | `if (-not [Net.IPAddress]::TryParse($TerraformDnsServer, [ref]$resolver)) { throw 'TerraformDnsServer must be a DNS server IP address' }` |
| 706 | `Configure-TerraformDns` | `if ($script:ManagementCluster -notin $script:OwnedClusters) { throw 'Refusing DNS configuration outside the owned management cluster' }` |
| 710 | `Configure-TerraformDns` | `if ($original -match '(?m)^registry\.terraform\.io') { throw 'Terraform DNS zone already configured; refusing to replace it' }` |
| 737 | `Configure-TerraformDns` | `Stop-Regression 'Terraform DNS preflight failed; see terraform-dns-probe.log'` |
| 762 | `Build-Images` | `if ($matches.Count -ne 1) { throw "Expected one canonical Go build command in $canonical" }` |
| 778 | `Build-Images` | `if ($version.terraform_version -ne '1.14.0') { throw 'Built runner has an unexpected Terraform version' }` |
| 932 | `Install-Manager` | `if ($ready.Trim() -ne '2') { throw "manager deployment is not fully ready: $ready" }` |
| 1056 | `Wait-Plan` | `if ($candidate -and $candidate.status.executionOutcome -in @('Failed', 'Indeterminate', 'Rejected')) { Stop-Regression "Terraform plan failed: $($candidate.status.executionOutcome) $($candidate.status.conditions &#124; ConvertTo-Json -Compress)" }` |
| 1059 | `Wait-Plan` | `if ($plan.status.executionOutcome -notin @('ChangesPresent', 'NoChange')) { throw "Expected terminal Plan outcome, got $($plan.status.executionOutcome)" }` |
| 1066 | `New-Approval` | `if ($Plan.status.executionOutcome -ne 'ChangesPresent') { throw "Approval requested for non-mutating plan $($Plan.metadata.name)" }` |
| 1080 | `Wait-Apply` | `if ($apply -and $apply.status.executionOutcome -in @('Failed', 'Indeterminate', 'Rejected')) { Stop-Regression "Terraform Apply failed: $($apply.status.executionOutcome) $($apply.status.conditions &#124; ConvertTo-Json -Compress)" }` |
| 1109 | `Wait-DestroyApply` | `if ($apply.status.executionOutcome -in @('Failed', 'Indeterminate', 'Rejected')) { Stop-Regression "Terraform Destroy Apply failed: $($apply.status.executionOutcome) $($apply.status.conditions &#124; ConvertTo-Json -Compress)" }` |
| 1115 | `Wait-DestroyApply` | `if ($terminal.executionOutcome -in @('Failed', 'Indeterminate', 'Rejected')) { Stop-Regression "Terraform Destroy terminal result failed: $($terminal.executionOutcome) $($terminal.error)" }` |
| 1136 | `Assert-TargetService` | `$config = if ($Context -eq $script:TargetAContext) { $script:TargetKubeconfigA } elseif ($Context -eq $script:TargetBContext) { $script:TargetKubeconfigB } else { throw "Unknown owned target context: $Context" }` |
| 1138 | `Assert-TargetService` | `if (($null -ne $service) -ne $Present) { throw "target service $Name on $Context presence=$($null -ne $service) expected=$Present" }` |
| 1155 | `Assert-LocalBucket` | `if ($Present -and $code -ne 0) { throw "LocalStack bucket $Bucket missing: $text" }` |
| 1156 | `Assert-LocalBucket` | `if (-not $Present -and ($code -eq 0 -or $text -notmatch '404&#124;NoSuchBucket&#124;Not Found')) { throw "LocalStack bucket $Bucket did not return a confirmed not-found result: $text" }` |
| 1163 | `Restart-Manager` | `if ($job.status.active -ne 1) { throw "Terraform Plan Job $ActivePlanName is not active at manager restart" }` |
| 1167 | `Restart-Manager` | `if (-not $leaderLease -or -not $leaderLease.spec.holderIdentity) { throw 'manager leader lease not found for restart recovery' }` |
| 1170 | `Restart-Manager` | `if ($pod -notlike 'platform-control-plane-*') { throw "unexpected manager leader identity: $previousHolder" }` |
| 1197 | `Delete-EnvironmentLifecycle` | `if (-not $deleting.metadata.deletionTimestamp -or @($deleting.metadata.finalizers).Count -eq 0) { throw 'Deletion was not held by finalizers' }` |
| 1205 | `Delete-EnvironmentLifecycle` | `if (-not $terminal -or $terminal.planDigest -ne $destroy.status.planDigest -or $terminal.planRef -ne $destroy.status.planRef -or $terminal.terraformExitCode -ne 0) { throw 'Destroy did not consume the independently approved saved plan' }` |
| 1207 | `Delete-EnvironmentLifecycle` | `if ($apply.status.executionOutcome -ne 'Succeeded') { throw "destroy Apply not succeeded" }` |
| 1256 | `Invoke-FullLifecycle` | `if ($initialPlan.status.executionOutcome -ne 'ChangesPresent') { throw "expected initial ChangesPresent, got $($initialPlan.status.executionOutcome)" }` |
| 1259 | `Invoke-FullLifecycle` | `if (@($jobs.items).Count -ne 1) { throw "manager restart created duplicate Plan Jobs for $($initialPlan.metadata.name)" }` |
| 1262 | `Invoke-FullLifecycle` | `if ($applyCountBeforeApproval -ne 0) { throw 'Apply was created before ChangeApproval' }` |
| 1269 | `Invoke-FullLifecycle` | `$recoveredPlan.status.sourceBundleDigest -ne $initialPlan.status.sourceBundleDigest -or @($planJobs.items).Count -ne 1) { throw 'Restart after completed Plan changed evidence or duplicated execution' }` |
| 1295 | `Invoke-FullLifecycle` | `if (-not $discoveryDigest -or -not $inventoryUID) { throw 'Ready environment lacks discovery or runtime inventory evidence before manager restart' }` |
| 1300 | `Invoke-FullLifecycle` | `if ($recoveredStack.status.targetDiscoveryDigest -ne $discoveryDigest -or [string](@($recoveredSet.status.inventory)[0].uid) -ne $inventoryUID) { throw 'manager restart changed trusted discovery or runtime inventory' }` |
| 1303 | `Invoke-FullLifecycle` | `if ($applyCount -ne 1) { throw "manager restart caused duplicate Apply count=$applyCount" }` |
| 1324 | `Invoke-FullLifecycle` | `if ($targetService.metadata.uid -ne $beforeService.metadata.uid -or [string](@($afterSet.status.inventory)[0].uid) -ne [string](@($beforeSet.status.inventory)[0].uid)) { throw 'runtime v2 update replaced the Service or inventory identity' }` |
| 1328 | `Invoke-FullLifecycle` | `if ($applyCountAfterRuntimeUpdate -ne 1) { throw "runtime-only update created unexpected Terraform Apply count=$applyCountAfterRuntimeUpdate" }` |
| 1338 | `Invoke-FullLifecycle` | `if ($updatedPlan.status.executionOutcome -eq 'ChangesPresent') { throw 'safe capacity update unexpectedly required Apply for fixture with no capacity variable' }` |
| 1339 | `Invoke-FullLifecycle` | `if ($updatedPlan.status.executionOutcome -ne 'NoChange') { throw "safe update expected NoChange, got $($updatedPlan.status.executionOutcome)" }` |
| 1341 | `Invoke-FullLifecycle` | `if ($applyCountAfterUpdate -ne 1) { throw "NoChange update created an unexpected Apply count=$applyCountAfterUpdate" }` |
| 1480 | `Assert-OwnershipRecord` | `if ($Record.owner -ne 'kpcp-local-e2e' -or $Record.runId -notmatch '^\d{14}-[a-f0-9]{8}$') { throw 'Invalid local E2E ownership record' }` |
| 1482 | `Assert-OwnershipRecord` | `if ([IO.Path]::GetFullPath($Record.stateRoot) -ne $expectedRoot) { throw 'Ownership state path is outside its exact workspace run directory' }` |
| 1484 | `Assert-OwnershipRecord` | `foreach ($cluster in $Record.clusters) { if ($cluster -notin @("pcp-local-e2e-$suffix-management", "pcp-local-e2e-$suffix-target-a", "pcp-local-e2e-$suffix-target-b")) { throw "Refusing unrelated cluster: $cluster" } }` |
| 1485 | `Assert-OwnershipRecord` | `foreach ($bucket in $Record.buckets) { if ($bucket -notmatch '^pcp-local-e2e-[a-z0-9-]+$' -or $bucket -notlike "*$suffix*" -or $bucket.Length -gt 63) { throw "Refusing unrelated bucket: $bucket" } }` |
| 1486 | `Assert-OwnershipRecord` | `if ($Record.ownedGitImage -and $Record.ownedGitImage -ne "pcp-local-e2e-git:$($Record.runId)") { throw 'Invalid owned Git image' }` |
| 1487 | `Assert-OwnershipRecord` | `if ($Record.originalGitImageID -and $Record.originalGitImageID -notmatch '^sha256:[a-f0-9]{64}$') { throw 'Invalid original Git image identity' }` |
| 1490 | `Assert-OwnershipRecord` | `if ($uri.Host -notin @('localhost', '127.0.0.1', '::1') -or $Record.localStackContainer -ne "pcp-local-e2e-localstack-$($uri.Port)") { throw 'Invalid owned LocalStack container' }` |
| 1512 | `Remove-RecordedRun` | `if ($identity.Status -eq 'Match') { throw "Run-owned process PID $($entry.id) is still running at '$($entry.path)' after stop" }` |
| 1537 | `Remove-RecordedRun` | `if ((Get-Field $info.Config.Labels 'platform.example.io/local-e2e-run') -ne $Record.runId) { throw 'Refusing LocalStack cleanup: ownership label does not match' }` |
| 1546 | `Remove-RecordedRun` | `if ($resolved -ne [IO.Path]::GetFullPath($Record.stateRoot) -or (Get-Item -LiteralPath $resolved).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Refusing cleanup through a reparse point or unexpected resolved state path' }` |
| 1550 | `Remove-RecordedRun` | `if ($errors.Count) { throw ($errors -join '; ') }` |
| 1556 | `Verify-Cleanup` | `if (-not $process.HasExited) { throw "Temporary process remains: PID $($process.Id), executable '$($process.Path)'" }` |
| 1559 | `Verify-Cleanup` | `if (@($script:OwnedClusters &#124; Where-Object { $clusters -split "&#96;r?&#96;n" -contains $_ }).Count) { throw 'Run-owned Kind clusters remain' }` |
| 1562 | `Verify-Cleanup` | `if (@($script:OwnedBuckets &#124; Where-Object { $_ -in $buckets }).Count) { throw 'Run-owned LocalStack buckets remain' }` |
| 1566 | `Verify-Cleanup` | `if ($containers -split "&#96;r?&#96;n" -contains $script:LocalStackContainer) { throw 'Run-owned LocalStack container remains' }` |
| 1568 | `Verify-Cleanup` | `if (-not $KeepArtifacts -and (Test-Path -LiteralPath $script:StateRoot)) { throw 'Run-owned temporary state remains' }` |
| 1582 | `Invoke-Clean` | `foreach ($name in @('docker', 'kind', 'kubectl')) { if (-not (Get-Command $name -ErrorAction SilentlyContinue)) { throw "Required cleanup tool missing: $name" } }` |
| 1590 | `Invoke-Clean` | `if ($file.Directory.Name -ne $record.runId) { throw 'Ownership file directory does not match run ID' }` |
| 1601 | `Invoke-Clean` | `if ($identity.Status -eq 'Match') { throw "Recorded process remains: PID $($entry.id), executable '$($entry.path)'" }` |
| 1605 | `Invoke-Clean` | `if ($containers -split "&#96;r?&#96;n" -contains $record.localStackContainer) { throw 'Recorded LocalStack container remains' }` |
| 1608 | `Invoke-Clean` | `if (@($record.clusters &#124; Where-Object { $clusters -split "&#96;r?&#96;n" -contains $_ }).Count -or (Test-Path -LiteralPath $record.stateRoot)) { throw 'Recorded run cleanup verification failed' }` |
| 1611 | `Invoke-Clean` | `if (@($record.buckets &#124; Where-Object { $_ -in $buckets }).Count) { throw 'Recorded buckets remain' }` |
| 1623 | `Invoke-Clean` | `if ($BrowserSession -and $Suite -eq 'clean') { throw 'BrowserSession cannot be combined with clean' }` |
| 1624 | `Invoke-Clean` | `if ($Suite -eq 'browser' -and -not $BrowserSession) { throw 'Suite browser requires -BrowserSession' }` |

## Recorded acceptance gates

| Original line | Function | Original result expression |
|---|---|---|
| 458 | `Assert-Convergence` | `Add-Result -Name 'TERRAFORM_OUTPUT' -Status 'PASS' -Evidence 'Allowlisted Terraform target_discovery output persisted, digest verified and admitted into InfraStack status'` |
| 465 | `Invoke-Lane` | `try { & $Action; Add-Result -Name ('SUITE_' + $Name.ToUpper()) -Status 'PASS' -Evidence ('duration={0:N1}s' -f $timer.Elapsed.TotalSeconds) }` |
| 466 | `Invoke-Lane` | `catch { Add-Result -Name ('SUITE_' + $Name.ToUpper()) -Status 'FAIL' -Evidence $_.Exception.Message; throw }` |
| 497 | `Invoke-Platform` | `Add-Result -Name 'PLATFORM_CLASS_API' -Status PASS -Evidence 'Dry-run, create, current Ready, list and detail use the real Kubernetes class'` |
| 514 | `Invoke-Platform` | `Add-Result -Name 'PLATFORM_TEMPLATE_API' -Status PASS -Evidence 'Real ConfigMap create/read/duplicate rejection/exact-name delete; no class or environment created by template'` |
| 535 | `Invoke-Platform` | `Add-Result -Name 'PLATFORM_READ_MODEL' -Status PASS -Evidence 'Explicit top-level API create, automatic InfraStack, WaitingApproval and no early Apply/runtime'` |
| 553 | `Invoke-Platform` | `Add-Result -Name 'PLATFORM_PLAN_GRAPH' -Status PASS -Evidence 'Actual immutable Plan UID/digest and allowlisted create node through run and architecture endpoints'` |
| 577 | `Invoke-Platform` | `Add-Result -Name 'PLATFORM_APPROVAL_API' -Status PASS -Evidence 'Stale report digest rejected; exact ChangeApproval persisted; saved-plan Apply, ResourceSet and read model Ready'` |
| 588 | `Invoke-Platform` | `Add-Result -Name 'PLATFORM_UPDATE_API' -Status PASS -Evidence 'Stale generation rejected; new capacity generation converged through NoChange without extra Apply (no infrastructure scaling claim)'` |
| 595 | `Invoke-Platform` | `Add-Result -Name 'PLATFORM_DELETE_API' -Status PASS -Evidence 'UID-bound delete, separate exact Destroy approval, finalizer removal, API NotFound and class cleanup'` |
| 745 | `Configure-TerraformDns` | `Add-Result -Name 'TERRAFORM_DNS_PREFLIGHT' -Status PASS -Evidence 'Explicit dependency-domain forwarding; real Pod verified both public dependencies, host.docker.internal and Kubernetes service DNS'` |
| 846 | `Wait-BrowserSession` | `Add-Result -Name 'BROWSER_SESSION' -Status 'PASS' -Evidence 'Ready class and live API supplied; explicit hold-file release observed'` |
| 1244 | `Invoke-FullLifecycle` | `Add-Result -Name 'AUTO_INFRASTACK' -Status 'PASS' -Evidence 'PlatformEnvironment controller materialized source/workspace/backend/identity/capacity; no manual InfraStack'` |
| 1271 | `Invoke-FullLifecycle` | `Add-Result -Name 'RECOVERY_AFTER_PLAN' -Status PASS -Evidence 'Completed Plan UID/digest/source and single runner Job persisted across manager leader restart'` |
| 1280 | `Invoke-FullLifecycle` | `Add-Result -Name 'INVALID_APPROVAL_FENCE' -Status 'PASS' -Evidence 'Wrong Plan UID persisted but unlocked no Apply or runtime mutation'` |
| 1289 | `Invoke-FullLifecycle` | `Add-Result -Name 'FULL_LIFECYCLE' -Status 'PASS' -Evidence "create=$envName plan=$($initialPlan.status.executionOutcome) apply=$($apply.status.executionOutcome) runtime=$runtimeName bucket=$bucket"` |
| 1306 | `Invoke-FullLifecycle` | `Add-Result -Name 'RESTART_RECOVERY' -Status 'PASS' -Evidence 'Leader restarted during active Plan, after completed Plan and after Ready; one Plan Job, one Apply, stable discovery/inventory/approval/runtime'` |
| 1329 | `Invoke-FullLifecycle` | `Add-Result -Name 'UPDATE_RUNTIME' -Status 'PASS' -Evidence 'v2 class changed target Service annotation in place; Service/inventory UID stable; LocalStack bucket retained; no extra Apply'` |
| 1344 | `Invoke-FullLifecycle` | `Add-Result -Name 'UPDATE_DELETE' -Status 'PASS' -Evidence "runtime updated in place, capacity reconciliation=NoChange, destroy=completed bucket=$bucket"` |
| 1370 | `Invoke-MultiTarget` | `Add-Result -Name 'MULTI_TARGET' -Status 'PASS' -Evidence "A=$($script:TargetAContext)/$svcA B=$($script:TargetBContext)/$svcB"` |
| 1412 | `Invoke-FailClosed` | `Add-Result -Name 'FAIL_CLOSED' -Status 'PASS' -Evidence 'Unregistered target, incomplete trusted discovery and durable mutation fence produced no runtime mutation; approval rejection is covered by lifecycle/platform'` |
| 1617 | `Invoke-Clean` | `Add-Result -Name 'CLEAN_MODE' -Status PASS -Evidence "Cleaned $count recorded inactive runs; external resources and live E2E runs preserved"` |
| 1644 | `Invoke-Clean` | `if ($Suite -eq 'all') { Add-Result -Name SUITE_LIFECYCLE -Status PASS -Evidence 'Executed within the recovery lifecycle, including update and independent Destroy' }` |
| 1655 | `Invoke-Clean` | `Add-Result -Name LOCAL_E2E_EXECUTION -Status FAIL -Evidence $_.Exception.Message` |
| 1657 | `Invoke-Clean` | `Add-Result -Name LOCAL_ENVIRONMENT -Status BLOCKED_LOCAL_ENVIRONMENT -Evidence 'Host memory allocation failed; this run does not establish a product failure'` |
| 1668 | `Invoke-Clean` | `Add-Result -Name CLEANUP -Status PASS -Evidence "Owned processes, clusters, buckets and container (if created) verified absent; $stateEvidence"` |
| 1670 | `Invoke-Clean` | `Add-Result -Name CLEANUP -Status FAIL -Evidence $_.Exception.Message` |
| 1674 | `Invoke-Clean` | `Add-Result -Name LOCAL_E2E -Status $overall -Evidence ('duration={0:N1}s; requested lanes and cleanup' -f $script:RunTimer.Elapsed.TotalSeconds)` |
