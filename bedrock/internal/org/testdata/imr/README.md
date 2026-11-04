# imr-root-infrastructure

OpenTofu configurations for the imp.example Google Cloud organization,
rendered by bedrock. The name says whose and what: `imr` is the
organization prefix every resource here carries, and this is the root
organization's infrastructure, one repository per (organization, kind), so a GitHub
organization can hold infrastructure for more than one Google Cloud
organization without the names colliding. The other way round holds too: a
Google Cloud organization can hold more than one bedrock organization, each
under a folder of its prefix's name at the organization root (`0-bootstrap`),
with projects, identities, custom roles and grants of its own and
nothing at the organization node ("Several organizations", below). The
projects carry the label `organization = imr`. Modeled on
[cccteam/tf-gcp-setup](https://github.com/cccteam/tf-gcp-setup): numbered
layers, one identity per layer, no static credentials anywhere. The difference
is the control plane. There is no Scalr; state lives in a Cloud Storage bucket
in the boot project, and the layers run from two Cloud Build triggers in the
boot project, whose steps live in the trigger and not in this repository, as
their own service accounts, with no key anywhere: a pull request plans, its
merge applies.

The running log is [JOURNAL.md](JOURNAL.md). Every step that changes the organization
gets a line there first.

## Layers

| Layer | Directory | Applied as | Creates |
|---|---|---|---|
| 0 | [`0-bootstrap/`](0-bootstrap/) | the break-glass account the first time (local state, then migrated); then the apply trigger as `imr-boot-gbl-tofu` | adopts the seeded `terraform` folder, boot project, and boot identity; the dns layer identity and its grant on the boot project; the project-updater and state-bucket-policy custom roles; the Cloud Build connection, the two runner identities and the two triggers that plan and apply the layers; the three GitHub Apps' key secrets, held for every organization, with their aliases and the hosting organizations' and the key operators' grants; the state bucket's grants for its two identities |
| 1 | [`1-dns/`](1-dns/) | the break-glass account the first time; then the apply trigger as `imr-dns-gbl-tofu` | one Cloud DNS public zone per domain delegated to this organization, in the boot project, and one NS record set per label handed onward to a hosting organization's zone; this repository's default branch ruleset |

Two layers, nothing else: this organization holds DNS zones alone, no
environments, no instances and no load balancer. Every application's name
under a domain here is served by a hosting organization, which `1-dns` hands
the name to ("What this organization is").

## What this organization is

A customer delegates one domain (`mydomain.com`, or a label of it such as
`apps.mydomain.com`) and expects every application to take a name under it:
`acme.mydomain.com`, `time.mydomain.com`. Applications are placed where there
is room, so a customer's second or third application lands in another hosting
organization sooner or later, and one hosting organization's zone cannot
answer for a name another serves. The shared domain's zone lives here instead,
above the hosting organizations, in a bedrock organization with no
environments that holds zones alone: the bootstrap every organization gets,
then `1-dns` in place of the organization and environment layers. One such
organization per Google Cloud organization that hosts applications: the same
pattern for shared hosting and for a client's own. Two more things are its,
for every organization in the Google Cloud organization: the three GitHub
Apps' keys and the Cloud Build authorization, which live in its boot project
and are rotated in one place (`0-bootstrap/README.md`, "The GitHub Apps'
keys"), and the creation of each hosting organization, by pull request
("Creating a hosting organization", below).

Its placement lists the domains delegated to it (`domains` in
`placement.json`) and, under each, the labels handed onward, each naming the
hosting organization and the four name servers of the application's zone
there. `1-dns` renders one zone per domain and one NS record set per label,
and nothing else; the hosting organization's side is its per-application
vanity shape, unchanged (the application's name is a vanity domain of that
organization, with its zone, wildcard certificate, authorization record and
address records). A lookup walks from the customer's registrar to the zone
here, which refers everything under the label to the application's zone,
whose wildcard answers; the environment names nest under the application's
(`stg.acme.mydomain.com`). A second application takes the same steps whether
it lands in the same hosting organization or another, so a customer's
delegation is made once and never moves. The same mechanism hands each
hosting organization its apps domain, a label of a domain here handed to that
organization's apps zone, in place of a delegation made by hand.

Nothing in an application's runtime path depends on anything but DNS: the
zones here are rendered infrastructure, written by pull request. The steps,
the records and the checks are in [`1-dns/README.md`](1-dns/README.md).

## Several organizations

A Google Cloud organization holds several bedrock organizations as sibling
folders, each named after its prefix, and the folder is the whole of an
organization's reach: every role a layer identity holds, the boot identity's,
the dns layer identity's and the plan identities' (`0-bootstrap`),
is granted on the organization's folder and nowhere higher, and every custom
role lives in a project. Nothing of
the organization's sits at the organization node but the creation
identity's Folder Creator, by which it makes a hosting organization's folder
there ("Creating a hosting organization").

The reason is reach. A role granted at the organization node applies to every
folder and project below it, so an identity holding project IAM administrator
there could make itself owner of any other organization's project, one
holding service account administrator could mint tokens as any other's layer
identities, and one holding organization administrator could set the Google
Cloud organization's own IAM policy. Whoever drives one organization's
pipeline, through a merge into its infrastructure repository, through any
pull request's plan for the read roles, through a build in its boot project,
or simply through a wrong id in a placement, would reach all of that. Scoped
to the folder, a mistake or a compromise in one organization's pipeline stops
at that organization. Inside the folder nothing changes: the organization is
the unit of blast radius, and the Google Cloud organization's administrator
stays above every folder.

The one account above the folders is the organization's break-glass account
(`breakGlassAccount` in `placement.json`): it creates the folder at the
organization root with Folder Creator, keeps the administration of the folder
Google gives a creator, and holds nothing else once the first-time setup is
trimmed (`0-bootstrap/README.md`, "Trimming the break-glass account"). Beside
it, this organization's creating identity holds the same Folder Creator at
the organization node and Billing Account User on the billing account, the
two roles the break-glass account held for the seed, granted once by the
administrators: with them it creates each hosting organization under a folder
of its own, grants itself the rest there for the run, and holds nothing under
that folder after (`0-bootstrap/README.md`, "The creating identity's grants").

## Naming

```
{prefix}-{environment}-{region}-{purpose}
```

Prefix `imr`. `gbl` is the region segment only for resources that really are
global: projects, folders, service accounts, IAM. Regional resources carry the
region code, `uc1` for us-central1.

| Resource | Name |
|---|---|
| boot project | `imr-boot-gbl-core-a1b2` |
| dns layer identity / plan identity | `imr-dns-gbl-tofu` / `imr-dns-gbl-plan` |
| a domain's zone | `imr-boot-gbl-dns-mydomain-com` |
| state bucket | `imr-boot-gbl-state-a1b2` |

Project IDs and bucket names are globally unique and permanent, so both carry a
random four-hex suffix. The custom roles (`projectUpdater`) live in the projects, where a name is the project's own,
so they carry none. Every project carries the label `organization = imr`, and every project and bucket the label
`bedrock-lab = "true"`.

## How the layers are applied

Routine changes go through pull requests. Two Cloud Build triggers in the
boot project, rendered by bedrock like the layers (`0-bootstrap/cloud-build.tf`;
`0-bootstrap/README.md`, "The Cloud Build triggers"), run them. The plan
trigger plans every layer a pull request changes as that layer's plan
identity and posts the plan on the pull request, one comment per layer, kept
current across pushes; the apply trigger, on the merge, applies every layer
changed since its last successful apply as its apply identity, in layer order
(0-bootstrap, then 1-dns), one at a time, every one of them initialized before the
first applies (a layer that has never applied gets its empty state written by
its own init, which a later layer may then read), stopping at the first
failure.

The two layers share no output: `1-dns` reads nothing from `0-bootstrap`'s
state, so a pull request that changes both has both planned at once, side by
side. A layer that has never applied (no state object under its prefix yet, as
on a new organization's first pull request) is not planned at all and passes,
its comment on the pull request saying it is "planned after" its first apply,
which the merge makes. Any other failure fails the pull request.

The steps a run takes are the trigger's own and never this repository's: a
pull request changes what the steps read, never what they do. A run fetches
the bedrock `placement.json` pins (`bedrockVersion`, moved by `bedrock
upgrade`), checks the committed layers against the placement (`bedrock org
check`, which also refuses a `.tf` file no render produces and any override
file) and only then plans or applies. No key exists anywhere: the trigger's
runner identity impersonates each layer's own identity for that layer's
tofu, the planner the plan identities and the applier the apply identities.
`gcloud builds triggers run` on the apply trigger applies one layer again
with no change to it
(`0-bootstrap/README.md`, "The Cloud Build triggers", which also says how a
change to the steps themselves lands one apply later). Recovery is by hand (`0-bootstrap/README.md`, "Recovery, by hand"),
as the break-glass account. Destroying the organization, projects and all,
is `bedrock org destroy`, by hand (`0-bootstrap/README.md`, "Destroying the
organization").

First-time setup, by hand, before any identity can run. Each step opens with
the place it happens.

1. **GitHub.** On GitHub, as an owner of the `imp-example` organization
   (whoever creates it is its first owner): the organization itself, its
   machine account `imp-machine` (`githubMachineAccount`, a GitHub
   user that is an owner of the organization and acts for no person), this
   repository (`1-dns` rules its default branch; nothing else of it is the
   layers'), and the deployer app ("The deployer GitHub App", below). A
   GitHub organization that holds a hosting organization has the machine
   account and the apps already; this organization records the same.
2. **Seed.** In a terminal, as the break-glass account: `bedrock org
   preflight` first, which checks that the account holds each of these roles
   above the organization's folder:
   - Folder Creator (`roles/resourcemanager.folderCreator`), on the organization
   - Billing Account User (`roles/billing.user`), on the billing account

   Then the gcloud commands that make the organization's folder, which makes
   the account its administrator, the `terraform` folder under it, the boot
   project, the boot layer identity and its roles on the folder, and the state
   bucket. Both are in [`0-bootstrap/README.md`](0-bootstrap/README.md), "1.
   Seed with gcloud". In a terminal, a billing administrator separately
   grants `roles/billing.user` on the billing account to the boot identity;
   in the Billing console, the same administrator sets the spend budget
   (`0-bootstrap/README.md`, "5. Billing grants and the budget").
3. **The Cloud Build GitHub authorization.** In the boot project's Cloud
   Build console, as the break-glass account (Owner on the boot project
   from the seed), signed in to GitHub as the machine account
   `imp-machine`, before the first apply of `0-bootstrap`
   (`0-bootstrap/README.md`, "2. The Cloud Build GitHub authorization"): the
   host connection, which installs the Google Cloud Build GitHub App on the
   organization and stores its token in the boot project. Its installation
   ID and the token secret's version go into `placement.json`
   (`cloudBuildInstallationId`, `cloudBuildTokenSecretVersion`). The bedrock
   the triggers run goes in beside them: `bedrock upgrade`, in this
   repository, which records the release as `bedrockVersion` and
   `bedrockSha256` and renders.
4. **`0-bootstrap`.** In a terminal, in `0-bootstrap/`, as the break-glass
   account. The first apply runs on local state, with the backend block
   commented out, and adopts the seeded resources and the console's connection
   through `import` blocks and an import. The seed's values go into
   `placement.json` (`stateBucket`, `projects.boot`, `projectNumbers.boot`)
   and `bedrock org render` names the bucket in every backend block; the
   state is then moved in with `tofu init -migrate-state`. From here the plan
   and apply triggers exist, and their steps are the trigger's own.
5. **`1-dns`.** In a terminal, in `1-dns/`, as the same person, against the
   bucket directly, with their own GitHub token
   ([`1-dns/README.md`](1-dns/README.md), "Applying"). From here on the apply
   trigger applies both layers. The grants steps 2 to 5 leave behind (Owner on
   the boot project and Folder Admin and Folder Editor on the folders, which
   Google gives whoever creates them, and the seed's own on the organization's
   folder) are temporary: once the apply trigger applies the layers, a hand
   step removes them (`0-bootstrap/README.md`, "Trimming the break-glass
   account"), and Folder Admin on the organization's folder alone stays.
6. **The infrastructure GitHub App** (`0-bootstrap/README.md`, "The
   infrastructure GitHub App"). On GitHub, as an organization owner: the app,
   its private key and its installation on this repository. Then in a
   terminal, in this repository: its App ID recorded in `placement.json`
   (`githubInfrastructureAppId`), and its key added to the boot project's
   secret, proven and pinned with `bedrock secret add
   github-infrastructure-key`, `bedrock org secrets check
   github-infrastructure-key <version>` and `bedrock secret pin
   github-infrastructure-key <version>`, which records the version the alias
   `current` points at and renders. Until then a run of the triggers that
   plans or applies `1-dns` says so and stops.
7. **The deployer and release apps' keys** (`0-bootstrap/README.md`, "The
   GitHub Apps' keys"). In a terminal, in this repository, as a key operator
   (`keyOperators` in `placement.json`), once `0-bootstrap` has applied: the
   two App IDs recorded in `placement.json` (`githubDeployerAppId`,
   `githubReleaseAppId`), then for each key `bedrock secret add`, `bedrock org
   secrets check` and `bedrock secret pin` ("The deployer GitHub App", below).
   This organization holds the three keys for every organization in the
   Google Cloud organization, and each hosting organization reads them through
   the alias `current` (`hostingOrganizations` names them). Until then the
   plans are in the runs' logs alone.
8. **The domains.** In `placement.json`: each domain delegated to this
   organization (`domains`), `bedrock org render`, and the pull request whose
   apply makes its zone; then, by the customer, where the domain's DNS is set:
   the delegation to the zone's four name servers, as `bedrock domain check`
   prints them with the place (`1-dns/README.md`, "Delegating a domain to this
   organization"). Each application's name under a domain follows at the
   application's registration, in two pull requests (`1-dns/README.md`,
   "Handing a label onward").
9. **The creating identity's grants.** In a terminal, once `0-bootstrap` has
   applied and made the identity `imr-create-gbl-tofu`: an administrator
   of the Google Cloud organization grants it Folder Creator at the
   organization node, and a billing administrator Billing Account User on the
   billing account (`0-bootstrap/README.md`, "7. The creating identity's
   grants"). From then on each hosting organization is created from here, by
   pull request, the first one included ("Creating a hosting organization",
   below).

GitHub prerequisites: the organization's private repositories need rulesets
that GitHub enforces; the organization holds GitHub Apps; and its machine
account (`githubMachineAccount`) is an owner of it. bedrock's README lists
each with what needs it
([GitHub prerequisites](https://github.com/cccteam/ccc/blob/master/bedrock/README.md#github-prerequisites)).

Each layer's `terraform.tfvars` is committed. It holds IDs, not secrets, and
the values a layer runs with belong in the repository.

## Creating a hosting organization

A hosting organization under this Google Cloud organization is created from
here, by pull request, not seeded by hand: this organization's creation
identity `imr-create-gbl-tofu` (`0-bootstrap/bootstrap.tf`) does under
a new folder what the hand seed and the first applies did for this one, and
the new organization's own apply trigger takes over from its first push.
Nobody signs in as the break-glass account for it. The identity holds Folder
Creator at the organization node and Billing Account User on the billing
account, granted once by the administrators (`0-bootstrap/README.md`, "The
creating identity's grants"), and grants itself the rest under the folder it
creates, for the run.

1. **The record.** In a terminal, in this repository: `bedrock org create
   <prefix>`, which writes `organizations/<prefix>/placement.json` for a
   person to fill in with what is the new organization's own to decide (its
   apps domain, the configuration of its shared database instance, its two
   regions with the primary this organization's, the Workspace groups of its
   environments, contact domains, entitlement durations, labels and vanity
   domains); run again, it fills in what this
   organization supplies (the Google Cloud organization and the billing
   account, the GitHub organization with its machine account, default branch
   and three apps, the Cloud Build authorization, the break-glass account, the
   bedrock pin, and `root`, which names this organization), chooses the boot
   project's suffix, lists the organization in `placement.json`
   (`hostingOrganizations`: its prefix and its boot project) and renders. The
   pull request carries both files; its plan says what the merge creates.
2. **The merge.** The apply trigger, after the layers, creates each listed
   organization without a folder yet, in order, as the creating identity: the
   folder at the organization node named after the prefix, the `terraform`
   folder under it, the boot project with its billing link and its APIs, the
   boot identity with its roles on the folder, the break-glass account's
   Folder Admin on the folder, the state bucket, and the boot identity's
   policy administration of the four shared secrets here; then, against the
   new bucket, the first applies of the new organization's two foundation
   layers, `0-bootstrap` and the organization layer that makes its folders and
   projects, with those projects recorded in its placement; then the
   repository `<prefix>-infrastructure`, private in `imp-example`,
   with the rendered foundation as its first push, which its apply trigger
   runs on, every layer as its own identity; then the creating identity's own
   grants off everything it made, a marker in this organization's state bucket
   (`applied/created/<prefix>`), and the hand-off: a pull request here, from the
   branch `create/<prefix>`, recording the folder in `placement.json` and the
   placement as created under `organizations/<prefix>/`. A creation that stops
   finishes on the next push, since every step looks before it makes; a
   created organization is left alone.
3. **The hand-off.** Its merge records the folder, from which `0-bootstrap`
   keeps the new boot identity's grants on the shared secrets rendered
   (`github.tf`), and that is the last reach into the new organization:
   nothing under its folder is this organization's to refresh, and `bedrock
   org check` there lists any grant on the folder to a principal outside it.
   Left to a billing administrator, in the new organization: `roles/billing.user`
   on the billing account for its boot and org identities, and its spend
   budget (its `0-bootstrap/README.md`, "Created by the root organization");
   its apps domain is a label of a domain here, handed onward at its first
   registration like every application's name (`1-dns/README.md`).

The first hosting organization is created the same way as every later one. A
hosting organization's primary region is this organization's when the Cloud
Build token is a regional secret, as the console stores it: a connection
reads a token in its own region.

## The deployer GitHub App

The organization holds one GitHub App beside the infrastructure app `1-dns`
acts as (`0-bootstrap/README.md`): the deployer app, which the plan trigger
posts each layer's plan on a pull request as, here and in every hosting
organization, whose pipelines talk back on their pull requests as it too. Its
key lives in Secret Manager in this organization's boot project, for every
organization, and never reaches GitHub; the release app's key lives beside it
(`0-bootstrap/README.md`, "The GitHub Apps' keys"). A GitHub organization that
holds a hosting organization has the app already, with the two permissions
below among its own; this organization records the same App ID and installs
nothing new. Otherwise, once, by a person:

1. On GitHub, as an owner of the `imp-example` organization: the
   organization's Settings, Developer settings, GitHub Apps, New GitHub App.
   Give it any homepage URL, turn Webhook off, and choose only this account
   under Where can this app be installed. Repository permissions, with
   Metadata (read), which GitHub requires of every app: Issues and Pull
   requests, each read and write. Create it and note the App ID on its
   settings page.
2. On GitHub, on the app's settings page: Install App, on the organization,
   for all repositories. Then Private keys, Generate a private key: the
   browser downloads a `.pem` file.
3. In `placement.json`: the app's App ID as `githubDeployerAppId`.
4. In a terminal, in this repository, once `0-bootstrap` has applied and made
   the key's secret, as a key operator (`keyOperators` in `placement.json`):

   ```bash
   bedrock secret add github-deployer-key --from-file <the deployer app's .pem file>
   bedrock org secrets check github-deployer-key <version>
   bedrock secret pin github-deployer-key <version>
   ```

   `add` puts the key into its secret in the boot project
   (`imr-boot-gbl-github-deployer-key`) as a new version and prints the number; `check`
   proves the version with GitHub; `pin` records it in `placement.json`
   (`githubDeployerKeyVersion`) and renders the alias `current` at that
   version, which the pull request's apply moves. The plan trigger posts the
   layers' plans as the deployer app from then on, here and in every hosting
   organization. Delete the `.pem` file once it is pinned.

## Contributing

1. Branch from `master`.
2. Follow the naming convention.
3. Run `tofu fmt -recursive` and `tofu validate` before committing.
4. Open a pull request describing the change. The plan trigger posts each
   touched layer's plan on it; the merge applies.

A few things resist deletion on purpose. The boot project carries
`deletion_policy = "PREVENT"`, the folders carry `deletion_protection`, and
every zone carries `prevent_destroy`, so removing any takes a deliberate
change (`1-dns/README.md`, "Making a zone again"). Custom role IDs cannot be
reused for a period after deletion, so rename rather than delete and recreate.
