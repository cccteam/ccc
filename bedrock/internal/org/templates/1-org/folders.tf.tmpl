# The environment folders, under the organization's folder 0-bootstrap seeded
# and named after the prefix: the one tree this bedrock organization keeps in
# the GCP organization, beside another's.
resource "google_folder" "this" {
  for_each = local.folders

  display_name = each.value
  parent       = "folders/${local.boot.organization_folder_id}"

  # Guards against a folder being removed while it still holds projects.
  deletion_protection = true
}
