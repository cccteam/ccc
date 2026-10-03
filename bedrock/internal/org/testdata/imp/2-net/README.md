# 2-net

The shared network project: the one global external Application Load
Balancer every application is served through, its address, certificate, and
the public DNS zone of the apps domain. Applied by the layers workflow as
`imp-net-gbl-tofu`, planned on a pull request as `imp-net-gbl-plan`.

There is no VPC in this project. See "Cross-project backends" for why.

## What it creates

- A static global IPv4 address, `imp-net-gbl-ip`.
- An SSL policy, TLS 1.2 minimum, `ssl_policy_profile` (MODERN) ciphers.
- Certificate Manager: a DNS authorization for `apps_domain`, one
  Google-managed certificate for the apex and `*.apps_domain`, a certificate
  map with an entry for each of the two hostnames, no PRIMARY entry. The
  wildcard covers every hostname the naming convention produces:
  `harbor.impulseframework.dev`, `harbor-stg.`, `harbor-tst.`, and
  `pr12-harbor-tst.`.
- A Cloud DNS public zone for `apps_domain`, DNSSEC on, with A records for
  the apex and the wildcard pointing at the address, the authorization CNAME
  copied from the DNS authorization, and whatever `extra_records` lists.
- The load balancer: an HTTPS URL map whose host rules come from `hosts`, an
  HTTP URL map that only redirects to HTTPS, the two target proxies (the HTTPS
  one holds the certificate map and the SSL policy), and the two
  `EXTERNAL_MANAGED` global forwarding rules on ports 80 and 443.
- An empty backend service, `imp-net-gbl-sink`, as the URL map's default
  service, and a default route action that aborts every unmatched request
  with 404 before any backend is chosen. A request to the bare address, or to
  a hostname in the wildcard no application owns, gets 404 and nothing else.

Not created here, on purpose:

- **Backend services, serverless NEGs, Cloud Run services.** Those belong to
  the application layer in each environment project, next to the service
  they front. This layer only routes to them.
- **Outlier detection.** A backend service setting, so the application
  layer's. Decided on (2026-09-25): every backend service the application
  layer creates carries an `outlier_detection` block.
- **Cloud Armor.** Opt-in later; a security policy attaches to a backend
  service, again in the application layer.
- **A VPC, Cloud NAT, a VPC connector, subnets, firewall rules.** Nothing
  here or in the environment projects needs a network: Cloud Run with
  `internal-and-cloud-load-balancing` ingress is reached by the load balancer
  over Google's network, and cross-project backends need no Shared VPC.

## Cross-project backends

The URL map in this project references backend services in the environment
projects. The question was whether that needs a Shared VPC with the
environment projects attached as service projects, the way regional and
internal Application Load Balancers do.

It does not. For the global external Application Load Balancer, Google's
documentation says the frontend and URL map "can reference backend services
or backend buckets from any project within the same organization. No VPC
network restrictions apply," and "while you can use a Shared VPC environment
to configure a cross-project deployment, this isn't a requirement"
([External Application Load Balancer overview, cross-project service
referencing](https://docs.cloud.google.com/load-balancing/docs/https)). The
setup guide for exactly this shape, frontend in one project and backend
service in another with no Shared VPC, is [Set up a global external
Application Load Balancer with a cross-project backend service and backend
bucket](https://docs.cloud.google.com/load-balancing/docs/https/setup-cross-project-backend-service-backend-bucket).
The one serverless restriction is App Engine: "you can't reference a
cross-project backend service if the backend service has serverless NEG
backends with App Engine." Cloud Run is fine, with the rule that the backend
service, its serverless NEG, and the Cloud Run service are all in the same
project, which they are.

So this project has no VPC, and there are no Shared VPC attachments. The
`shared_vpc_id` output exists and is always `null`.

What has to be true on the backend side, quoting the guide: "an administrator
of project B must grant the Compute Load Balancer Services User role
(`roles/compute.loadBalancerServiceUser`) to the administrator of project A to
allow access to the backend service ... This role can be granted at the
project level or at the resource level." Project A is `net`, its
administrator is this layer's identity `imp-net-gbl-tofu`, and project B is
each environment project. That grant is made by the environment layer, which
is the only identity holding `projectIamAdmin` there; it reads the member
from this layer's `load_balancer_service_user` output and binds it:

```hcl
resource "google_project_iam_member" "load_balancer_service_user" {
  project = local.project_id
  role    = "roles/compute.loadBalancerServiceUser"
  member  = data.terraform_remote_state.net.outputs.load_balancer_service_user
}
```

CCC's reference deployment additionally grants the same role to the net project's Compute
Engine service agent (`service-<number>@compute-system.iam.gserviceaccount.com`).
The documentation does not ask for it; it is published as
`compute_service_agent` so an environment layer can add it if a plan or a
request is ever refused without it.

How a host is added, in order:

1. The application layer, in an environment project, creates the Cloud Run
   service, its serverless NEG, and the backend service, and outputs the
   backend service's full URI.
2. The environment layer has already granted `load_balancer_service_user`
   in that project.
3. `hosts` in this layer's `applications.auto.tfvars` carries
   `"harbor-tst.impulseframework.dev" = "projects/.../global/backendServices/..."`,
   rendered from `placement.json` when the application was registered
   (`bedrock org register`), and this layer is applied. DNS and the
   certificate already cover the hostname; nothing else changes.

The apply order on a fresh organization is therefore this layer first with no
application registered yet (the wildcard host alone), then the environment
and application layers, then this layer again with the hosts. A host whose
backend service does not exist, or whose project
has not made the grant, fails the apply here with a permission or not-found
error on the URL map.

Pull-request environments (`pr<N>-harbor-tst.impulseframework.dev`) follow
the same path, which means an entry here per open pull request. That is a
cost of keeping the URL map in one place; the alternative, letting the tst
environment write host rules into this URL map, would hand a pull-request
build write access to the shared load balancer. Left as is.

## Delegating the domain

`apps_domain` defaults to `impulseframework.dev`, a domain of its own
beside the Workspace identity domain `impulseframework.com`. The zone here
does not become authoritative until the registrar delegates the domain to the
zone's name servers (`dns_zone_name_servers` output), or this layer registers
it (`registrations`). A domain that already does something else, mail above
all, needs every record it has in `extra_records` before the delegation, or
the delegation silently breaks it.

Until the delegation, the certificate stays in PROVISIONING because the
authorization CNAME is not resolvable. The `dns_authorization_record` output
carries that CNAME, so it can be added at the current DNS host instead if the
certificate should be issued before the delegation.

If another domain is registered for the applications, `apps_domain`
changes and the zone, the authorization, the certificate, the map entries,
and the records are recreated for it; every host in `hosts` changes with it.

## Applying

By the layers workflow (`.github/workflows/layers.yml`), after `1-org` has been applied:
a pull request that changes this directory plans it as the plan identity and
posts the plan, and the merge applies it as the layer identity. Both hold
their state-bucket grants from `1-org`. The first `init` writes
`.terraform.lock.hcl`; commit it.

By hand, for recovery, the bootstrap administrator applies it with their own
sign-in and the organization-level roles the seed names (no entitlement
covers a shared layer; `0-bootstrap/README.md`, "Recovery, by hand"):

```bash
cd 2-net
tofu init
tofu plan
tofu apply
```

## Inputs

| Name | Description | Type | Default | Required |
|---|---|---|---|:---:|
| `apps_domain` | Domain the applications are served under. | `string` | `"impulseframework.dev"` | no |
| `extra_records` | Further records in the zone (mail, verification). | `list(object)` | `[]` | no |
| `hosts` | Hostname to backend service URI, one entry per served host. | `map(string)` | `{}` | no |
| `ssl_policy_profile` | Cipher profile of the SSL policy. | `string` | `"MODERN"` | no |

Project ID and number, the layer identity, the boot project, and the prefix
are read from `1-org`'s state rather than declared.

## Outputs

Read by the environment and application layers through
`data "terraform_remote_state"` on the state bucket, prefix `2-net`.

| Name | Description |
|---|---|
| `address`, `address_name` | The static IPv4 address and its resource name. |
| `apps_domain` | The domain, as applied. |
| `certificate_map_id`, `certificate_map_name` | The certificate map. |
| `compute_service_agent` | This project's Compute Engine service agent, for the optional extra grant. |
| `dns_authorization_record` | The authorization CNAME (name, type, data). |
| `dns_zone_name` | The zone. |
| `dns_zone_name_servers` | Name servers to delegate the domain to. |
| `hosts` | The routing map, as applied. |
| `load_balancer_service_user` | The member each environment project grants `roles/compute.loadBalancerServiceUser`. |
| `project_id` | Shared network project. |
| `shared_vpc_id` | Always `null`; no VPC is created. |
| `url_map_id`, `url_map_name` | The HTTPS URL map. |
