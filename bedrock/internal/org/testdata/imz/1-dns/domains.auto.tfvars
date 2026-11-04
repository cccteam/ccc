# Rendered by bedrock from placement.json (domains): the domains this
# organization holds a zone for, each with the labels of it handed onward to
# the hosting organizations' zones. Owned: bedrock org render rewrites it and
# bedrock org check compares it, so a change here is a change to placement.json.

domains = {
  "clientbay.example" = {
    labels = {
      "beacon" = { organization = "rgb", name_servers = ["ns-cloud-d1.googledomains.com.", "ns-cloud-d2.googledomains.com.", "ns-cloud-d3.googledomains.com.", "ns-cloud-d4.googledomains.com."] }
      "harbor" = { organization = "imp", name_servers = ["ns-cloud-c1.googledomains.com.", "ns-cloud-c2.googledomains.com.", "ns-cloud-c3.googledomains.com.", "ns-cloud-c4.googledomains.com."] }
    }
  }
  "impapps.example" = {
    labels = {
      "a" = { organization = "imp", name_servers = ["ns-cloud-e1.googledomains.com.", "ns-cloud-e2.googledomains.com.", "ns-cloud-e3.googledomains.com.", "ns-cloud-e4.googledomains.com."] }
    }
  }
  "zones.imp.example" = {
    labels = {}
  }
}
