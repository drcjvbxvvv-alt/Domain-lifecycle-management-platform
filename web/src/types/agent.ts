import type { AgentStatus } from './common'

// Mirror of store/postgres Agent + AgentStateHistoryRow DTOs.

export interface AgentResponse {
  id:            number
  uuid:          string
  agent_id:      string
  hostname:      string
  ip:            string | null
  region:        string | null
  datacenter:    string | null
  agent_version: string | null
  status:        AgentStatus
  last_seen_at:  string | null
  last_error:    string | null
  created_at:    string
  updated_at:    string
}

export interface AgentStateHistoryEntry {
  id:           number
  agent_id:     number
  from_state:   AgentStatus | null
  to_state:     AgentStatus
  reason:       string | null
  triggered_by: string
  created_at:   string
}
