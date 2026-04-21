// DNS record template API calls.
import { http } from '@/utils/http'
import type { DNSTemplate, RenderedRecord, TemplateRecord } from '@/types/dnstemplate'

interface ListResponse { items: DNSTemplate[]; total: number }
interface ApplyResponse { records: RenderedRecord[]; count: number }

export interface CreateTemplateRequest {
  name: string
  description?: string
  records: TemplateRecord[]
  variables?: Record<string, string>
}

export const dnsTemplateApi = {
  list(): Promise<{ data: ListResponse }> {
    return http.get('/dns-templates') as Promise<{ data: ListResponse }>
  },

  get(id: number): Promise<{ data: DNSTemplate }> {
    return http.get(`/dns-templates/${id}`) as Promise<{ data: DNSTemplate }>
  },

  create(req: CreateTemplateRequest): Promise<{ data: DNSTemplate }> {
    return http.post('/dns-templates', req) as Promise<{ data: DNSTemplate }>
  },

  update(id: number, req: CreateTemplateRequest): Promise<{ data: DNSTemplate }> {
    return http.put(`/dns-templates/${id}`, req) as Promise<{ data: DNSTemplate }>
  },

  delete(id: number): Promise<unknown> {
    return http.delete(`/dns-templates/${id}`)
  },

  /** Render a template's records with variable substitution.
   *  Returns records ready to be staged in the DNS management UI. */
  applyTemplate(domainId: number, templateId: number, variables: Record<string, string>): Promise<{ data: ApplyResponse }> {
    return http.post(`/domains/${domainId}/dns/apply-template`, {
      template_id: templateId,
      variables,
    }) as Promise<{ data: ApplyResponse }>
  },
}
