# Committed on purpose: nothing here is a secret. Project IDs come from
# 1-org's state; the one REPLACEME is the tst environment project, for the
# wildcard host below.

apps_domain = "impulseframework.dev"

# Domains this layer registers through Cloud Domains, keyed by name, each with
# the yearly price Cloud Domains quoted and the notices it asked to acknowledge
# (bedrock domain add fills an entry in). Empty means no registration is
# managed here: the apps domain is delegated to this layer's zone from wherever
# it is registered. A registration also needs registrant_contact, a person's
# details (variables.tf), which are not committed until a domain is registered.
registrations = {}

# One entry per hostname the load balancer serves, added as the application
# layers create their backend services (their net_hosts output). The wildcard
# routes every pull-request environment, <app>-pr<N>.impulseframework.dev, to the
# tst environment's pull-request backend (2-env output pull_request_backend_id),
# which picks the Cloud Run service by the hostname's first label; exact
# hostnames win over it.
hosts = {
  "*.impulseframework.dev" = "projects/imp-tst-gbl-core-REPLACEME/global/backendServices/imp-tst-gbl-pr-backend"
}

# Records the apps domain needs beyond the load balancer's, such as mail or
# verification records if the domain ever carries them.
extra_records = {}
