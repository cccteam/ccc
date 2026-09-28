# 2-shr

The shared services project: the container registry. One Docker repository
per application, and the grants that let each application's pipeline push and
each environment's Cloud Run pull. Applied by the bootstrap administrator for now, later by
Cloud Build in the boot project as `imp-shr-gbl-tofu`.

## What it creates

- One Artifact Registry Docker repository per entry in `applications`, in the
  primary region: `imp-shr-uc1-<application>`, so `imp-shr-uc1-harbor` to
  start. Tags are immutable: a tag names one digest forever, so what stg
  approved is exactly what prd runs, and a rebuild gets a new tag.
- Cleanup policies on each repository: untagged versions older than
  `untagged_retention_days` (7) are deleted; the newest
  `keep_tagged_versions` (10) versions of each image are exempt from every
  delete. Tagged versions are never deleted unless `tagged_retention_days` is
  set. `cleanup_dry_run` makes the policies log instead of delete.
  Vulnerability scanning is left at the project default.
- `roles/artifactregistry.reader` on every repository for the Cloud Run
  service agent of each environment in `pull_environments` (tst, stg, prd).
- `roles/artifactregistry.writer` on an application's repository for each
  member listed under it in `pushers`.

## Who pushes, who pulls

Two kinds of identity touch a repository, and they are bound in two different
ways on purpose.

Pulls are bound here, from remote state. Cloud Run does not pull an image as
the service's runtime identity; it pulls as the Cloud Run service agent of the
project the service runs in, `service-<project number>@serverless-robot-prod.iam.gserviceaccount.com`.
That agent is fully determined by the project number, which `1-org` publishes,
and it exists as soon as the Cloud Run API is enabled, which `1-org` did. So
this layer can grant reader to every environment without the environment
layers naming anything, and a new environment in `1-org` becomes a puller by
appearing in `pull_environments`.

Pushes are bound here, from a variable. The pusher is the application's deploy
identity in each environment project, a service account the environment layer
creates, and IAM refuses to bind a member that does not exist. The
environment layer cannot make the binding itself either: its identity holds
roles on its own project, not on `shr`. So the deploy identities are listed in
`pushers`, per application, after the environment layers have run for that
application, and this layer, whose identity does hold `artifactregistry.admin`
on `shr`, makes the grants. Registering an application is therefore two runs
of this layer: one that creates the repository, and one after the environment
layers that adds its pushers and pullers. The pullers are the application's
apply identities: Cloud Run checks that whoever creates a revision can read
its image, and a revision that comes from a placement change rather than a
deploy is the apply identity's, carrying the image the last deploy left on the
service (found on the first pull request that switched databases).

The alternative, reading the environment layers' state for their deploy
identities, was rejected: those layers read this one for repository IDs, and
two layers reading each other's state only works with `try()` on both sides
and an apply order nobody can see in the code.

## GitHub Actions

`github.tf` gives the organization's GitHub Actions an identity: a workload
identity pool whose provider trusts GitHub's OpenID Connect tokens for
repositories owned by `impulseframework` (and no other), and one grant,
reading the tools repository as that principal set. An application's
infrastructure workflow authenticates with the provider (the output
`github_identity_provider`, recorded in the application's placement as
`githubIdentityProvider`), pulls the bedrock image its placement pins, and runs
that bedrock's `check`: the same checker the pipeline runs, by digest. No
service account and no key stand in between.

## Applying

In a terminal, in this directory, after `1-org` has been applied. The bucket
name in the backend block and in the remote state block needs the same
substitution as the earlier layers.

```bash
cd 2-shr
tofu init
tofu plan
tofu apply
```

The first `init` writes `.terraform.lock.hcl`; commit it. Until the Cloud
Build runner is wired, the bootstrap administrator applies this layer, and the remote state
read works on that user's credentials; the layer identity will need object
read on the `1-org` prefix of the bucket when it takes over.

## Inputs

| Name | Description | Type | Default | Required |
|---|---|---|---|:---:|
| `applications` | Applications that get a repository, by short name. | `list(string)` | `["harbor", "beacon"]` | no |
| `cleanup_dry_run` | Cleanup policies log instead of delete. | `bool` | `false` | no |
| `keep_tagged_versions` | Newest versions of each image exempt from every delete. | `number` | `10` | no |
| `pull_environments` | Environment codes whose Cloud Run service agent may pull. | `list(string)` | `["tst", "stg", "prd"]` | no |
| `pushers` | IAM members granted writer, keyed by application. | `map(list(string))` | `{}` | no |
| `pullers` | IAM members granted reader beside the Cloud Run service agents, keyed by application: the apply identities, since Cloud Run checks that the creator of a revision can read its image. | `map(list(string))` | `{}` | no |
| `tagged_retention_days` | When set, tagged versions older than this are deleted. | `number` | `null` | no |
| `untagged_retention_days` | Untagged versions older than this are deleted. | `number` | `7` | no |

Project IDs, project numbers, the boot project, the region, and the prefix are
read from `1-org`'s state rather than declared.

## Outputs

Read by the environment and application layers through
`data "terraform_remote_state"` on the state bucket, prefix `2-shr`.

| Name | Description |
|---|---|
| `gcp_region` | Region the repositories live in. |
| `image_paths` | `{registry_hostname}/{project}/{repository}` by application. |
| `project_id` | Shared services project. |
| `registry_hostname` | `us-central1-docker.pkg.dev`. |
| `repository_ids` | Full resource ID of each repository, by application. |
| `repository_names` | Repository ID (last path segment), by application. |

## Notes

- Multi-architecture pushes (`docker buildx` with attestations or a manifest
  list) leave the per-platform manifests untagged. Before relying on the
  untagged cleanup with such builds, run it with `cleanup_dry_run = true`
  and read what it would delete.
- The repositories are regional in us-central1. Cloud Run in us-west3 pulls
  across regions, which works and costs a little egress; a `us` multi-region
  repository would avoid that at the price of a name outside the region-code
  convention. Decided for the region; revisit if the egress shows up.

The layer also holds the tools repository (`<prefix>-shr-<region>-tools`), which
carries bedrock's image: every application's pipeline runs its deploy steps
from it, at the digest the application's placement pins (`bedrockImage`), and
every application's deploy identity may read it. An operator pushes the image
for now, built from the bedrock module; a release pipeline follows.
