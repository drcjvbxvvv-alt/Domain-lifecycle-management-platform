// web/src/types/maintenance.ts — mirrors Go DTOs for maintenance windows.

export type MaintenanceStrategy = 'single' | 'recurring_weekly' | 'recurring_monthly' | 'cron'

export interface WeeklyRecurrence {
  weekdays: number[]          // 0=Sun … 6=Sat
  start_time: string          // "HH:MM"
  duration_minutes: number
  timezone: string
}

export interface MonthlyRecurrence {
  day_of_month: number
  start_time: string
  duration_minutes: number
  timezone: string
}

export interface CronRecurrence {
  expression: string
  duration_minutes: number
  timezone: string
}

export type Recurrence = WeeklyRecurrence | MonthlyRecurrence | CronRecurrence

export interface MaintenanceTarget {
  id: number
  target_type: 'domain' | 'host_group' | 'project'
  target_id: number
}

export interface MaintenanceWindowResponse {
  id: number
  uuid: string
  title: string
  description?: string
  strategy: MaintenanceStrategy
  start_at?: string
  end_at?: string
  recurrence?: Recurrence
  active: boolean
  created_at: string
  updated_at: string
}

export interface MaintenanceWindowWithTargets extends MaintenanceWindowResponse {
  targets: MaintenanceTarget[]
}

export interface CreateMaintenanceRequest {
  title: string
  description?: string
  strategy: MaintenanceStrategy
  start_at?: string
  end_at?: string
  recurrence?: Recurrence
  active?: boolean
}

export interface UpdateMaintenanceRequest extends CreateMaintenanceRequest {}

export interface AddTargetRequest {
  target_type: 'domain' | 'host_group' | 'project'
  target_id: number
}
