# ---------------------------------------------------------------------------
# Cloud Armor
#
# The security policy on the backend services (load-balancer.tf), off unless
# terraform.tfvars turns it on in this environment (cloud_armor: "preview"
# evaluates the rules and logs what each would have done, "enforce" applies
# them). Rules run in priority order and the first match decides. A rule set
# that reads no body (scanner detection) runs first, on every path. Then the
# bypasses: each a route whose body is a file or a third party's rather than
# the application's JSON, read from apps/console/pkg/router/zz_gen_release.json (every
# @upload method's route and every stored file's) and from placement.json
# (cloudArmor.bypasses), allowed so that no rule below reads a body written for
# text. Then the other rule sets, each scoped to the outlets' routes
# (/api), where the input a rule can judge arrives; the scope
# comes first in the expression, so a request elsewhere short-circuits and the
# set never runs on it. Every other request is allowed. Each rule's description
# names what it comes from. A pull-request stack has no policy: the environment
# layer serves previews from one backend service, and a policy attaches to a
# backend service.
#
# The sign-in throttles come after the rule sets and before the default rule:
# one per route of the generated router that takes a credential by POST (each
# session outlet's password login and its pending sign-in's confirmation and
# cancel), keyed by client address, so that past var.cloud_armor_sign_in_count
# POSTs to the route in var.cloud_armor_sign_in_interval seconds (both set in
# terraform.tfvars) an address answers 429 on it until the interval passes.
# After the rule sets so that a sign-in POST is still read by them: a rule
# that matches decides, and a throttle under its threshold allows. Preview
# logs what a throttle would have done, as it does for the rule sets.
#
# "off" keeps the policy and detaches it from the backend services; removing
# the environment's entry deletes it. The two are separate applies on purpose:
# an apply that deletes the policy while the backend services still name it
# fails, because OpenTofu destroys the policy before it updates the services
# (a dependency edge orders creation, not this), and Compute refuses to delete
# a policy in use. Off first, then removed, each apply changes what it can.
# ---------------------------------------------------------------------------

locals {
  # The environment's mode: "preview", "enforce", "off" (the policy kept,
  # detached), or empty, the environment left out (no policy).
  cloud_armor_mode     = lookup(var.cloud_armor, var.environment, "")
  cloud_armor_declared = !local.is_pr && local.cloud_armor_mode != ""
  cloud_armor_on       = local.cloud_armor_declared && local.cloud_armor_mode != "off"
  cloud_armor_preview  = local.cloud_armor_mode != "enforce"
}

resource "google_compute_security_policy" "app" {
  count = local.cloud_armor_declared ? 1 : 0

  project     = local.project_id
  name        = "${local.name}-gbl-${local.app}-armor"
  description = "lantrn in ${var.environment}: the web application firewall's rules, ${local.cloud_armor_mode == "off" ? "off, kept and detached from the backend services" : local.cloud_armor_preview ? "in preview, logging what each rule would do" : "enforced"}."
  type        = "CLOUD_ARMOR"

  # scanner detection (scannerdetection-v33-stable, sensitivity 1) on every path; bedrock's default rule sets
  rule {
    priority    = 1000
    action      = "deny(403)"
    description = "scanner detection (scannerdetection-v33-stable, sensitivity 1) on every path; bedrock's default rule sets"
    preview     = local.cloud_armor_preview

    match {
      expr {
        expression = "evaluatePreconfiguredWaf('scannerdetection-v33-stable', {'sensitivity': 1})"
      }
    }
  }

  # GET /api/notices/{id}/file; the @file column Notice.Key, apps/console/pkg/router/zz_gen_release.json; its answer is the stored object
  rule {
    priority    = 2000
    action      = "allow"
    description = "GET /api/notices/{id}/file; the @file column Notice.Key, apps/console/pkg/router/zz_gen_release.json; its answer is the stored object"

    match {
      expr {
        expression = "request.method == 'GET' && request.path.matches('^/api/notices/[^/]+/file$')"
      }
    }
  }

  # SQL injection (sqli-v33-stable, sensitivity 1) on /api; bedrock's default rule sets
  rule {
    priority    = 3000
    action      = "deny(403)"
    description = "SQL injection (sqli-v33-stable, sensitivity 1) on /api; bedrock's default rule sets"
    preview     = local.cloud_armor_preview

    match {
      expr {
        expression = "request.path.startsWith('/api/') && evaluatePreconfiguredWaf('sqli-v33-stable', {'sensitivity': 1})"
      }
    }
  }

  # SQL injection in JSON bodies (json-sqli-canary, sensitivity 1) on /api; bedrock's default rule sets
  rule {
    priority    = 3010
    action      = "deny(403)"
    description = "SQL injection in JSON bodies (json-sqli-canary, sensitivity 1) on /api; bedrock's default rule sets"
    preview     = local.cloud_armor_preview

    match {
      expr {
        expression = "request.path.startsWith('/api/') && evaluatePreconfiguredWaf('json-sqli-canary', {'sensitivity': 1})"
      }
    }
  }

  # cross-site scripting (xss-v33-stable, sensitivity 1) on /api; bedrock's default rule sets
  rule {
    priority    = 3020
    action      = "deny(403)"
    description = "cross-site scripting (xss-v33-stable, sensitivity 1) on /api; bedrock's default rule sets"
    preview     = local.cloud_armor_preview

    match {
      expr {
        expression = "request.path.startsWith('/api/') && evaluatePreconfiguredWaf('xss-v33-stable', {'sensitivity': 1})"
      }
    }
  }

  # protocol attacks (protocolattack-v33-stable, sensitivity 1) on /api; bedrock's default rule sets
  rule {
    priority    = 3030
    action      = "deny(403)"
    description = "protocol attacks (protocolattack-v33-stable, sensitivity 1) on /api; bedrock's default rule sets"
    preview     = local.cloud_armor_preview

    match {
      expr {
        expression = "request.path.startsWith('/api/') && evaluatePreconfiguredWaf('protocolattack-v33-stable', {'sensitivity': 1})"
      }
    }
  }

  # POST /api/user/password/login, throttled by client address; the default outlet's password login, the generated router; past cloud_armor_sign_in_count POSTs in cloud_armor_sign_in_interval seconds (terraform.tfvars) an address answers 429 until the interval passes
  rule {
    priority    = 4000
    action      = "throttle"
    description = "POST /api/user/password/login, throttled by client address; the default outlet's password login, the generated router; past cloud_armor_sign_in_count POSTs in cloud_armor_sign_in_interval seconds (terraform.tfvars) an address answers 429 until the interval passes"
    preview     = local.cloud_armor_preview

    match {
      expr {
        expression = "request.method == 'POST' && request.path.matches('^/api/user/password/login$')"
      }
    }

    rate_limit_options {
      conform_action = "allow"
      exceed_action  = "deny(429)"
      enforce_on_key = "IP"

      rate_limit_threshold {
        count        = var.cloud_armor_sign_in_count
        interval_sec = var.cloud_armor_sign_in_interval
      }
    }
  }

  # POST /api/user/pending/confirm, throttled by client address; the default outlet's pending sign-in confirmation, the generated router; past cloud_armor_sign_in_count POSTs in cloud_armor_sign_in_interval seconds (terraform.tfvars) an address answers 429 until the interval passes
  rule {
    priority    = 4001
    action      = "throttle"
    description = "POST /api/user/pending/confirm, throttled by client address; the default outlet's pending sign-in confirmation, the generated router; past cloud_armor_sign_in_count POSTs in cloud_armor_sign_in_interval seconds (terraform.tfvars) an address answers 429 until the interval passes"
    preview     = local.cloud_armor_preview

    match {
      expr {
        expression = "request.method == 'POST' && request.path.matches('^/api/user/pending/confirm$')"
      }
    }

    rate_limit_options {
      conform_action = "allow"
      exceed_action  = "deny(429)"
      enforce_on_key = "IP"

      rate_limit_threshold {
        count        = var.cloud_armor_sign_in_count
        interval_sec = var.cloud_armor_sign_in_interval
      }
    }
  }

  # POST /api/user/pending/cancel, throttled by client address; the default outlet's pending sign-in cancel, the generated router; past cloud_armor_sign_in_count POSTs in cloud_armor_sign_in_interval seconds (terraform.tfvars) an address answers 429 until the interval passes
  rule {
    priority    = 4002
    action      = "throttle"
    description = "POST /api/user/pending/cancel, throttled by client address; the default outlet's pending sign-in cancel, the generated router; past cloud_armor_sign_in_count POSTs in cloud_armor_sign_in_interval seconds (terraform.tfvars) an address answers 429 until the interval passes"
    preview     = local.cloud_armor_preview

    match {
      expr {
        expression = "request.method == 'POST' && request.path.matches('^/api/user/pending/cancel$')"
      }
    }

    rate_limit_options {
      conform_action = "allow"
      exceed_action  = "deny(429)"
      enforce_on_key = "IP"

      rate_limit_threshold {
        count        = var.cloud_armor_sign_in_count
        interval_sec = var.cloud_armor_sign_in_interval
      }
    }
  }

  # Every other request is allowed: the default rule, which every policy ends
  # with.
  rule {
    priority    = 2147483647
    action      = "allow"
    description = "every other request; the default rule"

    match {
      versioned_expr = "SRC_IPS_V1"
      config {
        src_ip_ranges = ["*"]
      }
    }
  }
}
