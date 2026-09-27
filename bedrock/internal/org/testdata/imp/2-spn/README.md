# 2-spn

The shared Spanner project: one multi-region instance that every application's
stg and prd databases live on. Applied by the bootstrap administrator for now, later by
Cloud Build in the boot project as `imp-spn-gbl-tofu`.

## What it creates

- The Spanner instance `imp-spn-gbl-spanner` in the `spn` project:
  configuration `nam10` (read-write replicas in us-central1 and us-west3,
  witness in us-central2), 100 processing units, STANDARD edition, labelled
  `bedrock-lab`. No autoscaler. `force_destroy` is off and the resource
  carries `prevent_destroy`, so neither a plan nor a stray destroy can take
  the databases with it.
- `roles/spanner.databaseAdmin` on the instance for each member of
  `database_admins`, empty until the environment layers have created the
  application apply identities.

Not created here: databases. Each application's layer creates its own
databases on this instance, one for stg and one for prd, named within the 30
character database ID limit, and decides per database whether it gets an
automatic backup schedule (`default_backup_schedule_type` is left at the
instance default here for that reason). tst does not use this instance; its
own instance lives in the tst project with the pull-request databases.

## IAM

Two levels, two owners.

Instance level, this layer: creating a database is `spanner.databases.create`
checked on the instance, so the identity that creates databases, the
application apply identity in stg and prd, is listed in `database_admins` and
bound here as database admin. That role at instance level reaches every
database on the instance; bounding it to an application's own databases is a
open question, and until it is answered the list stays short.

Database level, the application layer: `roles/spanner.databaseUser` for each
runtime identity on its database, `roles/spanner.databaseAdmin` for the
migrate identity on its own database for DDL, and any reader entitlements.
Those grants name a database that only the application layer knows, so they
are made there, with `google_spanner_database_iam_member`.

## Capacity

100 processing units is the smallest a multi-region instance can be. The
decision of 2026-09-25 is never more than 200 without explicit agreement, and
`variables.tf` enforces it: `processing_units` must be 100 or 200. Raising the
cap is a change to the validation, visible in review, not a number in
`terraform.tfvars`.

## Applying

In a terminal, in this directory, after `1-org` has been applied. The bucket
name in the backend block and in the remote state block needs the same
substitution as the earlier layers.

```bash
cd 2-spn
tofu init
tofu plan
tofu apply
```

The first `init` writes `.terraform.lock.hcl`; commit it. Until the Cloud
Build runner is wired, the bootstrap administrator applies this layer.

## Inputs

| Name | Description | Type | Default | Required |
|---|---|---|---|:---:|
| `database_admins` | Members granted database admin on the instance. | `list(string)` | `[]` | no |
| `edition` | Spanner edition. | `string` | `"STANDARD"` | no |
| `processing_units` | Compute capacity; 100 or 200. | `number` | `100` | no |
| `spanner_config` | Instance configuration. | `string` | `"nam10"` | no |

Project ID, the boot project, and the prefix are read from `1-org`'s state
rather than declared.

## Outputs

Read by the application layers through `data "terraform_remote_state"` on the
state bucket, prefix `2-spn`.

| Name | Description |
|---|---|
| `instance_id` | `projects/{project}/instances/imp-spn-gbl-spanner`. |
| `instance_name` | `imp-spn-gbl-spanner`, for `google_spanner_database.instance`. |
| `processing_units` | Compute capacity. |
| `project_id` | The `spn` project; a database resource names this project. |
| `spanner_config` | `nam10`. |
