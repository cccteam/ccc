# Rendered by bedrock from placement.json (applications, projects) and owned by it: bedrock org
# render rewrites this file and bedrock org check compares it, so an application is registered
# in placement.json (bedrock org register <app>), never here. OpenTofu reads *.auto.tfvars after
# terraform.tfvars, which keeps the values a person decides.

# The registered applications, by code, each with its own identities, repository link and
# stack in every environment it is placed in, beside each other in the same projects.
applications = ["harbor", "beacon"]

# The environments each application is placed in, by code, a prefix of the promotion
# order: this environment's run makes identities, grants, a link and triggers for the
# applications placed in it alone (locals.tf, applications).
application_environments = {
  harbor = ["tst", "stg", "prd"]
  beacon = ["tst", "stg", "prd"]
}

# Each application's repository under the GitHub organization, by code: what the
# repository link and the operations workflow's identity binding name.
application_repositories = {
  harbor = "harbor"
  beacon = "beacon"
}
