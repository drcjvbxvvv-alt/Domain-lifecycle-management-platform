import type { ReleaseStatus, ReleaseType } from './common'

// Mirror of internal/release DTOs (Go side). Keep in sync.

export interface ReleaseResponse {
  uuid:                 string
  release_id:           string                // "rel_01HXYZ..."
  project_id:           number
  project_name?:        string                // populated by handler join
  template_version_id:  number
  template_version?:    string                // version_label, populated by handler join
  artifact_id:          number | null
  release_type:         ReleaseType
  trigger_source:       'ui' | 'api' | 'webhook' | 'scheduler'
  status:               ReleaseStatus
  requires_approval:    boolean
  canary_shard_size:    number
  shard_size:           number
  total_domains:        number | null
  total_shards:         number | null
  success_count:        number
  failure_count:        number
  description:          string | null
  created_at:           string
  created_by:           number
  started_at:           string | null
  ended_at:             string | null
}

export interface CreateReleaseRequest {
  project_id:           number
  template_version_id:  number
  release_type:         ReleaseType
  domain_ids:           number[]
  host_group_ids?:      number[]
  description?:         string
}

export interface ReleaseShardResponse {
  id:            number
  shard_index:   number
  is_canary:     boolean
  domain_count:  number
  status:        'pending' | 'dispatching' | 'running' | 'paused' | 'succeeded' | 'failed' | 'cancelled'
  success_count: number
  failure_count: number
  pause_reason:  string | null
  started_at:    string | null
  ended_at:      string | null
}

export interface ReleaseStateHistoryEntry {
  id:           number
  from_state:   ReleaseStatus | null
  to_state:     ReleaseStatus
  reason:       string | null
  triggered_by: string
  created_at:   string
}

// P2.4 — Dry-run diff preview types
export interface DryRunResult {
  release_id:      string
  new_artifact_id: string
  old_artifact_id: string | null
  summary:         DiffSummary
  files:           FileDiff[]
}

export interface DiffSummary {
  added:     number
  removed:   number
  modified:  number
  unchanged: number
}

export interface FileDiff {
  path:   string
  change: 'added' | 'removed' | 'modified' | 'unchanged'
  diff?:  string
}
