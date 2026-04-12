export interface ProjectResponse {
  id:          number
  uuid:        string
  name:        string
  slug:        string
  description: string | null
  created_at:  string
  updated_at:  string
}

export interface CreateProjectRequest {
  name:        string
  slug:        string
  description?: string
}

export interface PrefixRuleResponse {
  id:             number
  project_id:     number | null
  prefix:         string
  purpose:        string
  dns_provider:   string
  cdn_provider:   string
  nginx_template: string
  html_template:  string | null
}
