# 1-dns

The zones of the domains customers delegate to this organization, one per
domain, each handing the names under it onward to the hosting organizations
that serve the applications. The one layer after the bootstrap in a zones
organization: no environments, no instances, no load balancer. Its zones live
in the boot project. Applied by the apply trigger's run as
`imz-dns-gbl-tofu`, planned on a pull request as `imz-dns-gbl-plan`,
both made by `0-bootstrap`; the first apply is the bootstrap administrator's
("Applying").

## What it creates

- A Cloud DNS public zone per domain (`domains`, from `placement.json`'s
  `domains`: `clientbay.example`, `impapps.example` and `zones.imp.example`), named
  `imz-boot-gbl-dns-<the domain with hyphens for its dots>`, DNSSEC on.
  The zone carries `prevent_destroy`, as every bedrock zone does, so neither
  a plan nor a stray destroy can make it again on other name servers ("Making
  a zone again").
- Per label handed onward (`domains.<domain>.labels.<label>` in
  `placement.json`): one NS record set in the domain's zone, the name under
  the domain pointing at the four name servers of the application's zone in
  its hosting organization, an hour's time to live. Nothing else goes in a
  zone: no address, no certificate, no mail record. 3 name(s) are handed
  onward today.
- This repository's default branch ruleset (`github.tf`): changed by pull
  request alone, one approving review, squash the only merge, and the
  plan trigger's build passing on the latest commit. The repository itself is
  adopted, never destroyed. In a run of the triggers the GitHub provider's
  token is the infrastructure GitHub App's, minted in the run
  (`0-bootstrap/README.md`, "The infrastructure GitHub App").

## How a name resolves

A customer delegates one domain (`mydomain.com`, or a label of it such as
`apps.mydomain.com`) and expects every application to take a name under it:
`acme.mydomain.com`, `time.mydomain.com`. Applications are placed where there
is room, so a customer's second or third application lands in another hosting
organization sooner or later, and one organization's zone cannot answer for a
name served by another. The shared domain's zone therefore lives here, above
the hosting organizations, and hands each name onward:

1. The customer's registrar, or the DNS provider that serves the domain above,
   names this zone's four name servers for the domain: made once, at the first
   application or before it, and never moved after ("Delegating a domain to
   this organization").
2. This zone answers for the domain and refers everything under a label to the
   application's zone: the NS record set for `acme` names the four servers
   Cloud DNS assigned the zone that serves `acme.mydomain.com` in the hosting
   organization.
3. That zone is the hosting organization's per-application vanity shape,
   unchanged: `2-net` there serves the name as a vanity domain, with the
   wildcard certificate, the authorization record and the address records at
   the load balancer. Its wildcard answers `acme.mydomain.com` and every name
   one label below it; the environment names nest under the application's
   (`stg.acme.mydomain.com`, `tst.acme.mydomain.com`).

A lookup walks from the registrar to this zone, from this zone to the
application's, and the application's wildcard answers. A second application
takes the same path whether it lands in the same hosting organization or
another; only the organization named at step 2 differs. A per-application
delegation (`app.mydomain.com` delegated by the customer straight to one
hosting organization) needs none of this and stays as it is.

## Delegating a domain to this organization

Once per domain, by the customer, where the domain's DNS is set. The zone
exists after this layer's first apply with the domain in `placement.json`;
`bedrock domain check`, run from this repository's root, reads each zone and
the world's answers and prints what is in place and what is missing, each
record on its own line as it is pasted. The shape of the domain is not
configured; the check finds it:

- **A domain as registered** (`mydomain.com`, with nothing else on it): at the
  registrar where it is registered, its name servers become this zone's four,
  in place of the ones it has. At Squarespace Domains: the domain's DNS
  settings, Domain Nameservers, Use Custom Nameservers; the change takes up
  to 48 hours to take effect.
- **A label of a domain served elsewhere** (`apps.mydomain.com`, when
  `mydomain.com` carries a website or mail): at the DNS provider that serves
  `mydomain.com`, one NS record set for the label, one record per name server
  of this zone, each as the provider's form takes it (the host is the label
  alone, the value the name server without a trailing dot). The records of
  `mydomain.com` stay where they are.

A domain that carries other DNS is never delegated whole: the check refuses
an undelegated domain that answers records that are not the zone's and names
a label of it to delegate instead. The organization's own domain
(`imp.example`, whose mail is on it) is refused as a domain here
for the same reason; a label of it is fine. The apex of a shared domain is
never an application's name: a zone cannot hand its own apex onward, so a
customer whose bare domain is the application delegates it to that
application's hosting organization alone, as a vanity domain there.

Until the delegation resolves, nothing under the domain resolves and every
application's certificate under it waits ("Handing a label onward", step 3).
`bedrock org check` reads nothing about these domains: the delegation is the
customer's, and `bedrock domain check` is where it shows.

## Handing a label onward

At an application's registration, two pull requests, in this order, by a
person or by the registration that automates them:

1. **In the hosting organization's infrastructure repository:** the
   application's name is declared as a vanity domain of that organization
   (`vanityDomains` in its `placement.json`: `acme.mydomain.com`), its
   `2-net` rendered and the pull request merged. The apply creates the zone,
   and Cloud DNS assigns it four name servers, which cannot be chosen in
   advance. `bedrock domain check` run there prints them, as the NS records
   for a label of a domain served elsewhere: the host `acme` under
   `mydomain.com`, one record per server.
2. **In this repository:** the label under its domain in `placement.json`,
   naming the hosting organization by its prefix and those four servers as
   Cloud DNS spells them, with the trailing dot:

   ```json
   "domains": {
     "mydomain.com": {
       "labels": {
         "acme": {
           "organization": "rga",
           "nameServers": ["ns-cloud-a1.googledomains.com.", "ns-cloud-a2.googledomains.com.", "ns-cloud-a3.googledomains.com.", "ns-cloud-a4.googledomains.com."]
         }
       }
     }
   }
   ```

   Then `bedrock org render`, which writes `domains.auto.tfvars`, and the
   pull request. Its plan shows one NS record set added; the merge applies it.
   The placement refuses a label twice, one above or below another, one
   handed to this organization itself, and other than four name servers.
3. **In the hosting organization**, once the delegation resolves (within the
   hour, once this zone is delegated): the certificate for `acme.mydomain.com`
   and `*.acme.mydomain.com` turns ACTIVE, which `bedrock domain check` there
   says. Here, `bedrock domain check` says each label answers its four
   servers, and names a label whose servers do not answer for it yet (the zone
   in the hosting organization not applied, or the servers mistyped).

A second application, in the same hosting organization or another, takes the
same two pull requests; only the repository of the first and the organization
named in the second differ. A move to another hosting organization is a new
zone there (step 1) and this label's organization and name servers changed in
one pull request (step 2), seen everywhere within the hour. A retirement runs
the steps backward: the label removed here first, so the name stops resolving
before the zone that served it goes, then the vanity domain removed there.

The same mechanism hands a hosting organization its apps domain: a label of a
domain held here (`a.ourapps.example`, say) handed to that organization's apps
zone, in place of a delegation made by hand at the registrar.

## Making a zone again

Cloud DNS gives a zone its name servers when it creates the zone, and a zone
made again can land on a different set. The customer's delegation then points
at servers that no longer answer for the domain, and every application under
it stops resolving until the customer points it again. So every zone here
carries `prevent_destroy`: no plan and no stray destroy removes one, and
making a zone again is a deliberate change of three renders and one hand step
that is the customer's. The rule is lifted by a value in `placement.json`,
`zoneReplacement`, never by editing the files, which are bedrock's; each
render goes in a pull request of its own, merged and applied:

1. In this repository: set `"zoneReplacement": true` in `placement.json` and
   run `bedrock org render`. Every zone is written without `prevent_destroy`;
   nothing else changes. While the value is set, `bedrock org check` names it
   and what is left to do.
2. In this repository: make the change that recreates the zone and run
   `bedrock org render`, the value still set. Its plan shows the zone
   destroyed and created, its record sets with it.
3. By the customer, where the domain's DNS is set: the delegation pointed at
   the new name servers, as when it was first made. `bedrock domain check`
   prints them with the place, and, once every domain points at its zone,
   says that the value can be cleared.
4. In this repository: remove `zoneReplacement` from `placement.json` (or set
   it to `false`) and run `bedrock org render`. The rule is written back.

The labels' record sets move with the zone and need no step: a label names
the application's servers, not this zone's.

## Applying

The first apply is the bootstrap administrator's, by hand, once `0-bootstrap`
has applied and `boot_project_id` is set in `terraform.tfvars` (the seed's
value, suffix included). In a terminal, in this directory, as the bootstrap
administrator, with `GITHUB_TOKEN` set to an organization owner's token (gh's
token serves when gh is signed in as one) for the repository's import and its
ruleset:

```bash
cd 1-dns
tofu init
tofu plan
tofu apply
```

The plan shows this repository as an import, and as creates its ruleset and a
zone per domain with the record sets of its labels. From then on the apply
trigger applies this layer as `imz-dns-gbl-tofu` on every push to
`master` that changes it, and a pull request plans it as
`imz-dns-gbl-plan`, with the infrastructure GitHub App's token minted in
the run; the first apply by hand leaves no grant behind, since this layer
creates no identity and no project. Recovery is by hand the same way
(`0-bootstrap/README.md`, "Recovery, by hand"). Destroying the organization
is `bedrock org destroy`, which takes the zones out of this layer's state
(they go with the boot project) and destroys the ruleset; the customers'
delegations then point at servers that answer nothing, so a zones organization
is destroyed once no domain is delegated to it.

## Inputs

| Name | Description | Type | Default | Required |
|---|---|---|---|:---:|
| `boot_project_id` | The boot project, the output `boot_project_id` of `0-bootstrap`, suffix included: the zones live there. | `string` | n/a | yes |
| `domains` | The domains and their labels handed onward, rendered from `placement.json` into `domains.auto.tfvars`; never set by hand. | `map(object)` | n/a | yes |
| `github_default_branch` | This repository's default branch, which the ruleset rules. | `string` | `"master"` | no |
| `github_organization` | The GitHub organization that holds this repository. | `string` | `"imp-example"` | no |
| `infrastructure_repository` | This repository's name. | `string` | `"imz-zones-infrastructure"` | no |
| `prefix` | Short org-wide prefix used in resource names. | `string` | `"imz"` | no |

`terraform.tfvars` sets `boot_project_id`; the rest are rendered from
`placement.json`.

## Outputs

| Name | Description |
|---|---|
| `delegations` | Every name handed onward, with the hosting organization that serves it and the name servers of its zone there, as applied. |
| `zone_name_servers` | Each domain's name servers, as Cloud DNS assigned them: what the customer sets, once. `bedrock domain check` prints them with the place. |
| `zone_names` | Each domain's zone, as Cloud DNS names it in the boot project. |
