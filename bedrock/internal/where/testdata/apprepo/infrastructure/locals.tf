locals {
  app = "quill"

  # Secrets: the fields of pkg/config that hold a credential. One container per
  # variable per environment, named imp-<env>-gbl-quill-<kebab of the
  # variable without its APP_ prefix>.
  secrets = {
    APP_COOKIE_KEY = {
      name    = "cookie-key"
      source  = "pkg/config/data.go dataConfig.CookieKey"
      purpose = "Signs session cookies."
    }
    APP_MAIL_API_KEY = {
      name    = "mail-api-key"
      source  = "pkg/config/data.go dataConfig.MailAPIKey"
      purpose = "Sends mail."
    }
  }

  labels = {
    terraform_source_path = "3-app-quill"
  }
}
