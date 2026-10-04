# Committed on purpose: nothing here is a secret. Project IDs come from
# 1-org's state; the one REPLACEME is the tst environment project, for the
# wildcard host below.

apps_domain = "apps.imp.example"

# Domains this layer registers through Cloud Domains, keyed by name, each with
# the yearly price Cloud Domains quoted and the notices it asked to acknowledge
# (bedrock domain add fills an entry in). Empty means no registration is
# managed here: the apps domain is delegated to this layer's zone from wherever
# it is registered. A registration also needs registrant_contact, a person's
# details (variables.tf), which are not committed until a domain is registered.
registrations = {}

# The hostnames the load balancer serves (each application's per environment, and
# the wildcard for pull-request environments) are in applications.auto.tfvars,
# rendered from placement.json.

# Records the apps domain needs beyond the load balancer's, such as mail or
# verification records if the domain ever carries them.
extra_records = {}
