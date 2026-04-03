package terraform

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hashicorp/terraform-exec/tfexec"
)

type BackendConfig struct {
	URL      string
	Username string
	Password string
}

type Runner struct {
	tf      *tfexec.Terraform
	workDir string
	stdout  io.Writer
	stderr  io.Writer
}

func NewRunner(workDir string, env map[string]string) (*Runner, error) {
	execPath, err := exec.LookPath("terraform")
	if err != nil {
		return nil, fmt.Errorf("failed to find terraform executable: %w", err)
	}

	tf, err := tfexec.NewTerraform(workDir, execPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize terraform exec: %w", err)
	}

	if len(env) > 0 {
		err = tf.SetEnv(env)
		if err != nil {
			return nil, fmt.Errorf("failed to set terraform env: %w", err)
		}
	}

	return &Runner{
		tf:      tf,
		workDir: workDir,
	}, nil
}

func (r *Runner) SetStdout(w io.Writer) {
	r.stdout = w
}

func (r *Runner) SetStderr(w io.Writer) {
	r.stderr = w
}

func (r *Runner) WriteBackendConfig(config BackendConfig) error {
	backendTF := fmt.Sprintf(`
terraform {
  backend "http" {
    address        = "%s"
    update_method  = "POST"
    lock_address   = "%s"
    lock_method    = "LOCK"
    unlock_address = "%s"
    unlock_method  = "UNLOCK"
    username       = "%s"
    password       = "%s"
  }
}
`, config.URL, config.URL, config.URL, config.Username, config.Password)

	return os.WriteFile(filepath.Join(r.workDir, "backend.tf"), []byte(backendTF), 0644)
}
