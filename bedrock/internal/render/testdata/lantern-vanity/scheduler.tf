# ---------------------------------------------------------------------------
# Cloud Scheduler
#
# The scheduled routes: the methods the code marks @schedule, which the
# generated router serves under /_scheduled and lists in
# apps/console/pkg/router/zz_gen_release.json. Cloud Scheduler calls each with POST on its
# schedule, a cron expression read in its time zone, from one job per route in
# the primary region. A schedule calls a route on the running service and never
# runs a process of its own: a route that has a job to start starts it.
#
# Each job calls the route on the environment's canonical host (the first of
# its hostnames) with an OIDC token, an identity token Google signs, of the
# invoker identity below, minted for the route's URL. The service is open to
# the load balancer, so Cloud Run's IAM does not check the call: the framework
# in front of the routes does, admitting a token Google signed for the route's
# URL whose email is the one this stack sets on the service as
# APP_SCHEDULER_INVOKER (locals.tf, scheduler_env), and answering any other
# call 401. So the invoker holds no role. A pull-request stack creates neither
# the identity nor the jobs, as it gets no sweep, and leaves the variable
# unset, so its scheduled routes refuse every call.
# ---------------------------------------------------------------------------

locals {
  # From each site's router's zz_gen_release.json, by the route's name: the method's
  # name in kebab case, which names its job, behind the site's name, which also
  # names the hostname the job calls.
  scheduled_routes = {
    "console-close-stale-holds" = {
      site      = "console"
      path      = "/_scheduled/close-stale-holds"
      schedule  = "*/15 * * * *"
      time_zone = "UTC"
    }
    "portal-send-weekly-letter" = {
      site      = "portal"
      path      = "/_scheduled/send-weekly-letter"
      schedule  = "0 9 * * 1"
      time_zone = "Europe/London"
    }
  }
}

resource "google_service_account" "scheduler" {
  count = local.is_pr ? 0 : 1

  project      = local.project_id
  account_id   = local.scheduler_account
  display_name = "Scheduler SA - ${local.scheduler_account}"
  description  = "The identity Cloud Scheduler calls lantrn's scheduled routes as in ${var.environment}."
}

resource "google_cloud_scheduler_job" "scheduled" {
  for_each = local.is_pr ? {} : local.scheduled_routes

  project     = local.project_id
  region      = local.primary_region
  name        = "${local.name}-${local.primary_region_code}-${local.app}-sched-${each.key}"
  description = "Calls ${each.value.path} on lantrn's ${each.value.site} site on its schedule (@schedule)."
  schedule    = each.value.schedule
  time_zone   = each.value.time_zone

  http_target {
    http_method = "POST"
    uri         = "https://${local.site_hostnames[each.value.site][0]}${each.value.path}"

    oidc_token {
      service_account_email = local.scheduler_email
      audience              = "https://${local.site_hostnames[each.value.site][0]}${each.value.path}"
    }
  }

  depends_on = [google_service_account.scheduler]
}
