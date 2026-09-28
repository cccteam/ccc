# ---------------------------------------------------------------------------
# Cloud Storage
#
# Nothing here yet. A bucket for the application's files appears when the code
# declares one: a configuration variable named APP_ASSETS_BUCKET, at the level
# whose processes read and write it (the data level as a rule). The stack then
# creates imp-<env>-gbl-beacon-assets-<project number> in the primary
# region, sets the variable to its name, and grants the site (and the job
# process, when it constructs that level) objectUser on it.
# ---------------------------------------------------------------------------
