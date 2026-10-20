# Rendered by bedrock from placement.json (applications, registration) and owned by it: bedrock org render
# rewrites this file and bedrock org check compares it, so an application is registered in
# placement.json (bedrock org register <app>), never here. OpenTofu reads *.auto.tfvars after
# terraform.tfvars, which keeps the values a person decides.

# The applications whose GitHub repositories this layer configures (github.tf), by
# code: the segment every resource name of the application carries.
applications = ["harbor", "beacon"]

# Each application's repository under the organization, by code: the name 1-org
# creates the repository with (its module path's last segment).
application_repositories = {
  harbor = "harbor"
  beacon = "beacon"
}

# The applications whose stacks have had their first apply in every environment: those
# registered in full (placement.json's registration names them no more, which bedrock org
# register <app> --next does once each environment's stack has a state). Their
# repositories' rulesets require the stack's check trigger, which the first apply made
# (github.tf); an application under registration merges on the CI jobs alone until then.
applied_applications = ["harbor", "beacon"]
