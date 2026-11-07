# imr-root-infrastructure

The root organization of the imp.example Google Cloud organization,
kept by bedrock. The name says whose and what: `imr` is the prefix every
resource here carries, and this is the root organization's infrastructure, one
repository per (organization, kind), so a GitHub organization can hold the
infrastructure of more than one Google Cloud organization without the names
colliding. The other way round holds too: a Google Cloud organization holds
several bedrock organizations, each under a folder of its prefix's name at the
organization root, with projects, identities and grants of its own and nothing
at the organization node ("Several organizations", below).

There is no terraform here. The root organization is bedrock's own bookkeeping:
`placement.json` at the repository root says what the organization is, and two
Cloud Build triggers in the root project keep it true, by API, on every pull
request and every merge, the way the organization was made in the first place
(`bedrock org bootstrap`, the one thing done by hand; "Bootstrapped by one
command", below). A hosting organization under it, where applications run,
is rendered as OpenTofu layers and applied by its own triggers; it is created
from here, by pull request ("Creating a hosting organization", below).

The running log is [JOURNAL.md](JOURNAL.md). Every step that changes the
organization gets a line there first.

## What this organization is

A customer delegates one domain (`mydomain.com`, or a label of it such as
`apps.mydomain.com`) and expects every application to take a name under it:
`acme.mydomain.com`, `time.mydomain.com`. Applications are placed where there
is room, so a customer's second or third application lands in another hosting
organization sooner or later, and one hosting organization's zone cannot
answer for a name another serves. The shared domain's zone lives here instead,
above the hosting organizations, in an organization with no environments that
holds zones alone, in its root project. One such organization per Google Cloud
organization that hosts applications: the same pattern for shared hosting and
for a client's own. Two more things are its, for every organization in the
Google Cloud organization: the three GitHub Apps' keys and the Bot Account's
token, which live in Secret Manager in the root project and are rotated in one
place ("The GitHub Apps' keys", below), and the creation of each hosting
organization, by pull request.

Its placement lists the domains delegated to it (`domains` in
`placement.json`: `clientbay.example`, `impapps.example` and `zones.imp.example`) and, under each, the labels
handed onward, each naming the hosting organization and the four name servers
of the application's zone there. The apply makes one zone per domain, with
DNSSEC on, one NS record set per label, an hour's time to live, and the records
the placement declares for the domain (`records`: a verification record, a mail
policy, whatever the domain needs at its apex or under it; "Records in a zone",
below), and nothing else: no address and no certificate. 3 name(s) are handed
onward today. The hosting organization's side is its
per-application vanity shape, unchanged: the application's name is a vanity
domain of that organization, with its zone, wildcard certificate, authorization
record and address records. A lookup walks from the customer's registrar to the
zone here, which refers everything under the label to the application's zone,
whose wildcard answers; the environment names nest under the application's
(`stg.acme.mydomain.com`). A second application takes the same steps whether it
lands in the same hosting organization or another, so a customer's delegation
is made once and never moves. The same mechanism hands each hosting
organization its apps domain, a label of a domain here handed to that
organization's apps zone by its creation's hand-off, in place of a delegation
made by hand.

Nothing in an application's runtime path depends on anything but DNS: the
zones here are rendered infrastructure, written by pull request. The steps, the
records and the checks are below ("Delegating a domain to this organization",
"Handing a label onward").

## Several organizations

A Google Cloud organization holds several bedrock organizations as sibling
folders, each named after its prefix, and the folder is the whole of an
organization's reach: every role an identity of this organization holds, the
two runners' and the creating identity's, is granted on the root project or on
the organization's folder and nowhere higher, and the one custom role lives in
the root project. Nothing of a hosting organization's sits at the organization
node. Three roles of this organization's do, granted by the bootstrap, since
Google grants them there and nowhere lower: the creating identity's Folder
Creator, by which it makes a hosting organization's folder ("Creating a hosting
organization"), and the applier's Organization Policy Administrator and the
planner's Organization Policy Viewer, by which this organization sets and
compares the organization-policy baseline on each hosting organization's
folders ("The organization-policy baseline", below).

The reason is reach. A role granted at the organization node applies to every
folder and project below it, so an identity holding project IAM administrator
there could make itself owner of any other organization's project, one
holding service account administrator could mint tokens as any other's
identities, and one holding organization administrator could set the Google
Cloud organization's own IAM policy. Whoever drives one organization's
pipeline, through a merge into its infrastructure repository, through any
pull request's plan for the read roles, through a build in its root project,
or simply through a wrong id in a placement, would reach all of that. Scoped
to the folder, a mistake or a compromise in one organization's pipeline stops
at that organization. Inside the folder nothing changes: the organization is
the unit of blast radius, and the Google Cloud organization's administrator
stays above every folder.

The one account above the folders is the organization's break-glass account
(`breakGlassAccount` in `placement.json`, `seed@imp.example`): a Google
account in the Workspace directory with no person behind it, which ran the
bootstrap and keeps the administration of the folder Google gives a creator,
its key for a recovery by hand, and holds nothing else. Beside it, the creating
identity `imr-create-gbl-tofu@imr-root-gbl-core-c3d4.iam.gserviceaccount.com` holds Folder Creator at the organization node
and Billing Account Administrator on the billing account, granted by the
bootstrap: with them it creates each hosting organization under a folder of its
own, grants itself the rest there for the run, grants the new organization's
identities their use of the billing account, and holds nothing under that
folder after. The applier holds Organization Policy Administrator at the node
and the planner Organization Policy Viewer: the reach of the first is every
organization policy in the Google Cloud organization, which is why it sits with
the identity that already holds every organization's keys and the Bot Account's
token, runs from this repository's default branch alone, and never with a
hosting organization's.

## Naming

```
{prefix}-{part}-{region}-{purpose}
```

Prefix `imr`. `gbl` is the region segment only for resources that really are
global: projects, folders, service accounts, IAM, zones, secrets. Regional
resources carry the region code, `uc1` for us-central1.

| Resource | Name |
|---|---|
| root project | `imr-root-gbl-core-c3d4` |
| state bucket | `imr-root-gbl-state-c3d4` |
| the plan trigger's runner / the apply trigger's runner | `imr-root-gbl-planner@imr-root-gbl-core-c3d4.iam.gserviceaccount.com` / `imr-root-gbl-applier@imr-root-gbl-core-c3d4.iam.gserviceaccount.com` |
| creating identity | `imr-create-gbl-tofu@imr-root-gbl-core-c3d4.iam.gserviceaccount.com` |
| a domain's zone | `imr-root-gbl-dns-mydomain-com` |
| the GitHub Apps' keys / the Bot Account's token | `imr-root-gbl-github-infrastructure-key`, `imr-root-gbl-github-deployer-key`, `imr-root-gbl-github-release-key` / `imr-root-gbl-github-token` |
| Cloud Build connection, plan trigger, apply trigger | `imr-root-uc1-github`, `imr-root-uc1-plan`, `imr-root-uc1-apply` |
| the GitHub Apps | `imp-example-infrastructure`, `imp-example-deployer`, `imp-example-release` |

The project id and the bucket name are globally unique and permanent, so both
carry a random four-hex suffix, chosen by the bootstrap. The custom role
(`secretPolicyAdmin`) lives in the root project, where a name is the project's
own, so it carries none. The folder, the project and the bucket carry the label
`organization = imr`, and the label `bedrock-lab = "true"`.

## Bootstrapped by one command

The root organization is the one thing made by hand, by one command run once,
at a terminal, with the break-glass account's credentials (`gcloud auth
application-default login`):

```bash
bedrock org bootstrap <dir> --prefix imr --github-organization imp-example [--domain <domain>]... [--key-operator <member>]...
```

Before it, by hand, where it happens:

1. In the Google Cloud console, as a super administrator of the Workspace:
   the break-glass account, and its roles, which `bedrock org preflight`
   checks and the bootstrap checks again before it touches anything:
   - Organization Administrator (`roles/resourcemanager.organizationAdmin`), on the organization
   - Folder Creator (`roles/resourcemanager.folderCreator`), on the organization
   - Billing Account Administrator (`roles/billing.admin`), on the billing account

   A billing account that belongs to another Google Cloud organization is
   granted by a billing administrator of that organization; the break-glass
   account then sees it, and the bootstrap takes it.
2. On GitHub, as an owner of the `imp-example` organization
   (whoever creates it is its first owner): the organization itself; the Bot
   Account `imp-bot` (`githubBotAccount`, a GitHub user that is an
   owner of the organization and acts for no person), with a classic personal
   access token of the `repo`, `read:user` and `read:org` scopes, which the
   bootstrap reads from `GITHUB_TOKEN` or asks for without echo and stores in
   Secret Manager, where every Cloud Build connection authorizes with it
   through its alias `current`, rotated like a key ("The GitHub Apps' keys",
   below); and
   Google's Cloud Build GitHub App (`github.com/apps/google-cloud-build`),
   installed on the organization as the Bot Account, for all repositories.

The bootstrap looks before each step and makes what is missing, so a run that
stopped finishes on the next run in the same directory, whose `placement.json`
then names everything. In order: the organization's folder `imr` at the
organization node (the break-glass account's Folder Admin on it, its key for a
recovery by hand); the root project under it, linked to the billing account
with its APIs enabled, and the state bucket; the token's secret, its version
pinned (`githubTokenVersion`); the three GitHub Apps, infrastructure, deployer
and release, made in the browser from the
manifests bedrock declares (a page the command serves: Create, then Install for
all repositories, for each) or brought (`--infrastructure-app` with
`--infrastructure-key`, and the others) and held to the same declaration, each
key going from GitHub into its secret and its version pinned; the Cloud Build
installation found through the infrastructure app; this repository, private,
with `placement.json` and this README as its first commit; then everything the
apply keeps true ("How the root organization is kept", below), through the
same engine the apply trigger runs, the zones as the applier, which Google
holds as the domains' owner, with a Token Creator on it and on the creating
identity that the run grants itself and takes off at its end; last its own
Owner off the root project, and the check. No key and no token touches the computer's disk.

What it leaves: the break-glass account's Folder Admin on the folder, nothing
else by hand, and this repository at its first commit, from which every change
goes by pull request.

## How the root organization is kept

Routine changes go through pull requests. `placement.json` is edited, `bedrock
org render` rewrites this README from it, and the pull request carries both.
Two Cloud Build triggers in the root project, made by the bootstrap and kept by
the apply, run on it:

- The plan trigger (`imr-root-uc1-plan`), on a pull request into
  `master`, runs `bedrock org check` and then `bedrock org
  plan` as `imr-root-gbl-planner@imr-root-gbl-core-c3d4.iam.gserviceaccount.com`: the check holds this README to the
  placement; the plan compares the organization with the pull request's
  placement, section by section (the APIs, the identities and their roles, the
  secrets' aliases and policies, the grants, the zones and their record sets,
  the applier's and the creating identity's ownership of the domains,
  the connection and the triggers, the repository's settings and ruleset), and
  posts what the merge would change, and which hosting organizations it would
  create, in one comment on the pull request as the deployer app, updated on a
  later push. Nothing is changed.
- The apply trigger (`imr-root-uc1-apply`), on the merge, runs the check and
  then `bedrock org apply` as `imr-root-gbl-applier@imr-root-gbl-core-c3d4.iam.gserviceaccount.com`: under a lock in the state
  bucket (`locks/root`) it makes the organization as the merged placement says,
  creates each hosting organization recorded without a folder yet, as the
  creating identity, and leaves a marker (`applied/root`) with the commit. A
  run that fails leaves the lock; a person deletes the object, and the next
  push applies again.

The steps a run takes are the trigger's own and never this repository's: a
pull request changes what the steps read, never what they do. A run fetches
the bedrock `placement.json` pins (`bedrockVersion`, moved by `bedrock
upgrade`), checks, and only then plans or applies. The runners hold their
roles on the root project and read roles on the folder; the apply impersonates
the creating identity for a creation alone.

Two things the apply reports and never changes, since they lie outside the
folder or would break a customer: the grants at the organization node and on
the billing account, made by the bootstrap, and a zone
whose domain was taken out of `domains`, which stays standing (a finding) until
a person deletes it in the root project, because every name under the domain
still points at its name servers ("Making a zone again"). The grants above the
folder a trigger's run does not even read, since the runners hold nothing
there: the run notes that it did not compare them. The bootstrap compares them
on every run of its own, as does `bedrock org check` run by an administrator of
the organization and the billing account, and a missing one is a finding there,
made again by the bootstrap run again.

`bedrock org check`, at a terminal in a clone with the break-glass account's
credentials, prints the same comparison as information after the README check,
and lists each grant on the folder or the project to a person, beside the
break-glass account's Folder Admin. Recovery by hand is `bedrock org bootstrap`
again, in a clone: it finds everything that stands and makes the rest.
Destroying the organization, project and all, is `bedrock org destroy`, by
hand, refused while a hosting organization is recorded as created; it leaves
this repository and the three apps on GitHub, and every domain delegated here
then points at servers that answer nothing, so a root organization is
destroyed once no domain is.

## The GitHub Apps' keys

The organization holds three GitHub Apps, owned by the `imp-example`
organization and installed on all its repositories, each declared by bedrock
and made by the bootstrap from that declaration (a brought app is held to it):

- the infrastructure app (`imp-example-infrastructure`), which a run of
  the triggers acts on GitHub as, here and in every hosting organization: it
  configures the repositories and bypasses the branch rules it writes, and the
  creation pushes a new organization's repository and opens its hand-off pull
  request with it;
- the deployer app (`imp-example-deployer`), which the plan trigger posts
  on a pull request as, here and in every hosting organization, and every
  application's pipeline talks back on its pull requests as;
- the release app (`imp-example-release`), which cuts each application's
  releases as release-please, the only actor that makes a release tag.

Their private keys live in Secret Manager in the root project, one secret per
app for every bedrock organization in the Google Cloud organization
(`imr-root-gbl-github-infrastructure-key`, `imr-root-gbl-github-deployer-key`, `imr-root-gbl-github-release-key`),
beside the Bot Account's token (`imr-root-gbl-github-token`), which every Cloud Build
connection authorizes with, held the same way. No key reaches GitHub. Every
reader, here and in the hosting organizations, names a secret's alias
`current`, which `placement.json` pins (`githubInfrastructureKeyVersion`, the
two other keys' and `githubTokenVersion`) and the apply points at the pinned
version, so a rotation switches every reader at once and nothing changes before
the merge.

Rotating a key, by a key operator (`keyOperators` in `placement.json`, the IAM
members who may add a version and read it back; the runners read the keys, and
each hosting organization's boot identity holds the secrets' policy
administration for its own grants):

1. On GitHub, on the app's settings page: Private keys, Generate a private
   key. The browser downloads a `.pem` file.
2. In a terminal, in this repository:

   ```bash
   bedrock secret add github-deployer-key --from-file <the .pem file>
   bedrock org secrets check github-deployer-key <version>
   bedrock secret pin github-deployer-key <version>
   ```

   `add` puts the key into its secret as a new version and prints the number;
   `check` reads the version back and proves it with GitHub, as the app;
   `pin` records the version in `placement.json` and renders. Delete the
   `.pem` file.
3. The pull request; its apply moves the alias, and every reader switches at
   once. Then, on GitHub, delete the old key on the app's settings page once
   the next run has gone through clean.

Rotating the Bot Account's token, the same way, before it expires (GitHub
mails the account ahead of the date):

1. On GitHub, signed in as the Bot Account `imp-bot`: a new
   classic personal access token with the `repo`, `read:user` and `read:org`
   scopes and an expiration.
2. In a terminal, in this repository:

   ```bash
   bedrock secret add github-token
   bedrock org secrets check github-token <version>
   bedrock secret pin github-token <version>
   ```

   `add` asks for the token without echo and puts it into its secret as a new
   version; `check` reads the version back and proves it with GitHub: a
   personal access token, the Bot Account's, an owner's; `pin` records the
   version in `placement.json` (`githubTokenVersion`) and renders.
3. The pull request; its apply moves the alias, and every Cloud Build
   connection in every organization reads the new token from then on. Then, on
   GitHub, as the Bot Account, delete the old token.

## The organization-policy baseline

Each hosting organization's `1-org` makes four folders under the
organization's folder, `shared`, `tst`, `stg` and `prd`, and nothing of that
organization can set an organization policy on them: Google grants
Organization Policy Administrator at the organization node and nowhere lower,
and a hosting organization's identities hold their roles under its folder
alone. So this organization sets the baseline on those folders, as the
applier, and compares it, as the planner, from each hosting organization's
record (`organizations/<prefix>/placement.json`): in the creation, right
after `1-org`'s first apply, and on every apply after. The record carries what
the policies name, written by the creation from `1-org`'s outputs: the folders'
ids (`folders`) and each environment project's public-invoker tag
(`publicInvokerTags`). The `terraform` folder gets no policy, so the boot
project and the layer identities in it are never locked out by a constraint
they depend on.

On every one of the four folders, the tf-gcp-setup baseline trimmed to
constraints that cost nothing operationally, each closing a door nothing here
uses or pinning a decision already made:

| Constraint | Shape |
|---|---|
| `iam.managed.disableServiceAccountKeyCreation` | enforce |
| `iam.managed.disableServiceAccountKeyUpload` | enforce |
| `iam.serviceAccountKeyExpiryHours` | allow `2160h` |
| `iam.allowServiceAccountCredentialLifetimeExtension` | deny all |
| `iam.automaticIamGrantsForDefaultServiceAccounts` | enforce |
| `iam.disableAuditLoggingExemption` | enforce |
| `storage.publicAccessPrevention` | enforce |
| `storage.secureHttpTransport` | enforce |
| `storage.uniformBucketLevelAccess` | enforce |
| `storage.restrictAuthTypes` | deny `in:ALL_HMAC_SIGNED_REQUESTS` |
| `compute.disableGlobalSerialPortAccess` | enforce |
| `compute.disableSerialPortAccess` | enforce |
| `compute.requireOsLogin` | enforce |
| `compute.requireShieldedVm` | enforce |
| `compute.skipDefaultNetworkCreation` | enforce |
| `compute.vmCanIpForward` | deny all |
| `compute.vmExternalIpAccess` | deny all |
| `compute.restrictLoadBalancerCreationForTypes` | allow `GLOBAL_EXTERNAL_MANAGED_HTTP_HTTPS` |
| `run.allowedIngress` | allow `internal-and-cloud-load-balancing` |
| `gcp.restrictTLSVersion` | deny `TLS_VERSION_1`, `TLS_VERSION_1_1` |
| `cloudbuild.disableCreateDefaultServiceAccount` | enforce |
| `cloudbuild.allowedIntegrations` | allow `github.com` |
| `essentialcontacts.managed.allowedContactDomains` | parameters: the record's `contactDomains` |

On each environment folder, `iam.allowedPolicyMemberDomains`, domain
restricted sharing: a first rule allows every member on a resource that
carries that environment project's public-invoker tag (`resource.matchTagId`
on the key and the value `true` the record names), which is how an
application stack opens a Cloud Run service to the load balancer with
`allUsers`; a second rule allows the Google Cloud organization's own Cloud
Identity customer (`directoryCustomerId`) and no other domain. The policy
replaces its parent's (`inheritFromParent` false), so it is the whole of the
constraint on the folder.

Dropped from the model, and why: constraints for services not used here
(`ainotebooks.*`, `appengine.disableCodeDownload`,
`datastream.disablePublicConnectivity`, `firestore.requireP4SAforImportExport`,
`sql.*`, `cloudfunctions.*`, `dataform.restrictGitRemotes`,
`compute.restrictDedicatedInterconnectUsage`,
`compute.restrictNonConfidentialComputing`,
`iam.workloadIdentityPoolAwsAccounts`); `cloudkms.disableBeforeDestroy`, with
no customer-managed keys; `compute.restrictVpcPeering` and
`compute.restrictPrivateServiceConnectConsumer`, which interact with Private
Service Access, a decision for a later layer;
`iam.managed.preventPrivilegedBasicRolesForDefaultServiceAccounts`, since the
default service accounts are deleted at project creation and
`iam.automaticIamGrantsForDefaultServiceAccounts` covers the window before;
and `iam.managed.allowedPolicyMembers`, which needs a violation-log review
loop before it can be enforced. The baseline is bedrock's, the same for every
hosting organization; a constraint to add or drop is a change to bedrock.

`bedrock org plan` lists each policy a merge would set or bring back to the
baseline (`organization policies`), and `bedrock org check` compares them
after the README check. A record without the folders or the tags is a finding,
and that folder's policies wait for them.

## Delegating a domain to this organization

Once per domain, by the customer, where the domain's DNS is set. The zone
exists after the apply with the domain in `placement.json` (`domains`); `bedrock
domain check`, run from this repository's root, reads each zone and the world's
answers and prints what is in place and what is missing, each record on its
own line as it is pasted. The shape of the domain is not configured; the check
finds it:

- **A domain as registered** (`mydomain.com`, with nothing else on it): at the
  registrar where it is registered, its name servers become this zone's four,
  in place of the ones it has. At Squarespace Domains: the domain's DNS
  settings, Domain Nameservers, Use Custom Nameservers; the change takes up to
  48 hours to take effect.
- **A label of a domain served elsewhere** (`apps.mydomain.com`, when
  `mydomain.com` carries a website or mail): at the DNS provider that serves
  `mydomain.com`, one NS record set for the label, one record per name server
  of this zone, each as the provider's form takes it (the host is the label
  alone, the value the name server without a trailing dot). The records of
  `mydomain.com` stay where they are.

A domain that carries other DNS is never delegated whole: the check refuses an
undelegated domain that answers records that are not the zone's and names a
label of it to delegate instead. That is the live answers' call, not the
placement's: the organization's own domain (`imp.example`) is a
domain like any other here, delegated whole when nothing but what its zone
would carry is on it (a Cloud Identity domain with no mailboxes), or by a label
when its mail stays where it is. What a domain delegated whole needs at its apex
or under it is declared in the placement ("Records in a zone", below). The apex
of a shared domain is never an application's name: a zone cannot hand its own apex onward, so a customer
whose bare domain is the application delegates it to that application's
hosting organization alone, as a vanity domain there.

Until the delegation resolves, nothing under the domain resolves and every
application's certificate under it waits ("Handing a label onward", step 3).
`bedrock org check` reads nothing about these domains: the delegation is the
customer's, and `bedrock domain check` is where it shows.

**Ownership in Google's webmaster verification.** Cloud DNS asks whoever makes
a public zone for a name under a domain Google holds as verified (one
registered through Google, or well known) to own the domain, or a parent, in
Google's webmaster verification. Two identities make zones under this
organization's domains: the applier, which makes the zone here, and the
creating identity `imr-create-gbl-tofu@imr-root-gbl-core-c3d4.iam.gserviceaccount.com`, which makes each hosting
organization's apps zone. The apply keeps both verified, with nobody made an
owner by hand: as each identity, it asks Google for the identity's token for
the domain, writes the token as a TXT record at the domain's apex in the zone
here, beside whatever else the apex carries, and asks Google to see it, which
makes the identity a verified owner in its own right. Google sees the record
once the delegation resolves, so until then every apply says the verification
waits and asks again on the next run, and a creation under the domain refuses
its zone until the creating identity is verified. The plan and `bedrock org
check`, which cannot act as the identities, say the ownership is not compared.

A domain Google holds as verified, before its zone exists here: the applier
cannot own it yet, since its token has nowhere to resolve from, and Cloud DNS
refuses the zone. The apply reports the refusal as a finding that carries the
applier's token: where the domain's DNS is served today (the registrar's DNS,
until the delegation), a person adds a TXT record at the apex with that value.
The next apply verifies the applier, makes the zone and names its name
servers, which then go to the registrar as above. The record at the registrar's
DNS stops being served with the delegation, and by then the apply has put the
token in the zone here, so the ownership holds. For most domains Cloud DNS
asks nothing, and the zone is made on the first apply.

## Records in a zone

A zone here carries the labels' NS records, the owners' verification tokens
(below) and what the person who knows the domain declares under it in
`placement.json`: `records`, under the domain, a list of record sets, each
with a `name` (`@` for the apex, else a label, or labels joined with dots, with
a leading underscore for a service name such as `_dmarc`), a `type` (A, AAAA,
CAA, CNAME, MX, SRV or TXT; never NS, which is the labels' and the zone's own),
a `ttl` in seconds (300 unless given) and its `data`, the values as Cloud DNS
takes them (a TXT value is quoted by the apply when it is not already). A name
handed onward, or under one, is refused: the hosting organization's zone
answers for it.

```json
"domains": {
  "mydomain.com": {
    "records": [
      { "name": "@", "type": "TXT", "data": ["v=spf1 -all"] },
      { "name": "_dmarc", "type": "TXT", "data": ["v=DMARC1; p=reject"] },
      { "name": "@", "type": "CAA", "data": ["0 issue \"pki.goog\""] }
    ]
  }
}
```

`bedrock org plan` says which record a merge writes (`writes mydomain.com TXT
"v=spf1 -all"`), and the apply writes it as declared, or rewrites the set the
zone holds when its values or time to live differ. The apex TXT set is shared
with the owners' tokens: the apply keeps the tokens beside the declared values,
and anything else the set held goes. A record taken out of `records` stays in
the zone until a person deletes it (`gcloud dns record-sets delete`), as the
apply deletes no zone. What a domain delegated whole typically needs: an
identity domain, the TXT record that verifies it in Search Console for the
OAuth consent screen's authorized domains; a Workspace domain whose mail is on
it, its MX records, SPF and DMARC policies and DKIM key, as its Workspace
console lists them; any domain, a CAA record admitting `pki.goog`, the
authority that issues the certificates here.

## Handing a label onward

At an application's registration, two pull requests, in this order, by a
person or by the registration that automates them:

1. **In the hosting organization's infrastructure repository:** the
   application's name is declared as a vanity domain of that organization
   (`vanityDomains` in its `placement.json`: `acme.mydomain.com`), its `2-net`
   rendered and the pull request merged. The apply creates the zone, and Cloud
   DNS assigns it four name servers, which cannot be chosen in advance.
   `bedrock domain check` run there prints them, as the NS records for a label
   of a domain served elsewhere: the host `acme` under `mydomain.com`, one
   record per server.
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

   Then `bedrock org render`, and the pull request. Its plan says one NS record
   set is made; the merge makes it. The placement refuses a label twice, one
   above or below another, one handed to this organization itself, and other
   than four name servers.
3. **In the hosting organization**, once the delegation resolves (within the
   hour, once this zone is delegated): the certificate for `acme.mydomain.com`
   and `*.acme.mydomain.com` turns ACTIVE, which `bedrock domain check` there
   says. Here, `bedrock domain check` says each label answers its four servers,
   and names a label whose servers do not answer for it yet (the zone in the
   hosting organization not applied, or the servers mistyped).

A second application, in the same hosting organization or another, takes the
same two pull requests; only the repository of the first and the organization
named in the second differ. A move to another hosting organization is a new
zone there (step 1) and this label's organization and name servers changed in
one pull request (step 2), seen everywhere within the hour. A retirement runs
the steps backward: the label removed here first, so the name stops resolving
before the zone that served it goes, then the vanity domain removed there.

## Making a zone again

Cloud DNS gives a zone its name servers when it creates the zone, and a zone
made again can land on a different set. The customer's delegation then points
at servers that no longer answer for the domain, and every application under
it stops resolving until the customer points it again. So the apply never
deletes a zone: a domain taken out of `domains` leaves its zone standing, and
`bedrock org plan` and `bedrock org check` say so as a finding. Making a zone
again is deliberate, and the last step is the customer's:

1. In a terminal, as the break-glass account: delete the zone in the root
   project (`gcloud dns managed-zones delete`, its record sets first), with
   the domain still in `placement.json`.
2. In this repository: any pull request, or `gcloud builds triggers run` on the
   apply trigger; the apply makes the zone again, on new name servers.
3. By the customer, where the domain's DNS is set: the delegation pointed at
   the new name servers, as when it was first made. `bedrock domain check`
   prints them with the place, and says when the domain points at the zone.

The labels' record sets come back with the zone and need no step: a label
names the application's servers, not this zone's. So do the two identities'
tokens, which the apply writes again; Google checks an owner's record now and
then, not at the moment the zone is gone, so the applier's ownership holds
across the gap and Cloud DNS accepts the zone made again.

## Creating a hosting organization

A hosting organization under this Google Cloud organization is created from
here, by pull request, not seeded by hand: the creating identity
`imr-create-gbl-tofu@imr-root-gbl-core-c3d4.iam.gserviceaccount.com` does under a new folder what a hand seed and the first
applies would, and the new organization's own apply trigger takes over from its
first push. Nobody signs in as the break-glass account for it, and nobody
grants anything by hand: the identity holds Folder Creator at the organization
node and Billing Account Administrator on the billing account since the
bootstrap, grants itself the rest under the folder it creates, for the run, and
grants the new organization's identities their use of the billing account.

1. **The record.** In a terminal, in this repository: `bedrock org create
   <prefix>`, which writes `organizations/<prefix>/placement.json` for a person
   to fill in with what is the new organization's own to decide (its apps
   domain, the configuration of its shared database instance, its two regions
   with the primary this organization's, the Workspace groups of its
   environments, contact domains, entitlement durations, labels and vanity
   domains); run again, it fills in what this organization supplies (the Google
   Cloud organization and the billing account, the GitHub organization with its
   Bot Account, default branch and three apps, the Cloud Build authorization,
   the break-glass account, the bedrock pin, and `root`, which names this
   organization), chooses the boot project's suffix, lists the organization in
   `placement.json` (`hostingOrganizations`: its prefix and its boot project)
   and renders. The pull request carries both files; its plan says what the
   merge creates.
2. **The merge.** The apply trigger creates each listed organization without a
   folder yet, in order, as the creating identity: the folder at the
   organization node named after the prefix, the `terraform` folder under it,
   the boot project with its billing link and its APIs, the boot identity with
   its roles on the folder and on the boot project, the break-glass account's
   Folder Admin on the folder, the state bucket, and the boot identity's policy
   administration of the four shared secrets here; then, against the new
   bucket, the first applies of the new organization's two foundation layers,
   `0-bootstrap` and `1-org`, with the projects recorded in its placement, and
   Billing Account User on the billing account for its boot and org identities;
   then the public zone of its apps domain in its net project, made as the
   creating identity (which the apply verified as an owner of the domain,
   "Delegating a domain to this organization") and recorded in its placement
   (`appsZone`), which its `2-net` adopts; then the repository `<prefix>-infrastructure`, private in
   `imp-example`, with the rendered foundation as its first push,
   which its apply trigger runs on, every layer as its own identity; then the
   creating identity's own grants off everything it made, a marker in this
   organization's state bucket (`applied/created/<prefix>`), and the hand-off:
   a pull request here, from the branch `create/<prefix>`, recording the folder
   in `placement.json`, the placement as created under
   `organizations/<prefix>/`, the label of its apps domain handed onward
   to its zone (`domains`), and this README rendered from the placement,
   branch and pull request as the infrastructure
   app. A creation that stops finishes on the next push,
   since every step looks before it makes; a created organization is left
   alone, and its hand-off is kept by every apply until its merge records the
   folder or the record is taken out of `placement.json`: an open pull request
   moves to a fresh commit when what it records changed, and one closed
   unmerged stays as history while another is opened.
3. **The hand-off.** Its merge records the folder, from which the apply keeps
   the new boot identity's grants on the shared secrets, and that is the last
   reach into the new organization: nothing under its folder is this
   organization's to refresh, and `bedrock org check` there lists any grant on
   the folder to a principal outside it. Left to a billing administrator, in
   the new organization: its spend budget (its `0-bootstrap/README.md`, "The
   spend budget"). Its apps domain, a label of a domain here, is handed onward
   to its zone by the same merge, whose apply writes the NS record set, so the
   apps domain resolves within the hour; an application's name takes the two
   pull requests of "Handing a label onward".

The first hosting organization is created the same way as every later one. A
hosting organization's primary region is this organization's: a connection
reads the Bot Account's token in its own region.

## Contributing

1. Branch from `master`.
2. Edit `placement.json` and run `bedrock org render`; commit both.
3. Open a pull request describing the change. The plan trigger posts what the
   merge changes on it (one approving review, squash the only merge, the
   plan's build passing on the latest commit); the merge applies.

Nothing here is deleted by an apply: the zones stay until a person deletes
them ("Making a zone again"), the project and the folder go with `bedrock org
destroy` alone, and a secret's old versions stay until a key operator destroys
them in Secret Manager once nothing reads them.
