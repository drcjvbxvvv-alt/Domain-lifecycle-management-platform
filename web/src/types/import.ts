// Types for domain CSV import jobs — mirrors Go's importJobResponse DTO.

export type ImportJobStatus = 'pending' | 'fetching' | 'processing' | 'completed' | 'failed'

export interface ImportJob {
  id: number
  uuid: string
  project_id: number
  registrar_account_id?: number
  source_type: string
  status: ImportJobStatus
  total_count: number
  imported_count: number
  skipped_count: number
  failed_count: number
  error_details?: string   // JSON-encoded RowError[]
  created_by?: number
  started_at?: string
  completed_at?: string
  created_at: string
}

export interface ParsedRow {
  fqdn: string
  expiry_date?: string
  auto_renew: boolean
  registrar_account_id?: number
  dns_provider_id?: number
  tags?: string[]
  notes?: string
}

export interface RowError {
  line: number
  fqdn?: string
  reason: string
}

export interface PreviewResult {
  valid_count: number
  error_count: number
  rows: ParsedRow[]
  errors: RowError[]
}
