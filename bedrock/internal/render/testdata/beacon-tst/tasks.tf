# ---------------------------------------------------------------------------
# Cloud Tasks
#
# Nothing here yet. A task queue appears when the code declares one: a
# configuration variable named APP_TASKS_QUEUE, at the level whose processes
# enqueue tasks (the data level as a rule). The stack then creates
# imp-<env>-<region>-beacon-tasks in the primary region, sets the variable to its
# resource name, and lets the site (and the job process, when it constructs
# that level) enqueue on it and sign tasks as itself for the call back.
# ---------------------------------------------------------------------------
