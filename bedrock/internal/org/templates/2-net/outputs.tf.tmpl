# Consumed by the environment and application layers through
# data "terraform_remote_state" on the state bucket (prefix 2-net), and by
# the domain's administrator for the delegation. Nothing here is a secret; keep it that way.

output "address" {
  description = "Static IPv4 address of the load balancer. The zone already points the apex and the wildcard at it."
  value       = google_compute_global_address.lb.address
}

output "address_name" {
  description = "Name of the address resource."
  value       = google_compute_global_address.lb.name
}

output "apps_domain" {
  description = "Domain the applications are served under."
  value       = var.apps_domain
}

output "certificate_map_id" {
  description = "Full resource ID of the certificate map the HTTPS proxy holds."
  value       = google_certificate_manager_certificate_map.apps.id
}

output "certificate_map_name" {
  description = "Name of the certificate map."
  value       = google_certificate_manager_certificate_map.apps.name
}

output "compute_service_agent" {
  description = "IAM member of this project's Compute Engine service agent. Not required by the documentation for cross-project service referencing; the reference deployment grants it roles/compute.loadBalancerServiceUser beside the layer identity, and an environment layer that wants the same can bind this."
  value       = local.compute_service_agent
}

output "dns_authorization_record" {
  description = "The CNAME Certificate Manager needs (name, type, data). Already in the zone; published so it can be added at another DNS host if the domain is not delegated here."
  value       = google_certificate_manager_dns_authorization.apps.dns_resource_record[0]
}

output "dns_zone_name" {
  description = "Name of the Cloud DNS zone for the apps domain."
  value       = google_dns_managed_zone.apps.name
}

output "dns_zone_name_servers" {
  description = "Name servers of the zone. The domain's administrator delegates the apps domain to these at the registrar."
  value       = google_dns_managed_zone.apps.name_servers
}

output "hosts" {
  description = "The hostname to backend service map the URL map currently routes, as applied."
  value       = var.hosts
}

output "load_balancer_service_user" {
  description = "IAM member of this layer's identity. Every environment project that owns a backend service the URL map references grants it roles/compute.loadBalancerServiceUser, at project level or on the backend service; that grant is the environment layer's, made from this output."
  value       = local.load_balancer_service_user
}

output "project_id" {
  description = "Shared network project."
  value       = local.project_id
}

output "shared_vpc_id" {
  description = "Always null. The global external Application Load Balancer references backend services across projects without a Shared VPC, and Cloud Run needs no VPC, so this layer creates no network. Kept as an output so a layer written against a Shared VPC design reads null rather than a missing attribute."
  value       = null
}

output "url_map_id" {
  description = "Full resource ID of the HTTPS URL map."
  value       = google_compute_url_map.lb.id
}

output "url_map_name" {
  description = "Name of the HTTPS URL map."
  value       = google_compute_url_map.lb.name
}

output "registrations" {
  description = "Registered domains: state, expiry and any issue the registrar reports (an unverified registrant mailbox is one)."
  value = {
    for d, r in google_clouddomains_registration.this : d => {
      state       = r.state
      expire_time = r.expire_time
      issues      = r.issues
    }
  }
}
