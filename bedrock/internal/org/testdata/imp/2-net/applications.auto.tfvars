# Rendered by bedrock from placement.json (applications, projects) and owned by it: bedrock org
# render rewrites this file and bedrock org check compares it, so an application is registered
# in placement.json (bedrock org register <app>), never here. OpenTofu reads *.auto.tfvars after
# terraform.tfvars, which keeps the values a person decides.

# One entry per hostname the load balancer serves, each with the backend service the
# application's layer creates in that environment (its net_hosts output): <app>-<env>
# under the apps domain, and the bare <app> in production, each with its -next twin,
# which serves the revision the pipeline deployed and has not yet moved traffic to (the
# revision tag next), for the hook before traffic. The wildcard routes every pull-request
# environment, <app>-pr<N>, to the first environment's pull-request backend, which picks
# the Cloud Run service by the hostname's first label; exact hostnames win.
hosts = {
  "harbor-tst.impulseframework.dev"      = "projects/imp-tst-gbl-core-1a2b/global/backendServices/imp-tst-gbl-harbor-backend"
  "harbor-tst-next.impulseframework.dev" = "projects/imp-tst-gbl-core-1a2b/global/backendServices/imp-tst-gbl-harbor-next-backend"
  "harbor-stg.impulseframework.dev"      = "projects/imp-stg-gbl-core-3c4d/global/backendServices/imp-stg-gbl-harbor-backend"
  "harbor-stg-next.impulseframework.dev" = "projects/imp-stg-gbl-core-3c4d/global/backendServices/imp-stg-gbl-harbor-next-backend"
  "harbor.impulseframework.dev"          = "projects/imp-prd-gbl-core-5e6f/global/backendServices/imp-prd-gbl-harbor-backend"
  "harbor-next.impulseframework.dev"     = "projects/imp-prd-gbl-core-5e6f/global/backendServices/imp-prd-gbl-harbor-next-backend"
  "beacon-tst.impulseframework.dev"      = "projects/imp-tst-gbl-core-1a2b/global/backendServices/imp-tst-gbl-beacon-backend"
  "beacon-tst-next.impulseframework.dev" = "projects/imp-tst-gbl-core-1a2b/global/backendServices/imp-tst-gbl-beacon-next-backend"
  "beacon-stg.impulseframework.dev"      = "projects/imp-stg-gbl-core-3c4d/global/backendServices/imp-stg-gbl-beacon-backend"
  "beacon-stg-next.impulseframework.dev" = "projects/imp-stg-gbl-core-3c4d/global/backendServices/imp-stg-gbl-beacon-next-backend"
  "beacon.impulseframework.dev"          = "projects/imp-prd-gbl-core-5e6f/global/backendServices/imp-prd-gbl-beacon-backend"
  "beacon-next.impulseframework.dev"     = "projects/imp-prd-gbl-core-5e6f/global/backendServices/imp-prd-gbl-beacon-next-backend"
  "*.impulseframework.dev"               = "projects/imp-tst-gbl-core-1a2b/global/backendServices/imp-tst-gbl-pr-backend"
}
