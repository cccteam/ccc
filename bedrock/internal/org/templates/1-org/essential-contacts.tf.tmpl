# Essential Contacts receive Google's security, suspension, and technical
# notifications. Registered per folder rather than at the org node so the
# terraform folder, and anything else outside this layer, keeps its own
# routing. Empty until an address is chosen.
resource "google_essential_contacts_contact" "folder" {
  for_each = {
    for pair in setproduct(keys(local.folders), var.essential_contact_emails) :
    "${pair[0]}-${pair[1]}" => {
      folder = pair[0]
      email  = pair[1]
    }
  }

  parent                              = google_folder.this[each.value.folder].name
  email                               = each.value.email
  language_tag                        = "en-US"
  notification_category_subscriptions = ["ALL"]
}
