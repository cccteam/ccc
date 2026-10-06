# ---------------------------------------------------------------------------
# Organization policy baseline
#
# The GCP counterpart to the BaselineControls SCP in tf-aws-setup: guardrails
# that hold regardless of what a project layer tries to create.
#
# Applied at each folder this layer creates rather than at the org node. The
# organization is new and holds nothing this repository did not create, but
# folder scope keeps the terraform folder, and with it the boot project and the
# layer identities, out from under constraints that could lock out the identity
# managing them. Folder scope still covers every project created here, now and
# in future.
#
# The list itself is var.org_policies so an organization can trim or extend it
# without a code change. Constraints take effect the moment they are enforced,
# with no grace period, so extend it one constraint at a time.
# ---------------------------------------------------------------------------

locals {
  folder_policies = {
    for pair in setproduct(keys(local.folders), var.org_policies) :
    "${pair[0]}__${pair[1].constraint}" => { folder = pair[0], policy = pair[1] }
  }
}

# One resource covers every shape in var.org_policies: a boolean constraint
# sets enforce, a list constraint sets deny_all or a values block.
resource "google_org_policy_policy" "folder" {
  for_each = local.folder_policies

  name   = "${google_folder.this[each.value.folder].name}/policies/${each.value.policy.constraint}"
  parent = google_folder.this[each.value.folder].name

  spec {
    rules {
      enforce  = each.value.policy.enforce == null ? null : (each.value.policy.enforce ? "TRUE" : "FALSE")
      deny_all = each.value.policy.deny_all == null ? null : (each.value.policy.deny_all ? "TRUE" : "FALSE")

      dynamic "values" {
        for_each = each.value.policy.allowed_values != null || each.value.policy.denied_values != null ? [1] : []
        content {
          allowed_values = each.value.policy.allowed_values
          denied_values  = each.value.policy.denied_values
        }
      }
    }
  }
}

# Domain restricted sharing, restated on the environment folders with one
# exception (tags.tf): a resource tagged public-invoker=true may name allUsers.
# Without the copy the organization-level default applies unconditionally
# and the application layers cannot open a Cloud Run service to the load
# balancer. inherit_from_parent stays false, so this is the whole policy for
# the subtree: the second rule restates the organization's own customer.
resource "google_org_policy_policy" "allowed_member_domains" {
  for_each = { for k, f in local.folders : k => f if contains(["tst", "stg", "prd"], k) }

  name   = "${google_folder.this[each.key].name}/policies/iam.allowedPolicyMemberDomains"
  parent = google_folder.this[each.key].name

  spec {
    rules {
      condition {
        title       = "public-invoker"
        description = "A Cloud Run service the application layer tagged for the load balancer."
        expression  = "resource.matchTag('${local.org_id}/${google_tags_tag_key.public_invoker.short_name}', '${google_tags_tag_value.public_invoker.short_name}')"
      }
      allow_all = "TRUE"
    }

    rules {
      values {
        allowed_values = [data.google_organization.this.directory_customer_id]
      }
    }
  }
}

# Managed constraint with parameters, driven by var.allowed_contact_domains
# rather than listed in var.org_policies: a parameter value cannot be expressed
# in that variable's shape, and the domains belong beside the contact emails.
resource "google_org_policy_policy" "allowed_contact_domains" {
  for_each = local.folders

  name   = "${google_folder.this[each.key].name}/policies/essentialcontacts.managed.allowedContactDomains"
  parent = google_folder.this[each.key].name

  spec {
    rules {
      enforce = "TRUE"
      parameters = jsonencode({
        allowedDomains = var.allowed_contact_domains
      })
    }
  }
}
