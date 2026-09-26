# harbor's infrastructure

The application stack of [harbor](https://github.com/impulseframework/harbor):
everything the application needs in one environment that is not the
environment itself. It lives here, in the application repository's
`infrastructure` directory, and is applied once per environment, as the
application apply identity `imp-<env>-gbl-harbor-tofu` that `2-env` created.
Its state lives at `3-app/harbor/<env>` in the organization's state bucket,
the layer's slot there.

Every resource here is derived from something harbor declares, and each one
says in a comment which declaration (a struct field under `pkg/config`, a
route, a command) it comes from. bedrock will later generate these files from
the code; until then they are the hand-written statement of that derivation.

## Applying

Same shape as `2-env`: no workspaces, one state prefix per environment
(`3-app/harbor/<env>`, the stack's slot in the organization's state bucket),
supplied at init, with a backend cache per environment:

```bash
cd infrastructure
export TF_DATA_DIR=.terraform.tst
tofu init -backend-config="prefix=3-app/harbor/tst"
tofu plan -var environment=tst
tofu apply -var environment=tst
```

`2-env` for the same environment must be applied first (identities,
connection, repository link, records bucket, instance), `2-shr` before that
(the registry, with the deploy identity in its `pushers`), for stg and prd
`2-spn` with the apply identity in its `database_admins`, and `2-net`
afterwards with this stack's `net_hosts` output in its `hosts`. The stack uses
the environment project as its quota project (`user_project_override`), read
from `2-env`'s state.

## What it creates

- **Runtime identities**, one per process:
  `imp-<env>-gbl-harbor-app` for the site (`main.go`) with
  `roles/logging.logWriter`, `roles/cloudtrace.agent`,
  `roles/monitoring.metricWriter` on the project, `roles/spanner.databaseUser`
  on the database, and accessor on the secrets; `imp-<env>-gbl-harbor-migrate`
  for the migration job (`cmd/deployment/migrate`) with
  `roles/logging.logWriter` and `roles/spanner.databaseAdmin` on the database
  only, for DDL. The deploy identity from `2-env` gets
  `roles/iam.serviceAccountUser` on both.
- **The database** `imp-<env>-gbl-harbor-db` on the environment's instance
  (`2-env` output `spanner_instance`: tst's own, the spn instance for stg and
  prd), GoogleSQL, no schema (the migrations own it). prd: deletion and drop
  protection on, a weekly full backup (Sundays 02:00 UTC) and a daily
  incremental one (02:00 UTC), each kept 90 days.
- **Secret containers**, no versions, one per secret the code declares in
  `pkg/config/data.go`, named `imp-<env>-gbl-harbor-<name>`:

  | Variable | Field | Container |
  |---|---|---|
  | `APP_COOKIE_KEY` | `dataConfig.CookieKey` | `...-cookie-key` |
  | `APP_STAFF_OIDC_CLIENT_SECRET` | `dataConfig.StaffClientSecret` | `...-staff-oidc-client-secret` |
  | `APP_STAFF_OIDC_ADMIN_CREDENTIALS` | `dataConfig.StaffAdminCredentials` | `...-staff-oidc-admin-credentials` |

  Only the site's identity holds accessor. The migrate step constructs the
  same configuration level, but the session library reads these values only
  when someone signs in (the cookie key falls back to an ephemeral one), which
  a migration never does. The design brief's rule of thumb, accessor for every
  process that constructs the level, would grant the migrate identity too;
  this is the narrower reading, and a fork for bedrock's derivation to settle.
- **Cloud Run**: the service `imp-<env>-<uc1|uw3>-harbor-app` in both regions
  (ingress internal and load balancer, 0 to 2 instances, CPU only during
  requests, `allUsers` invoker so the load balancer can forward) and the job
  `imp-<env>-uc1-harbor-migrate` (one task, no retries, 15-minute timeout),
  both created with a placeholder image. From the first deploy on, the image
  and the labels and annotations a deploy stamps are the pipeline's
  (`ignore_changes`); identity, scaling, variables, and secret mounts stay
  this stack's.
- **Load balancer backend**: a serverless NEG per region and one global
  backend service `imp-<env>-gbl-harbor-backend` over both, external managed,
  outlier detection on (5 consecutive errors in a 1-second interval eject a
  backend for 30 seconds, at most 50% ejected, enforced at 100), request
  logging at full sample rate. No Cloud Armor. The URL map in the net project
  routes this environment's hostnames to it across projects (below).
- **Cloud Build triggers** on the repository link `2-env` registered, running
  `cloudbuild.yaml` as the deploy identity: `imp-<env>-uc1-harbor-version` on a
  tag `^v\d+\.\d+\.\d+$` in every environment, with Cloud Build approval
  required in stg and prd; `imp-tst-uc1-harbor-pr` in tst only, on a pull
  request against `master`, run only on a `/gcbrun` comment.

### Configuration the processes receive

By level (`pkg/config`): a process gets the levels it constructs and nothing
above them.

| Variable | Level | Value | Service | Job |
|---|---|---|---|---|
| `APP_SERVICE_NAME` | core | `harbor` / `harbor-migrate` | yes | yes |
| `GOOGLE_CLOUD_LOGGING_PROJECT` | core | the environment project | yes | yes |
| `GOOGLE_CLOUD_SPANNER_PROJECT`, `_INSTANCE_ID`, `_DATABASE_NAME` | data | the database | yes | yes |
| `APP_STAFF_OIDC_HOSTED_DOMAIN` | data | `var.staff_oidc_hosted_domain` | yes | yes |
| `APP_STAFF_OIDC_GROUP_PREFIX` | data | `var.staff_oidc_group_prefix` | yes | yes |
| `APP_STAFF_OIDC_CLIENT_ID` | data | `var.staff_oidc_client_id[env]` | yes | |
| `APP_STAFF_OIDC_REDIRECT_URL` | data | `https://<first hostname>/api/user/callback` | yes | |
| `APP_STAFF_OIDC_ADMIN_SUBJECT` | data | `var.staff_oidc_admin_subject[env]` | yes | |
| `APP_COOKIE_KEY`, `APP_STAFF_OIDC_CLIENT_SECRET`, `APP_STAFF_OIDC_ADMIN_CREDENTIALS` | data | secret, at the pinned version | yes | |

The job carries the hosted domain and group prefix because the session
library refuses to construct without them. Not set: `APP_VERSION` (the
pipeline bakes it into the image, so a deploy never edits the template's
variables), `APP_DEFAULT_SESSION_TIMEOUT` (code default), `PORT` (Cloud Run
sets it), `APP_CONSOLE_DIST` (where the image put the bundle).

### Secret versions

`var.secret_versions` pins, per environment and per variable, the version the
environment runs. A secret with no pin has its container but is not mounted:
the process starts without the variable, which is "not yet". After an
operator adds a value (`gcloud secrets versions add`, as a Secret Version
Adder), the pin is bumped in `terraform.tfvars` in the same pull request as
the rotation and rolled out by the next apply and deploy. `latest` is allowed
only where the map says so, for a secret whose placement marks it as tracking;
a pinned number is the default posture.

## The pipeline's contract

What `cloudbuild.yaml` in the harbor repository can rely on, from the trigger
substitutions and this stack's outputs:

- `_ENV`, `_APP`, `_PROJECT`; `_SERVICES` as `<region>=<service>` per region,
  comma-separated; `_MIGRATE_JOB` as `<region>=<job>`; `_REGISTRY` as
  `<hostname>/<shr project>/<repository>`; `_RECORDS_BUCKET`;
  `_REPO_CONNECTION_NAME` and `_REPO_NAME` (placeholders until 2-env holds the
  connection). Output `substitutions` is the same map, for a build submitted
  by hand before the triggers exist.
- The services and the job are deployed with `gcloud run services update
  --image` and `gcloud run jobs update --image` then `gcloud run jobs execute
  --wait`, which leave the template's variables and secrets alone: the
  revision template is this stack's, a deploy changes the image and its labels.
- Two images per release in the one repository, `harbor:<tag>` for the site
  and `harbor-migrate:<tag>` for the job, with `APP_VERSION` baked in at build.
- `options.logging: CLOUD_LOGGING_ONLY`, required when a build runs as a
  user-specified service account.
- The deploy identity writes one object per run into `_RECORDS_BUCKET`, at
  `<app>/<env>/<release>/<build>.json`, and can never overwrite one; a
  release that runs again in an environment (a re-run activates a placement
  change with the same image) adds a record, and the newest under the
  release's prefix is its current one.

## Hostnames and the net layer

`var.hostnames` gives each environment its hostnames, in `2-net`'s
convention of one label under the apps domain (its wildcard certificate
covers exactly that): `harbor-tst.impulseframework.dev`,
`harbor-stg.impulseframework.dev`, `harbor.impulseframework.dev`. The first is
canonical and forms the staff sign-in's redirect URL. `2-net` routes a
hostname from an entry in its `hosts` variable, hostname to backend service
URI, in its `terraform.tfvars`; this stack's `net_hosts` output is exactly
those entries for the environment:

```
hosts = {
  "harbor-tst.impulseframework.dev" = "projects/<tst project>/global/backendServices/imp-tst-gbl-harbor-backend"
}
```

DNS (the wildcard A record) and the certificate already cover the hostname;
adding the entry and applying `2-net` is the whole registration.

## Hand steps

Per environment, after the first apply:

1. In the Google Cloud console, in the environment project, APIs & Services >
   Credentials > Create credentials > OAuth client ID, type Web application,
   authorized redirect URI = output `staff_oidc_redirect_url`. Put the client
   ID in `terraform.tfvars` (`staff_oidc_client_id`) and add the client secret
   as version 1 of `imp-<env>-gbl-harbor-staff-oidc-client-secret`.
2. Generate a cookie key (`openssl rand -base64 32`) and add it as version 1
   of `imp-<env>-gbl-harbor-cookie-key`.
3. For the directory read: a service-account key with domain-wide delegation
   for the Admin SDK groups scope as version 1 of
   `imp-<env>-gbl-harbor-staff-oidc-admin-credentials`, and the administrator
   it impersonates in `staff_oidc_admin_subject`. The lab's org policy forbids
   creating service account keys under the environment folders, so where that
   key comes from is an open question (below).
4. Pin the versions in `secret_versions` and apply.

## Inputs

| Name | Description | Type | Default | Required |
|---|---|---|---|:---:|
| `environment` | `tst`, `stg`, or `prd`; passed as `-var` on every run. | `string` | n/a | yes |
| `hostnames` | Hostnames per environment; the first is canonical. | `map(list(string))` | the three above | no |
| `placeholder_image` | Image the services and job are created with. | `string` | `us-docker.pkg.dev/cloudrun/container/hello` | no |
| `secret_versions` | Pinned secret version per environment per variable. | `map(map(string))` | all empty | no |
| `staff_oidc_admin_subject` | Impersonated Workspace administrator, per environment. | `map(string)` | all empty | no |
| `staff_oidc_client_id` | OAuth client ID, per environment. | `map(string)` | all empty | no |
| `staff_oidc_group_prefix` | Prefix of the role groups. | `string` | `"staff-"` | no |
| `staff_oidc_hosted_domain` | Workspace domain logins are restricted to. | `string` | `"impulseframework.com"` | no |
| `state_bucket` | State bucket, for the upstream layers' outputs. | `string` | n/a | yes |

## Outputs

| Name | Description |
|---|---|
| `backend_service_id`, `backend_service_self_link` | The backend service, as a `projects/.../global/backendServices/...` URI and as a full self link. |
| `database` | `{ project, instance, name }`. |
| `hostnames` | For `2-net`'s host rules, certificate, and DNS. |
| `identities` | `{ app, migrate }` runtime identity emails. |
| `migrate_job` | `{ name, region }` of the migration job. |
| `net_hosts` | The `hosts` entries for `2-net`: each hostname mapped to the backend service URI. |
| `registry` | `<hostname>/<project>/<repository>`; null until `2-shr` registers harbor. |
| `secrets` | Per variable: `secret_id` and the pinned `version` (null when unpinned). |
| `services` | Per region code: `name`, `region`, `uri`. |
| `staff_oidc_redirect_url` | The redirect URI to register on the OAuth client. |
| `substitutions` | What the triggers pass to `cloudbuild.yaml`. |
| `triggers` | `{ version, pr }` trigger IDs (`pr` null outside tst). |

## Upstream outputs assumed

| Layer | Output | Used for |
|---|---|---|
| `1-org` | (read for completeness; nothing used directly yet) | |
| `2-env` | `applications[harbor]`, `spanner_instance`, `records_bucket`, `project_id`, `prefix`, `region`, `region_code`, `secondary_region`, `secondary_region_code` | everything; the organization's infrastructure repository defines them |
| `2-shr` | `image_paths` | `_REGISTRY`: `<registry hostname>/<shr project>/<repository>` for harbor |

`2-shr` grants `roles/artifactregistry.reader` on every repository to each
environment's Cloud Run service agent itself (`pull_environments`), so a
revision here can pull its image with nothing further.

## Open questions

- The Admin SDK credential is a service-account key, and `1-org` enforces
  `iam.managed.disableServiceAccountKeyCreation` on every environment folder.
  Either the key is minted in a project outside those folders, the policy gets
  a per-project exception, or the session library learns keyless domain-wide
  delegation (`iamcredentials.signJwt` with a subject). Until decided, the
  container exists and stays empty.
- Outlier detection with serverless NEGs on an external managed backend
  service validates against the provider schema; whether the API accepts this
  exact parameter set is confirmed at the first apply.
- The application apply identity's role set is a hypothesis (design brief):
  creating a second-generation trigger, and building as a user-specified
  service account from a second-generation repository, may want
  `cloudbuild.repositories.get` or similar. The first apply and first build
  read the denials.
