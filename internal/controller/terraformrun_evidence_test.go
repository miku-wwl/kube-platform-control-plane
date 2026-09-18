package controller

import "testing"

func TestParseRunnerTerminalResultUsesLastJSONLine(t *testing.T) {
	logs := "terraform init output\nnot-json\n{\"operation\":\"Apply\",\"terraformExitCode\":0,\"executionOutcome\":\"Succeeded\",\"artifactsReady\":true}\n"
	result, err := parseRunnerTerminalResult(logs)
	if err != nil {
		t.Fatalf("parseRunnerTerminalResult() error = %v", err)
	}
	if result.Operation != "Apply" || result.ExecutionOutcome != "Succeeded" || !result.ArtifactsReady {
		t.Fatalf("terminal result = %+v", result)
	}
}

func TestParseRunnerTerminalResultRejectsMissingTerminalLine(t *testing.T) {
	if _, err := parseRunnerTerminalResult("terraform init output\n"); err == nil {
		t.Fatal("missing terminal result must be rejected")
	}
}
