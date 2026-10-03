# ---------------------------------------------------------------------------
# Cloud Storage
#
# Nothing here yet. A bucket for the application's files appears when the code
# declares a file store: a configuration variable named APP_FILE_STORE (the
# default store) or APP_FILE_STORE_<NAME> (a named one), at the level whose
# processes read and write it (the data level as a rule). The stack then
# creates imp-<env>-gbl-beacon-files-<project number> (files-<name> for a
# named store) in the primary region, sets the variable to its gs:// URL, and
# grants the site (and the job process, when it constructs that level)
# objectUser on it.
# ---------------------------------------------------------------------------
