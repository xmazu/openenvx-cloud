plugin "docker" {
  config {
    endpoint = "unix:///Users/mackan/.docker/run/docker.sock"
    
    volumes {
      enabled = true
    }
    allow_privileged = true
  }
}