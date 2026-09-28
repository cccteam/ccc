# ---------------------------------------------------------------------------
# Firestore
#
# Nothing here yet. A document database beside the Spanner database appears
# when the code declares one: a configuration variable named
# APP_FIRESTORE_DATABASE, at the level whose processes read and write it (the
# data level as a rule). The stack then creates imp-<env>-gbl-beacon-fs in
# Native mode in the primary region, sets the variable to its id, and grants
# the site (and the job process, when it constructs that level) datastore.user
# on that database alone.
# ---------------------------------------------------------------------------
