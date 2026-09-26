# Head comment, kept.

/* A block comment
   over two lines, kept. */
secret_versions = { # opened here
  // The integration environment.
  tst = {
    APP_COOKIE_KEY = "2" # rotated last week
    # A comment at the end of the map, kept before a new entry.
    APP_MAIL_API_KEY = "4"
  }
  stg = {} # nothing yet
  prd = {}
} # closed here
# Tail comment, kept.
