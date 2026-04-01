job "terraform-worker" {
  datacenters = ["dc1"]
  type        = "batch"

  parameterized {
    meta_required = ["JOB_ID", "PROJECT_ID", "OPERATION"]
    meta_optional = ["MODULE_NAME"]
  }

  group "worker" {
    task "terraform" {
      driver = "docker"

      config {
        image      = "terraform-worker:local"
        command    = "/worker"
        force_pull = false
      }

      env {
        DATABASE_URL = "postgres://postgres:postgres@host.docker.internal:5432/openenvx?sslmode=disable"

        INFISICAL_CLIENT_ID     = "dummy"
        INFISICAL_CLIENT_SECRET = "dummy"
        INFISICAL_SITE_URL      = "http://host.docker.internal:8082"

        MINIO_ENDPOINT    = "host.docker.internal:9000"
        MINIO_ACCESS_KEY  = "minioadmin"
        MINIO_SECRET_KEY  = "minioadmin"
        MINIO_USE_SSL     = "false"
        MINIO_BUCKET_NAME = "openenvx-state"
      }

      resources {
        cpu    = 512
        memory = 1024
      }
    }
  }
}
