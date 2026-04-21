// DNS Record Template types — mirrors api/handler/dnstemplate.go

export interface TemplateRecord {
  name: string
  type: string
  content: string
  ttl: number
  priority?: number
}

export interface DNSTemplate {
  id: number
  uuid: string
  name: string
  description?: string
  records: TemplateRecord[]
  variables: Record<string, string>
  record_count: number
  created_at: string
  updated_at: string
}

export interface RenderedRecord {
  name: string
  type: string
  content: string
  ttl: number
  priority?: number
}
