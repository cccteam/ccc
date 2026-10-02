# ---------------------------------------------------------------------------
# Firestore
#
# Nothing here yet. A document database beside the Spanner database appears
# when the code declares one: a configuration variable named
# APP_FIRESTORE_DATABASE, at the level whose processes read and write it (the
# data level as a rule). The stack then creates imp-<env>-gbl-beacon-fs in
# Native mode in the primary region, sets the variable to its id, and grants
# the site (and the job process, when it constructs that level) datastore.user
# on that database alone. With it, from the two files beside the schema
# migrations (schema/firestore), the database's composite indexes, its
# time-to-live policies and its security rules; Token Creator for the site on
# its own account, for the custom tokens live pages sign a browser in with;
# and, when the code also declares APP_FIREBASE_API_KEY, the web API key the
# browser presents, that variable set to it.
# ---------------------------------------------------------------------------
