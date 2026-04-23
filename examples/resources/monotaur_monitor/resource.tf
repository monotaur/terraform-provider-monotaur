resource "monotaur_component" "api_server" {
  name = "API Server"
}

resource "monotaur_monitor" "api_health" {
  name         = "API Health Check"
  type         = "Heartbeat"
  component_ids = [monotaur_component.api_server.id]
}
