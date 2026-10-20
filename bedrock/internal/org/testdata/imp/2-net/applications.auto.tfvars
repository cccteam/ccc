# Rendered by bedrock from placement.json (applications, projects) and owned by it: bedrock org
# render rewrites this file and bedrock org check compares it, so an application is registered
# in placement.json (bedrock org register <app>), never here. OpenTofu reads *.auto.tfvars after
# terraform.tfvars, which keeps the values a person decides.

# One entry per hostname the load balancer serves, each with the backend service the
# application's layer creates in that environment (its net_hosts output): the hostnames
# recorded from the stack's state (placement.json's hostnames), <label>-<env> under the
# apps domain and the bare <label> in production, the label being the one the
# application's placement declares for its site (its code by default), each with its
# -next twin, which serves the revision the pipeline deployed and has not yet moved
# traffic to (the revision tag next), for the hook before traffic. An application
# recorded before hostnames were renders by the default rule, its code. A hostname an
# application serves on a vanity domain (var.vanity_domains, from placement.json's
# vanityDomains) is recorded and listed the same way, with no -next twin. The wildcard
# routes every pull-request environment, <label>-pr<N>, to the first environment's
# pull-request backend, which picks the Cloud Run service by the hostname's first label;
# exact hostnames win. An application's hostnames are listed once its registration is
# complete (placement.json's registration, moved by bedrock org register <app> --next
# after its stack's first apply in every environment has created the backends, which
# records them), recorded again by bedrock org hostnames <app> after a label change, and
# left out for an environment the
# application is out of service in (placement.json's outOfService: bedrock org teardown
# <app> [<env>...] takes an application's hostnames out ahead of its stack's teardown,
# while the listeners and every other application's hosts stay; bedrock org serve <app>
# [<env>...] puts them back once the stack is applied again).
hosts = {
  "harbor-tst.apps.imp.example"      = "projects/imp-tst-gbl-core-1a2b/global/backendServices/imp-tst-gbl-harbor-backend"
  "harbor-tst-next.apps.imp.example" = "projects/imp-tst-gbl-core-1a2b/global/backendServices/imp-tst-gbl-harbor-next-backend"
  "harbor-stg.apps.imp.example"      = "projects/imp-stg-gbl-core-3c4d/global/backendServices/imp-stg-gbl-harbor-backend"
  "harbor-stg-next.apps.imp.example" = "projects/imp-stg-gbl-core-3c4d/global/backendServices/imp-stg-gbl-harbor-next-backend"
  "harbor.apps.imp.example"          = "projects/imp-prd-gbl-core-5e6f/global/backendServices/imp-prd-gbl-harbor-backend"
  "harbor-next.apps.imp.example"     = "projects/imp-prd-gbl-core-5e6f/global/backendServices/imp-prd-gbl-harbor-next-backend"
  "beacon-tst.apps.imp.example"      = "projects/imp-tst-gbl-core-1a2b/global/backendServices/imp-tst-gbl-beacon-backend"
  "beacon-tst-next.apps.imp.example" = "projects/imp-tst-gbl-core-1a2b/global/backendServices/imp-tst-gbl-beacon-next-backend"
  "beacon-stg.apps.imp.example"      = "projects/imp-stg-gbl-core-3c4d/global/backendServices/imp-stg-gbl-beacon-backend"
  "beacon-stg-next.apps.imp.example" = "projects/imp-stg-gbl-core-3c4d/global/backendServices/imp-stg-gbl-beacon-next-backend"
  "beacon.apps.imp.example"          = "projects/imp-prd-gbl-core-5e6f/global/backendServices/imp-prd-gbl-beacon-backend"
  "beacon-next.apps.imp.example"     = "projects/imp-prd-gbl-core-5e6f/global/backendServices/imp-prd-gbl-beacon-next-backend"
  "*.apps.imp.example"               = "projects/imp-tst-gbl-core-1a2b/global/backendServices/imp-tst-gbl-pr-backend"
}
