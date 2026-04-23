resource "monotaur_label" "env" {
  text  = "production"
  color = "#E53935"
}

resource "monotaur_component" "api_server" {
  name      = "API Server"
  label_ids = [monotaur_label.env.id]
}
