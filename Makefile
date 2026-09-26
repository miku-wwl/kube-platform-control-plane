.PHONY: e2e e2e-lifecycle e2e-recovery e2e-multitarget e2e-failclosed e2e-clean

E2E_RUNNER ?= powershell -NoProfile -ExecutionPolicy Bypass -File e2e/Run-Stage1E2E.ps1

e2e:
	$(E2E_RUNNER) -Suite all

e2e-lifecycle:
	$(E2E_RUNNER) -Suite lifecycle

e2e-recovery:
	$(E2E_RUNNER) -Suite recovery

e2e-multitarget:
	$(E2E_RUNNER) -Suite multitarget

e2e-failclosed:
	$(E2E_RUNNER) -Suite failclosed

e2e-clean:
	$(E2E_RUNNER) -Suite clean
