export type SSLStatus = 'active' | 'expiring' | 'expired' | 'revoked'

export interface SSLCertResponse {
  id:            number
  uuid:          string
  domain_id:     number
  issuer:        string | null
  cert_type:     string | null
  serial_number: string | null
  issued_at:     string | null  // YYYY-MM-DD
  expires_at:    string         // YYYY-MM-DD
  days_left:     number
  auto_renew:    boolean
  status:        SSLStatus
  last_check_at: string | null  // ISO datetime
  notes:         string | null
  created_at:    string
  updated_at:    string
}

export interface CreateSSLCertRequest {
  issuer?:        string | null
  cert_type?:     string | null
  serial_number?: string | null
  issued_at?:     string | null  // YYYY-MM-DD
  expires_at:     string         // YYYY-MM-DD, required
  auto_renew?:    boolean
  notes?:         string | null
}

export interface SSLCheckRequest {
  fqdn: string
}
