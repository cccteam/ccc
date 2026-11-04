locals {
  # Naming convention: {prefix}-{environment}-{region}-{purpose}. The zones live
  # in the boot project and Cloud DNS is global, so every name here carries
  # boot and gbl. None of these names is globally unique, so none carries a
  # random suffix.
  name_prefix = "${var.prefix}-boot-gbl"

  labels = {
    terraform             = "true"
    terraform_source_path = "1-dns"
    source_repo           = "imz-zones-infrastructure"
    organization          = "imz"
    environment           = "boot"
    bedrock-lab           = "true"
  }

  # Every name handed onward, by its full name (the label under its domain:
  # acme.mydomain.com), with the domain whose zone holds its record set, the
  # hosting organization and the name servers of the application's zone there:
  # one NS record set each (dns.tf).
  delegations = merge([
    for domain, d in var.domains : {
      for label, to in d.labels : "${label}.${domain}" => {
        domain       = domain
        organization = to.organization
        name_servers = to.name_servers
      }
    }
  ]...)
}
