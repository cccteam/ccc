# Rendered by bedrock from placement.json (applications, projects) and owned by it: bedrock org
# render rewrites this file and bedrock org check compares it, so an application is registered
# in placement.json (bedrock org register <app>), never here. OpenTofu reads *.auto.tfvars after
# terraform.tfvars, which keeps the values a person decides.

# Each application's apply identity in every environment (2-env's), granted tagUser on the
# organization's public-invoker tag (tags.tf). A file of its own, apart from
# applications.auto.tfvars, because it is committed later: the repository in that file comes
# first, 2-env creates the identities under it, and a grant on an identity that does not exist
# is refused. bedrock org register prints the sequence.
public_invokers = {
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
