# ---------------------------------------------------------------------------
# Custom organization roles
#
# Roles granted to layer identities where no predefined role has the right
# shape. Created at the org node so one definition serves every project, and
# named in var.layer_roles by bare ID (see local.custom_roles).
# ---------------------------------------------------------------------------

# Secret Manager without the payloads. A layer identity creates secrets as
# containers and manages their IAM and the lifecycle of their versions, but
# never reads or writes a version: values are added by the operator that owns
# them (secretmanager.versions.add) and read by the workload that needs them
# (secretmanager.versions.access). roles/secretmanager.admin carries both, which
# would put every secret value within reach of the deploy pipeline.
resource "google_organization_iam_custom_role" "secret_container_admin" {
  org_id      = local.org_id
  role_id     = "secretContainerAdmin"
  title       = "Secret Container Admin"
  description = "Creates and manages Secret Manager secrets and the lifecycle of their versions, without access to any version payload."
  permissions = [
    "secretmanager.locations.get",
    "secretmanager.locations.list",
    "secretmanager.secrets.create",
    "secretmanager.secrets.delete",
    "secretmanager.secrets.get",
    "secretmanager.secrets.getIamPolicy",
    "secretmanager.secrets.list",
    "secretmanager.secrets.setIamPolicy",
    "secretmanager.secrets.update",
    "secretmanager.versions.destroy",
    "secretmanager.versions.disable",
    "secretmanager.versions.enable",
    "secretmanager.versions.get",
    "secretmanager.versions.list",
  ]
}

# Secret Manager for the operator who owns the values. The operator group
# creates a container ahead of the release that first reads it (bedrock
# secret add) and adds versions to it; the application stack adopts the
# container at its next apply. Creating and adding only: no payload access,
# no IAM, and no lifecycle of versions beyond adding (those stay with the
# stack's identity, secretContainerAdmin). roles/secretmanager.
# secretVersionAdder adds versions to a container that exists and creates
# none, which would put the container back with the pipeline and its
# creation a release behind the value.
resource "google_organization_iam_custom_role" "secret_operator" {
  org_id      = local.org_id
  role_id     = "secretOperator"
  title       = "Secret Operator"
  description = "Creates Secret Manager secrets and adds versions to them, without access to any version payload."
  permissions = [
    "secretmanager.secrets.create",
    "secretmanager.secrets.get",
    "secretmanager.secrets.list",
    "secretmanager.versions.add",
    "secretmanager.versions.get",
    "secretmanager.versions.list",
  ]
}
