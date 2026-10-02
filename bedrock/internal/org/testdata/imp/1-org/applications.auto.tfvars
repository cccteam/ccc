# Rendered by bedrock from placement.json (applications) and owned by it: bedrock org render
# rewrites this file and bedrock org check compares it, so an application is registered in
# placement.json (bedrock org register <app>), never here. OpenTofu reads *.auto.tfvars after
# terraform.tfvars, which keeps the values a person decides.

# The applications whose GitHub repositories this layer configures (github.tf), one
# repository per application, named after it under the organization.
applications = ["harbor", "beacon"]
