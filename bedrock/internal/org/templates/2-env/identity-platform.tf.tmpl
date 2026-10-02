# ---------------------------------------------------------------------------
# Firebase Authentication
#
# Identity Platform, initialized once per environment project with no
# sign-in provider. An application that serves live pages signs its browsers
# in with a custom token its server mints as its own runtime identity
# (through the IAM Credentials API, no key file), and the browser presents
# the application's web API key (its stack's, firestore.tf) to the Identity
# Toolkit API to exchange it for an ID token. The configuration is the
# project's, so it lives here rather than in a stack: two applications in
# one project, or a pull-request stack beside the environment's, cannot each
# own it. Nothing a browser could sign up through is enabled (a custom token
# is the only way in, and only a server mints one), and a browser cannot
# delete the account its token made. Anonymous users do not exist here, so
# nothing is auto-deleted. The Identity Toolkit API is in the set 1-org
# enables on the project, and this layer's identity holds
# roles/identityplatform.admin there for this resource.
# ---------------------------------------------------------------------------

resource "google_identity_platform_config" "project" {
  project = local.project_id

  autodelete_anonymous_users = false

  sign_in {
    allow_duplicate_emails = false

    anonymous {
      enabled = false
    }
    email {
      enabled = false
    }
    phone_number {
      enabled = false
    }
  }

  client {
    permissions {
      disabled_user_signup   = false
      disabled_user_deletion = true
    }
  }
}
