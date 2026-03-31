job "terraform-worker" {
  datacenters = ["dc1"]
  type        = "batch"

  parameterized {
    meta_required = ["job_id"]
  }

  group "worker" {
    task "terraform" {
      driver = "docker"

      config {
        image = "terraform-worker:local"
        command = "/usr/local/bin/terraform-worker"
        args = [
          "--job-id",
          "${NOMAD_META_job_id}",
          "--",
          "terraform",
          "version"
        ]
        force_pull = false
      }

      env {
        NOMAD_META_JOB_ID     = "${NOMAD_META_job_id}"
        AWS_ACCESS_KEY_ID     = "minioadmin"
        AWS_SECRET_ACCESS_KEY = "minioadmin"
        AWS_REGION            = "us-east-1"
        S3_ENDPOINT           = "http://host.docker.internal:9000"
      }

      resources {
        cpu    = 256
        memory = 512
      }
    }
  }
}
