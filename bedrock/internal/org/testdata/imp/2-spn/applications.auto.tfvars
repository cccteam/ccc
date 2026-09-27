# Rendered by bedrock from placement.json (applications, projects) and owned by it: bedrock org
# render rewrites this file and bedrock org check compares it, so an application is registered
# in placement.json (bedrock org register <app>), never here. OpenTofu reads *.auto.tfvars after
# terraform.tfvars, which keeps the values a person decides.

# Each application's apply identity in the environments whose databases live on the
# shared instance (every one but the first, whose instance is its own): database admin
# on the instance, since creating a database is checked there.
database_admins = [
  "serviceAccount:imp-stg-gbl-harbor-tofu@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com", # harbor's stg apply identity (2-env stg)
  "serviceAccount:imp-prd-gbl-harbor-tofu@imp-prd-gbl-core-5e6f.iam.gserviceaccount.com", # harbor's prd apply identity (2-env prd)
  "serviceAccount:imp-stg-gbl-beacon-tofu@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com", # beacon's stg apply identity (2-env stg)
  "serviceAccount:imp-prd-gbl-beacon-tofu@imp-prd-gbl-core-5e6f.iam.gserviceaccount.com", # beacon's prd apply identity (2-env prd)
]
