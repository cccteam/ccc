resource "google_folder" "this" {
  for_each = local.folders

  display_name = each.value
  parent       = data.google_organization.this.name

  # Guards against a folder being removed while it still holds projects.
  deletion_protection = true
}
