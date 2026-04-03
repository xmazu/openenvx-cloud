package terraform

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/hashicorp/terraform-exec/tfexec"
)

func (r *Runner) Init(ctx context.Context) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer

	var err error
	for i := 0; i < 3; i++ {
		stdout.Reset()
		stderr.Reset()

		if r.stdout != nil {
			r.tf.SetStdout(io.MultiWriter(&stdout, r.stdout))
		} else {
			r.tf.SetStdout(&stdout)
		}

		if r.stderr != nil {
			r.tf.SetStderr(io.MultiWriter(&stderr, r.stderr))
		} else {
			r.tf.SetStderr(&stderr)
		}

		err = r.tf.Init(ctx)
		if err == nil {
			return stdout.Bytes(), stderr.Bytes(), nil
		}

		select {
		case <-ctx.Done():
			return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("context cancelled during terraform init retries: %w", ctx.Err())
		case <-time.After(time.Duration(i+1) * time.Second):
		}
	}

	return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("terraform init failed after retries: %w\nstderr: %s", err, stderr.String())
}

func (r *Runner) Plan(ctx context.Context, outPath string, opts ...tfexec.PlanOption) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	if r.stdout != nil {
		r.tf.SetStdout(io.MultiWriter(&stdout, r.stdout))
	} else {
		r.tf.SetStdout(&stdout)
	}

	if r.stderr != nil {
		r.tf.SetStderr(io.MultiWriter(&stderr, r.stderr))
	} else {
		r.tf.SetStderr(&stderr)
	}

	allOpts := append([]tfexec.PlanOption{tfexec.Out(outPath)}, opts...)
	_, err := r.tf.Plan(ctx, allOpts...)
	if err != nil {
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("terraform plan failed: %w\nstderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), stderr.Bytes(), nil
}

func (r *Runner) Apply(ctx context.Context, planPath string) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	if r.stdout != nil {
		r.tf.SetStdout(io.MultiWriter(&stdout, r.stdout))
	} else {
		r.tf.SetStdout(&stdout)
	}

	if r.stderr != nil {
		r.tf.SetStderr(io.MultiWriter(&stderr, r.stderr))
	} else {
		r.tf.SetStderr(&stderr)
	}

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

func (r *Runner) Destroy(ctx context.Context) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	if r.stdout != nil {
		r.tf.SetStdout(io.MultiWriter(&stdout, r.stdout))
	} else {
		r.tf.SetStdout(&stdout)
	}

	if r.stderr != nil {
		r.tf.SetStderr(io.MultiWriter(&stderr, r.stderr))
	} else {
		r.tf.SetStderr(&stderr)
	}

	err := r.tf.Destroy(ctx)
	if err != nil {
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("terraform destroy failed: %w\nstderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), stderr.Bytes(), nil
}
