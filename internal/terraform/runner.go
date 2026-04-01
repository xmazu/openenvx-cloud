package terraform

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"github.com/hashicorp/terraform-exec/tfexec"
)

type Runner struct {
	tf *tfexec.Terraform
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
		tf: tf,
	}, nil
}

func (r *Runner) Init(ctx context.Context) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	r.tf.SetStdout(&stdout)
	r.tf.SetStderr(&stderr)

	err := r.tf.Init(ctx)
	if err != nil {
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("terraform init failed: %w\nstderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), stderr.Bytes(), nil
}

func (r *Runner) Plan(ctx context.Context, outPath string) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	r.tf.SetStdout(&stdout)
	r.tf.SetStderr(&stderr)

	_, err := r.tf.Plan(ctx, tfexec.Out(outPath))
	if err != nil {
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("terraform plan failed: %w\nstderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), stderr.Bytes(), nil
}

func (r *Runner) Apply(ctx context.Context, planPath string) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	r.tf.SetStdout(&stdout)
	r.tf.SetStderr(&stderr)

	var opts []tfexec.ApplyOption
	if planPath != "" {
		opts = append(opts, tfexec.DirOrPlan(planPath))
	}

	err := r.tf.Apply(ctx, opts...)
	if err != nil {
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("terraform apply failed: %w\nstderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), stderr.Bytes(), nil
}

func (r *Runner) Show(ctx context.Context, planPath string) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	r.tf.SetStdout(&stdout)
	r.tf.SetStderr(&stderr)

	out, err := r.tf.ShowPlanFileRaw(ctx, planPath)
	if err != nil {
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("terraform show failed: %w\nstderr: %s", err, stderr.String())
	}

	// ShowPlanFileRaw returns the raw output, but it might also write to stdout.
	// If stdout is empty, we fallback to the returned string.
	if stdout.Len() == 0 && len(out) > 0 {
		return []byte(out), stderr.Bytes(), nil
	}

	return stdout.Bytes(), stderr.Bytes(), nil
}
