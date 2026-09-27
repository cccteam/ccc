# Committed on purpose: nothing here is a secret. The REPLACEME values come
# from the seed step in README.md: the boot project ID the seed chose, suffix
# included, and the bare numeric ID of the seeded terraform folder.

billing_account_id  = "012345-6789AB-CDEF01"
boot_project_id     = "imp-boot-gbl-core-REPLACEME" # the ID the seed step chose, suffix included
organization_domain = "impulseframework.com" # organization 123456789012
terraform_folder_id = "REPLACEME" # bare numeric folder ID from the seed step

# The two roles/billing.user grants need Billing Account Administrator. A
# bootstrap administrator who holds only Billing Account User leaves this
# false and a billing administrator makes the grants by hand (README.md,
# step 4); set it true once the identity applying this layer can manage
# billing IAM.
manage_billing_iam = false
