/**
 * Design Tokens — Single source of truth for all visual values.
 *
 * Rules:
 *  - NEVER hardcode a color/spacing/font-size in a component.
 *  - ALWAYS use a value from this file or the corresponding CSS variable.
 *  - Adding a new token requires updating BOTH this file AND global.css.
 */

// ── Colors ─────────────────────────────────────────────────────────────────

export const colors = {
  // Backgrounds
  bgPage:    '#0f172a',  // page root background
  bgCard:    '#1e293b',  // card, table, modal surface
  bgInput:   '#0f172a',  // input, select background
  bgHover:   'rgba(56, 189, 248, 0.06)',  // row hover

  // Borders & dividers
  border:    '#334155',
  borderSub: '#1e293b',  // subtler divider inside cards

  // Text
  textPrimary:   '#f1f5f9',
  textSecondary: '#94a3b8',
  textMuted:     '#64748b',

  // Brand / primary
  primary:        '#38bdf8',
  primaryHover:   '#7dd3fc',
  primaryPressed: '#0ea5e9',

  // Domain status
  status: {
    inactive:  { color: '#64748b', bg: 'rgba(100,116,139,0.12)' },
    deploying: { color: '#fbbf24', bg: 'rgba(251,191,36,0.12)'  },
    active:    { color: '#4ade80', bg: 'rgba(74,222,128,0.12)'  },
    degraded:  { color: '#fb923c', bg: 'rgba(251,146,60,0.12)'  },
    switching: { color: '#c084fc', bg: 'rgba(192,132,252,0.12)' },
    suspended: { color: '#facc15', bg: 'rgba(250,204,21,0.12)'  },
    failed:    { color: '#f87171', bg: 'rgba(248,113,113,0.12)' },
    blocked:   { color: '#ef4444', bg: 'rgba(239,68,68,0.12)'   },
    retired:   { color: '#475569', bg: 'rgba(71,85,105,0.12)'   },
  },

  // Alert severity
  severity: {
    P0:   { color: '#ef4444', bg: 'rgba(239,68,68,0.12)'   },
    P1:   { color: '#f97316', bg: 'rgba(249,115,22,0.12)'  },
    P2:   { color: '#eab308', bg: 'rgba(234,179,8,0.12)'   },
    P3:   { color: '#60a5fa', bg: 'rgba(96,165,250,0.12)'  },
    INFO: { color: '#4ade80', bg: 'rgba(74,222,128,0.12)'  },
  },
} as const

// ── Spacing (4px base grid) ─────────────────────────────────────────────────

export const spacing = {
  1:  '4px',
  2:  '8px',
  3:  '12px',
  4:  '16px',
  5:  '20px',
  6:  '24px',
  8:  '32px',
  10: '40px',
  12: '48px',
  16: '64px',
} as const

// ── Typography ──────────────────────────────────────────────────────────────

export const fontSize = {
  xs:   '12px',
  sm:   '13px',
  base: '14px',
  md:   '16px',
  lg:   '20px',
  xl:   '24px',
  '2xl':'28px',
} as const

export const fontWeight = {
  normal:  400,
  medium:  500,
  semibold:600,
  bold:    700,
} as const

export const lineHeight = {
  tight:  1.3,
  normal: 1.5,
  relaxed:1.6,
} as const

// ── Borders ─────────────────────────────────────────────────────────────────

export const borderRadius = {
  sm:   '4px',
  base: '6px',
  md:   '8px',
  lg:   '12px',
  full: '9999px',
} as const

// ── Shadows ─────────────────────────────────────────────────────────────────

export const shadow = {
  card:  '0 1px 3px rgba(0,0,0,0.4), 0 1px 2px rgba(0,0,0,0.3)',
  modal: '0 20px 60px rgba(0,0,0,0.6)',
  glow:  '0 0 0 2px rgba(56,189,248,0.35)',
} as const

// ── Layout constants ─────────────────────────────────────────────────────────

export const layout = {
  sidebarWidth:    '220px',
  headerHeight:    '56px',
  pageHeaderHeight:'64px',
  tableRowHeight:  '48px',
  searchBarHeight: '52px',
  contentMaxWidth: '1400px',
  contentPadding:  '24px',
} as const

// ── Naive UI theme overrides (imported in App.vue) ──────────────────────────

import type { GlobalThemeOverrides } from 'naive-ui'

export const naiveThemeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor:        colors.primary,
    primaryColorHover:   colors.primaryHover,
    primaryColorPressed: colors.primaryPressed,
    primaryColorSuppl:   colors.primaryPressed,

    bodyColor:        colors.bgPage,
    cardColor:        colors.bgCard,
    modalColor:       colors.bgCard,
    popoverColor:     '#263147',
    tableColor:       colors.bgCard,
    tableColorHover:  colors.bgHover,
    tableHeaderColor: colors.bgPage,
    inputColor:       colors.bgInput,
    inputColorDisabled:'#1e293b',

    borderColor:   colors.border,
    dividerColor:  colors.border,

    textColorBase: colors.textPrimary,
    textColor1:    colors.textPrimary,
    textColor2:    '#cbd5e1',
    textColor3:    colors.textSecondary,
    placeholderColor:     colors.textMuted,
    scrollbarColor:       colors.border,
    scrollbarColorHover:  '#475569',
  },
}
