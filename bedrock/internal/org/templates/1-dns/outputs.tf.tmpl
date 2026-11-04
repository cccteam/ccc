# Read by hand when a domain is delegated to this organization (bedrock domain
# check prints the same, with the place each record goes). Nothing here is a
# secret; keep it that way.

output "zone_names" {
  description = "Each domain's zone, as Cloud DNS names it in the boot project."
  value       = { for d, z in google_dns_managed_zone.domain : d => z.name }
}

output "zone_name_servers" {
  description = "Each domain's name servers, as Cloud DNS assigned them: what the customer sets once, at the registrar for a domain as registered, or as NS records at the DNS provider that serves the domain above for a label of one (bedrock domain check prints them with the place)."
  value       = { for d, z in google_dns_managed_zone.domain : d => z.name_servers }
}

output "delegations" {
  description = "Every name handed onward (the label under its domain), with the hosting organization that serves it and the name servers of its zone there, as applied."
  value       = { for name, d in local.delegations : name => { organization = d.organization, name_servers = d.name_servers } }
}
