package terraform

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunner_Init(t *testing.T) {
	// Need a dummy tf file to init
	workDir := t.TempDir()
	tfFile := filepath.Join(workDir, "main.tf")
	err := os.WriteFile(tfFile, []byte(`
resource "local_file" "test" {
  content  = "test"
  filename = "test.txt"
}
`), 0644)
	if err != nil {
		t.Fatalf("failed to write dummy tf file: %v", err)
	}

	runner, err := NewRunner(workDir, nil)
	if err != nil {
		t.Skipf("Skipping test, terraform might not be installed: %v", err)
	}

	ctx := context.Background()
	stdout, stderr, err := runner.Init(ctx)
	if err != nil {
		t.Fatalf("Init failed: %v\nstdout: %s\nstderr: %s", err, string(stdout), string(stderr))
	}

	if len(stdout) == 0 {
		t.Error("expected stdout from Init")
	}

	planPath := filepath.Join(workDir, "tfplan")
	pStdout, pStderr, err := runner.Plan(ctx, planPath)
	if err != nil {
		t.Fatalf("Plan failed: %v\nstdout: %s\nstderr: %s", err, string(pStdout), string(pStderr))
	}

	if len(pStdout) == 0 {
		t.Error("expected stdout from Plan")
	}

	sStdout, sStderr, err := runner.Show(ctx, planPath)
	if err != nil {
		t.Fatalf("Show failed: %v\nstdout: %s\nstderr: %s", err, string(sStdout), string(sStderr))
	}

	if len(sStdout) == 0 {
		t.Error("expected stdout from Show")
	}

	aStdout, aStderr, err := runner.Apply(ctx, planPath)
	if err != nil {
		t.Fatalf("Apply failed: %v\nstdout: %s\nstderr: %s", err, string(aStdout), string(aStderr))
	}

	if len(aStdout) == 0 {
		t.Error("expected stdout from Apply")
	}

	// Verify file was created
	content, err := os.ReadFile(filepath.Join(workDir, "test.txt"))
	if err != nil {
		t.Fatalf("expected file to be created: %v", err)
	}
	if string(content) != "test" {
		t.Errorf("expected content 'test', got %q", string(content))
	}
}
