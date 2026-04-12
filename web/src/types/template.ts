// Mirror of internal/template DTOs. Keep in sync with Go backend.

export interface TemplateResponse {
  id:          number
  uuid:        string
  project_id:  number
  name:        string
  description: string | null
  created_at:  string
  updated_at:  string
}

export interface TemplateVersionResponse {
  id:                number
  uuid:              string
  template_id:       number
  version_label:     string
  html_body:         string | null
  nginx_conf:        string | null
  runtime_fields:    string[]        // variable names declared by template
  checksum:          string
  published_at:      string | null
  created_at:        string
}
