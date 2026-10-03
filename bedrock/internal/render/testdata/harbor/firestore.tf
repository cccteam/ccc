# ---------------------------------------------------------------------------
# Firestore
#
# The document database: dataConfig.FirestoreDatabase (pkg/config/data.go) names it
# to the processes that construct the data level, and the site and the
# job process read and write it as their own identities, on this database alone:
# the project's Firestore role is granted under a condition naming it, so an
# application in the shared environment project reaches no other application's
# documents. One database per environment beside the Spanner database, a
# pull-request stack's own for a pull request, Native mode, in the primary
# region; prd keeps point-in-time recovery on and resists deletion. The
# migrate command constructs the data level too, whose live service signals the
# policy kind when the release's roles are written, so it receives the name as
# well, and the deploy identity, which runs it on the build worker, the grant.
#
# With the database, what the live pages need of it, from the two files beside
# the schema migrations (schema/firestore): the composite indexes and the field
# settings of schema/firestore/firestore.indexes.json, the security rules of schema/firestore/firestore.rules
# (firestore.rules beside this file is bedrock's copy, owned and rendered
# with the rest), the site's signature on the custom tokens it mints, and
# the web API key the browser presents to sign in with one. Firebase
# Authentication itself is the environment project's, initialized once by
# 2-env with no sign-in provider.
# ---------------------------------------------------------------------------

resource "google_firestore_database" "firestore" {
  project     = local.project_id
  name        = local.firestore_database_id
  location_id = local.primary_region
  type        = "FIRESTORE_NATIVE"

  point_in_time_recovery_enablement = local.is_prd ? "POINT_IN_TIME_RECOVERY_ENABLED" : "POINT_IN_TIME_RECOVERY_DISABLED"
  delete_protection_state           = local.is_prd ? "DELETE_PROTECTION_ENABLED" : "DELETE_PROTECTION_DISABLED"
  deletion_policy                   = "DELETE"
}

# Documents, as the site reads and writes them, on this database only.
resource "google_project_iam_member" "firestore_app" {
  project = local.project_id
  role    = "roles/datastore.user"
  member  = local.app_member

  condition {
    title       = "${local.firestore_database_id} only"
    description = "The application's own Firestore database in this project."
    expression  = "resource.name == \"projects/${local.project_id}/databases/${local.firestore_database_id}\""
  }

  depends_on = [google_firestore_database.firestore, google_service_account.app]
}

# The deploy identity's, for the migrate command it runs on the build worker:
# the release's role migration is a policy write the running instances hear
# through the signals document it writes here. The same condition bounds it to
# this database.
resource "google_project_iam_member" "firestore_deploy" {
  project = local.project_id
  role    = "roles/datastore.user"
  member  = local.identities.deploy_identity_member

  condition {
    title       = "${local.firestore_database_id} only"
    description = "The application's own Firestore database in this project."
    expression  = "resource.name == \"projects/${local.project_id}/databases/${local.firestore_database_id}\""
  }

  depends_on = [google_firestore_database.firestore]
}

resource "google_project_iam_member" "firestore_jobs" {
  project = local.project_id
  role    = "roles/datastore.user"
  member  = local.jobs_member

  condition {
    title       = "${local.firestore_database_id} only"
    description = "The application's own Firestore database in this project."
    expression  = "resource.name == \"projects/${local.project_id}/databases/${local.firestore_database_id}\""
  }

  depends_on = [google_firestore_database.firestore, google_service_account.jobs]
}

# The composite indexes of schema/firestore/firestore.indexes.json, one resource per entry with
# the fields in the file's order (Firestore appends __name__ itself): a query
# that filters several fields by equality and one by range is served from a
# composite index alone, and refused with FAILED_PRECONDITION without one.
resource "google_firestore_index" "subscriptions_resource_key_expiry" {
  project     = local.project_id
  database    = google_firestore_database.firestore.name
  collection  = "subscriptions"
  query_scope = "COLLECTION"

  fields {
    field_path = "resource"
    order      = "ASCENDING"
  }

  fields {
    field_path = "key"
    order      = "ASCENDING"
  }

  fields {
    field_path = "expiry"
    order      = "ASCENDING"
  }
}

resource "google_firestore_index" "subscriptions_resource_domain_expiry" {
  project     = local.project_id
  database    = google_firestore_database.firestore.name
  collection  = "subscriptions"
  query_scope = "COLLECTION"

  fields {
    field_path = "resource"
    order      = "ASCENDING"
  }

  fields {
    field_path = "domain"
    order      = "ASCENDING"
  }

  fields {
    field_path = "expiry"
    order      = "ASCENDING"
  }
}

resource "google_firestore_index" "subscriptions_resource_expiry" {
  project     = local.project_id
  database    = google_firestore_database.firestore.name
  collection  = "subscriptions"
  query_scope = "COLLECTION"

  fields {
    field_path = "resource"
    order      = "ASCENDING"
  }

  fields {
    field_path = "expiry"
    order      = "ASCENDING"
  }
}

# The fields schema/firestore/firestore.indexes.json settles on their own (its fieldOverrides).
# A time-to-live policy deletes a document once the timestamp the field holds
# has passed, within a day rather than at the instant, so a lookup still
# filters on the field; without one the collection grows without bound. The
# field's single-field indexes are set as the file lists them, so recording
# the policy against the field changes nothing about how it is indexed.
resource "google_firestore_field" "subscriptions_expiry" {
  project    = local.project_id
  database   = google_firestore_database.firestore.name
  collection = "subscriptions"
  field      = "expiry"

  ttl_config {}

  index_config {
    indexes {
      order       = "ASCENDING"
      query_scope = "COLLECTION"
    }
    indexes {
      order       = "DESCENDING"
      query_scope = "COLLECTION"
    }
    indexes {
      array_config = "CONTAINS"
      query_scope  = "COLLECTION"
    }
  }
}

resource "google_firestore_field" "changes_expires" {
  project    = local.project_id
  database   = google_firestore_database.firestore.name
  collection = "changes"
  field      = "expires"

  ttl_config {}

  index_config {
    indexes {
      order       = "ASCENDING"
      query_scope = "COLLECTION"
    }
    indexes {
      order       = "DESCENDING"
      query_scope = "COLLECTION"
    }
    indexes {
      array_config = "CONTAINS"
      query_scope  = "COLLECTION"
    }
  }
}

# The token signer: the site mints a Firebase custom token for the session
# principal through the IAM Credentials API (signBlob) as its own identity,
# with no key file, which takes Token Creator on its own account. The API is
# in the set 1-org enables on every environment project.
resource "google_service_account_iam_member" "app_signs_as_itself" {
  service_account_id = local.app_account_name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = local.app_member

  depends_on = [google_service_account.app]
}

# The browser's web API key (dataConfig.FirebaseAPIKey): the Firebase SDK presents
# it to the Identity Toolkit API to sign in with the custom token and to the
# Secure Token API to refresh the ID token that yields, and the key is good
# for those two APIs and nothing else. A public value by design, set on the
# processes that construct the data level (locals.tf); one per
# environment, a pull-request stack's own for a pull request.
resource "google_apikeys_key" "firebase" {
  project      = local.project_id
  name         = local.firebase_key_name
  display_name = "Firebase web API key - ${local.firebase_key_name}"

  restrictions {
    api_targets {
      service = "identitytoolkit.googleapis.com"
    }
    api_targets {
      service = "securetoken.googleapis.com"
    }
  }
}

# The security rules: a signed-in browser reads its own change set and nothing
# else (firestore.rules beside this file, bedrock's copy of schema/firestore/firestore.rules).
# A ruleset is immutable and named by the service, so a change to the file
# makes a new one, the release moves to it and the old one is deleted after
# (create_before_destroy); its source file is named for the database the
# release serves, which is also how a pull-request build's guard tells the
# pull request's own ruleset. The release is the database's: Firestore reads
# a named database's rules from cloud.firestore/<database id>.
resource "google_firebaserules_ruleset" "firestore" {
  project = local.project_id

  source {
    files {
      name    = "${local.firestore_database_id}.rules"
      content = file("${path.module}/firestore.rules")
    }
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "google_firebaserules_release" "firestore" {
  project      = local.project_id
  name         = "cloud.firestore/${local.firestore_database_id}"
  ruleset_name = "projects/${local.project_id}/rulesets/${google_firebaserules_ruleset.firestore.name}"

  depends_on = [google_firestore_database.firestore]
}
