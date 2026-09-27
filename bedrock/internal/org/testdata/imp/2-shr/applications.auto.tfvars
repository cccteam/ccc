# Rendered by bedrock from placement.json (applications, projects) and owned by it: bedrock org
# render rewrites this file and bedrock org check compares it, so an application is registered
# in placement.json (bedrock org register <app>), never here. OpenTofu reads *.auto.tfvars after
# terraform.tfvars, which keeps the values a person decides.

applications = ["harbor", "beacon"]

# Each application's deploy identity in every environment (2-env's), writer on the
# application's repository in the shared registry.
pushers = {
  harbor = [
    "serviceAccount:imp-tst-gbl-harbor-deploy@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com",
    "serviceAccount:imp-stg-gbl-harbor-deploy@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com",
    "serviceAccount:imp-prd-gbl-harbor-deploy@imp-prd-gbl-core-5e6f.iam.gserviceaccount.com",
  ]
  beacon = [
    "serviceAccount:imp-tst-gbl-beacon-deploy@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com",
    "serviceAccount:imp-stg-gbl-beacon-deploy@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com",
    "serviceAccount:imp-prd-gbl-beacon-deploy@imp-prd-gbl-core-5e6f.iam.gserviceaccount.com",
  ]
}

# Each application's apply identity in every environment (2-env's), reader on the
# repository: a revision an apply creates carries the image the last deploy left, and
# Cloud Run checks that the creator can read it.
pullers = {
  harbor = [
    "serviceAccount:imp-tst-gbl-harbor-tofu@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com",
    "serviceAccount:imp-stg-gbl-harbor-tofu@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com",
    "serviceAccount:imp-prd-gbl-harbor-tofu@imp-prd-gbl-core-5e6f.iam.gserviceaccount.com",
  ]
  beacon = [
    "serviceAccount:imp-tst-gbl-beacon-tofu@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com",
    "serviceAccount:imp-stg-gbl-beacon-tofu@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com",
    "serviceAccount:imp-prd-gbl-beacon-tofu@imp-prd-gbl-core-5e6f.iam.gserviceaccount.com",
  ]
}
