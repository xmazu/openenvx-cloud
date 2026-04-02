data "external" "sleep" {
  program = ["sh", "-c", "sleep 5; echo '{\"status\": \"ok\"}'"]
}
