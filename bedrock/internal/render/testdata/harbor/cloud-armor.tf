# ---------------------------------------------------------------------------
# Cloud Armor
#
# The security policy on the backend services (load-balancer.tf), off unless
# terraform.tfvars turns it on in this environment (cloud_armor: "preview"
# evaluates the rules and logs what each would have done, "enforce" applies
# them). Rules run in priority order and the first match decides. A rule set
# that reads no body (scanner detection) runs first, on every path. Then the
# bypasses: each a route whose body is a file or a third party's rather than
# the application's JSON, read from pkg/router/zz_gen_release.json (every
# @upload method's route and every stored file's) and from placement.json
# (cloudArmor.bypasses), allowed so that no rule below reads a body written for
# text. Then the other rule sets, each scoped to the outlets' routes
# (/api), where the input a rule can judge arrives; the scope
# comes first in the expression, so a request elsewhere short-circuits and the
# set never runs on it. Every other request is allowed. Each rule's description
# names what it comes from. A pull-request stack has no policy: the environment
# layer serves previews from one backend service, and a policy attaches to a
# backend service.
# ---------------------------------------------------------------------------

locals {
  cloud_armor_mode    = lookup(var.cloud_armor, var.environment, "off")
  cloud_armor_on      = !local.is_pr && local.cloud_armor_mode != "off"
  cloud_armor_preview = local.cloud_armor_mode == "preview"
}

resource "google_compute_security_policy" "app" {
  count = local.cloud_armor_on ? 1 : 0

  project     = local.project_id
  name        = "${local.name}-gbl-${local.app}-armor"
  description = "harbor in ${var.environment}: the web application firewall's rules, ${local.cloud_armor_preview ? "in preview, logging what each rule would do" : "enforced"}."
  type        = "CLOUD_ARMOR"

  # scanner detection (scannerdetection-v33-stable, sensitivity 1) on every path; placement.json cloudArmor.ruleSets
  rule {
    priority    = 1000
    action      = "deny(403)"
    description = "scanner detection (scannerdetection-v33-stable, sensitivity 1) on every path; placement.json cloudArmor.ruleSets"
    preview     = local.cloud_armor_preview

    match {
      expr {
        expression = "evaluatePreconfiguredWaf('scannerdetection-v33-stable', {'sensitivity': 1})"
      }
    }
  }

  # POST /api/attach-manifest; the @upload method AttachManifest, pkg/router/zz_gen_release.json; its body is a file, not JSON
  rule {
    priority    = 2000
    action      = "allow"
    description = "POST /api/attach-manifest; the @upload method AttachManifest, pkg/router/zz_gen_release.json; its body is a file, not JSON"

    match {
      expr {
        expression = "request.method == 'POST' && request.path.matches('^/api/attach-manifest$')"
      }
    }
  }

  # GET /api/manifests/{id}/file; the @file column Manifest.Key, pkg/router/zz_gen_release.json; its answer is the stored object
  rule {
    priority    = 2001
    action      = "allow"
    description = "GET /api/manifests/{id}/file; the @file column Manifest.Key, pkg/router/zz_gen_release.json; its answer is the stored object"

    match {
      expr {
        expression = "request.method == 'GET' && request.path.matches('^/api/manifests/[^/]+/file$')"
      }
    }
  }

  # POST /hooks/registry; placement.json cloudArmor.bypasses; the registry's webhook body is signed by the registry, which the Root hook verifies
  rule {
    priority    = 2002
    action      = "allow"
    description = "POST /hooks/registry; placement.json cloudArmor.bypasses; the registry's webhook body is signed by the registry, which the Root hook verifies"

    match {
      expr {
        expression = "request.method == 'POST' && request.path.matches('^/hooks/registry$')"
      }
    }
  }

  # /streams/*; placement.json cloudArmor.bypasses; the manifest stream is bytes, not JSON
  rule {
    priority    = 2003
    action      = "allow"
    description = "/streams/*; placement.json cloudArmor.bypasses; the manifest stream is bytes, not JSON"

    match {
      expr {
        expression = "request.path.matches('^/streams/.*$')"
      }
    }
  }

  # SQL injection (sqli-v33-stable, sensitivity 1) on /api; placement.json cloudArmor.ruleSets
  rule {
    priority    = 3000
    action      = "deny(403)"
    description = "SQL injection (sqli-v33-stable, sensitivity 1) on /api; placement.json cloudArmor.ruleSets"
    preview     = local.cloud_armor_preview

    match {
      expr {
        expression = "request.path.matches('^/(api)/') && evaluatePreconfiguredWaf('sqli-v33-stable', {'sensitivity': 1})"
      }
    }
  }

  # SQL injection in JSON bodies (json-sqli-canary, sensitivity 1) on /api; placement.json cloudArmor.ruleSets
  rule {
    priority    = 3010
    action      = "deny(403)"
    description = "SQL injection in JSON bodies (json-sqli-canary, sensitivity 1) on /api; placement.json cloudArmor.ruleSets"
    preview     = local.cloud_armor_preview

    match {
      expr {
        expression = "request.path.matches('^/(api)/') && evaluatePreconfiguredWaf('json-sqli-canary', {'sensitivity': 1})"
      }
    }
  }

  # cross-site scripting (xss-v33-stable, sensitivity 2) on /api; placement.json cloudArmor.ruleSets
  rule {
    priority    = 3020
    action      = "deny(403)"
    description = "cross-site scripting (xss-v33-stable, sensitivity 2) on /api; placement.json cloudArmor.ruleSets"
    preview     = local.cloud_armor_preview

    match {
      expr {
        expression = "request.path.matches('^/(api)/') && evaluatePreconfiguredWaf('xss-v33-stable', {'sensitivity': 2})"
      }
    }

    preconfigured_waf_config {
      # the session cookie is random bytes, which the cross-site scripting rules misread
      exclusion {
        target_rule_set = "xss-v33-stable"
        request_cookie {
          operator = "EQUALS"
          value    = "harbor_session"
        }
      }
    }
  }

  # protocol attacks (protocolattack-v33-stable, sensitivity 1) on /api; placement.json cloudArmor.ruleSets
  rule {
    priority    = 3030
    action      = "deny(403)"
    description = "protocol attacks (protocolattack-v33-stable, sensitivity 1) on /api; placement.json cloudArmor.ruleSets"
    preview     = local.cloud_armor_preview

    match {
      expr {
        expression = "request.path.matches('^/(api)/') && evaluatePreconfiguredWaf('protocolattack-v33-stable', {'sensitivity': 1})"
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
