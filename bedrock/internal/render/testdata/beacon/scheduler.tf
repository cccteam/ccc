# ---------------------------------------------------------------------------
# Cloud Scheduler
#
# Nothing here yet. A job appears when the code marks a method @schedule, with
# a cron expression and a time zone: the generated router serves it under
# /_scheduled and lists it in the release file beside the router. The stack
# then creates, in every environment but never in a pull-request stack, an
# invoker identity imp-<env>-gbl-beacon-sched and one job per route,
# imp-<env>-<region>-beacon-sched-<route>, in the primary region,
# which calls the route on the environment's canonical host with that
# identity's token, and sets APP_SCHEDULER_INVOKER on the service to its email.
# ---------------------------------------------------------------------------
