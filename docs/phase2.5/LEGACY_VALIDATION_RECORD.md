# Historical validation record

Copied verbatim from README at `9d149cc82a3cec21998ae4a780a444c631cad6b4`. These dated runs used the removed harness and do not establish Phase 2.5 acceptance. Referenced ignored artifact directories may not exist in this checkout.

### Local validation — 2026-10-06

The following executions used real Kind controllers/Jobs and an external
LocalStack Ultimate instance. Every passing execution includes verified owned
cleanup. The final `all` run uses the current harness with the optional DNS
setting described above. Results are local backend/control-plane evidence.

| Suite | Result | Total, including bootstrap/cleanup | Run ID |
| --- | --- | --- | --- |
| lifecycle | PASS | 1297.4s / 21.6min | `20261006005812-7facad35` |
| platform | PASS | 674.5s / 11.2min | `20261006054055-1b5c0eb7` |
| recovery | PASS | 706.9s / 11.8min | `20261006055211-f0eb5b6e` |
| multitarget | PASS | 742.6s / 12.4min | `20261006060359-f0c1a85d` |
| failclosed | PASS | 276.7s / 4.6min | `20261006061623-dfaad889` |
| all | PASS | 1923.1s / 32.1min | `20261006084826-e454d7c8` |
| clean | PASS | 0.4s | Final check at 22:20 NZDT |

The standalone lifecycle ran before the final compiler-cache/DNS diagnostic
adjustments; the final `all` run repeated its assertions within recovery.
Final `all` lane times were bootstrap 244.0s, recovery/lifecycle 544.2s,
multitarget 623.1s, failclosed 7.2s and platform 485.0s. Dependency downloads
affect duration; these are measured workstation times, not CI guarantees.

Commands used for the passing executions:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File e2e/Run-LocalE2E.ps1 -Suite lifecycle -LocalStackEndpoint http://127.0.0.1:4566 -BuildParallelism 1
foreach ($lane in @('platform', 'recovery', 'multitarget', 'failclosed')) {
    powershell -NoProfile -ExecutionPolicy Bypass -File e2e/Run-LocalE2E.ps1 -Suite $lane -LocalStackEndpoint http://127.0.0.1:4566 -BuildParallelism 1 -RequireExistingLocalStack
}
powershell -NoProfile -ExecutionPolicy Bypass -File e2e/Run-LocalE2E.ps1 -Suite all -LocalStackEndpoint http://127.0.0.1:4566 -BuildParallelism 1 -RequireExistingLocalStack -TerraformDnsServer 1.1.1.1
powershell -NoProfile -ExecutionPolicy Bypass -File e2e/Run-LocalE2E.ps1 -Suite clean

$env:GOMEMLIMIT = '512MiB'
go test -p 1 ./...
go build -p 1 ./cmd/controller ./cmd/terraform-runner ./cmd/platform-api
go vet -p 1 ./...
npm --prefix web run build
npm --prefix web run typecheck
npm --prefix web run lint
```

Final Go test/build/vet, web build/typecheck/lint, PowerShell syntax/command
resolution and `git diff --check` passed. Evidence is retained under
`artifacts/e2e/<run-id>/`; the validation inventory and service restoration
checks are under `tmp/local-e2e-modernization/validation-20261006/`.

BrowserSession startup, live API/Ready class, legacy metadata compatibility,
active-run protection in `clean` and timeout cleanup passed in
`20261006062101-9df79c0a`. Normal hold-file release is **NOT VERIFIED**: automatic
policy review rejected both deletion attempts with `blocked by policy`. That
combined `all -BrowserSession` command exited 1 after timeout; its backend lanes
all passed and its final cleanup passed. This is a **NON-PRODUCT BLOCKER**;
the completed standalone/final backend results above remain PASS. No browser
clicks or live Ollama inference are claimed by this harness.

Earlier attempts retain their original failure evidence: external LocalStack
disappearance, upstream provider download/DNS errors, and a corrected DNS-probe
helper-name error. The final passing run did not relax the affected assertions.
The existing Ultimate container identity was preserved, test buckets/state were
absent, and the developer controller/API/Vite services were restored with
Ollama configured after validation.
