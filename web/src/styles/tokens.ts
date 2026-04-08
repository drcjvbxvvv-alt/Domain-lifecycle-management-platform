/**
 * Design Tokens — Single source of truth for all visual values.
 *
 * Theme: Soft Light — comfortable, elegant, fresh.
 *
 * Rules:
 *  - NEVER hardcode a color/spacing/font-size in a component.
 *  - ALWAYS use a value from this file or the corresponding CSS variable.
 *  - Adding a new token requires updating BOTH this file AND global.css.
 */

// ── Colors ─────────────────────────────────────────────────────────────────

export const colors = {
  // Backgrounds
  bgPage:    '#f0f4f8',  // page root — soft blue-gray wash
  bgCard:    '#ffffff',  // card, table, modal surface
  bgInput:   '#f8fafc',  // input, select background
  bgHover:   'rgba(79, 126, 248, 0.05)',  // row hover — barely-there blue tint
  bgSidebar: '#ffffff',  // sidebar surface

  // Borders & dividers
  border:    '#e4e9f2',
  borderSub: '#f1f5fb',  // subtler divider inside cards

  // Text
  textPrimary:   '#1a2233',
  textSecondary: '#4b5a6e',
  textMuted:     '#8b97a8',

  // Brand / primary
  primary:        '#4f7ef8',  // clear sky blue — confident, approachable
  primaryHover:   '#3b6ef0',
  primaryPressed: '#2c5de0',

  // Domain status — muted for light backgrounds
  status: {
    inactive:  { color: '#64748b', bg: 'rgba(100,116,139,0.08)' },
    deploying: { color: '#d97706', bg: 'rgba(217,119,6,0.08)'   },
    active:    { color: '#16a34a', bg: 'rgba(22,163,74,0.08)'   },
    degraded:  { color: '#ea580c', bg: 'rgba(234,88,12,0.08)'   },
    switching: { color: '#7c3aed', bg: 'rgba(124,58,237,0.08)'  },
    suspended: { color: '#b45309', bg: 'rgba(180,83,9,0.08)'    },
    failed:    { color: '#dc2626', bg: 'rgba(220,38,38,0.08)'   },
    blocked:   { color: '#dc2626', bg: 'rgba(220,38,38,0.08)'   },
    retired:   { color: '#94a3b8', bg: 'rgba(148,163,184,0.08)' },
  },

  // Alert severity — crisp but not glaring
  severity: {
    P0:   { color: '#dc2626', bg: 'rgba(220,38,38,0.08)'   },
    P1:   { color: '#ea580c', bg: 'rgba(234,88,12,0.08)'   },
    P2:   { color: '#d97706', bg: 'rgba(217,119,6,0.08)'   },
    P3:   { color: '#2563eb', bg: 'rgba(37,99,235,0.08)'   },
    INFO: { color: '#16a34a', bg: 'rgba(22,163,74,0.08)'   },
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
  normal:   400,
  medium:   500,
  semibold: 600,
  bold:     700,
} as const

export const lineHeight = {
  tight:   1.3,
  normal:  1.5,
  relaxed: 1.6,
} as const

// ── Borders ─────────────────────────────────────────────────────────────────

export const borderRadius = {
  sm:   '4px',
  base: '6px',
  md:   '8px',
  lg:   '12px',
  full: '9999px',
} as const

// ── Shadows — light, layered, modern ─────────────────────────────────────────

export const shadow = {
  xs:    '0 1px 2px rgba(15,23,42,0.04)',
  card:  '0 1px 3px rgba(15,23,42,0.06), 0 4px 12px rgba(15,23,42,0.06)',
  modal: '0 8px 40px rgba(15,23,42,0.14), 0 2px 8px rgba(15,23,42,0.08)',
  glow:  '0 0 0 3px rgba(79,126,248,0.18)',
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
// Light theme — do NOT pass darkTheme in App.vue

import type { GlobalThemeOverrides } from 'naive-ui'

export const naiveThemeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor:        colors.primary,
    primaryColorHover:   colors.primaryHover,
    primaryColorPressed: colors.primaryPressed,
    primaryColorSuppl:   colors.primaryPressed,

    bodyColor:          colors.bgPage,
    cardColor:          colors.bgCard,
    modalColor:         colors.bgCard,
    popoverColor:       colors.bgCard,
    tableColor:         colors.bgCard,
    tableColorHover:    colors.bgHover,
    tableHeaderColor:   '#f8fafc',
    inputColor:         colors.bgInput,
    inputColorDisabled: '#f1f5f9',

    borderColor:   colors.border,
    dividerColor:  colors.border,

    textColorBase: colors.textPrimary,
    textColor1:    colors.textPrimary,
    textColor2:    colors.textSecondary,
    textColor3:    colors.textMuted,
    placeholderColor:    colors.textMuted,
    scrollbarColor:      '#d1d9e6',
    scrollbarColorHover: '#aab5c8',

    fontFamily: "system-ui, -apple-system, 'Segoe UI', Roboto, 'Helvetica Neue', sans-serif",
    fontSize:   '14px',
    borderRadius: '8px',
  },
  Button: {
    borderRadiusMedium: '8px',
    borderRadiusLarge:  '8px',
    borderRadiusSmall:  '6px',
    fontWeightStrong:   '600',
  },
  Input: {
    borderRadius: '8px',
    color:        colors.bgInput,
    colorFocus:   colors.bgCard,
    border:       `1px solid ${colors.border}`,
    borderHover:  `1px solid ${colors.primary}`,
    borderFocus:  `1px solid ${colors.primary}`,
    boxShadowFocus: `0 0 0 3px rgba(79,126,248,0.15)`,
  },
  Select: {
    peers: {
      InternalSelection: {
        borderRadius: '8px',
        border:       `1px solid ${colors.border}`,
        borderHover:  `1px solid ${colors.primary}`,
        borderFocus:  `1px solid ${colors.primary}`,
        boxShadowFocus: `0 0 0 3px rgba(79,126,248,0.15)`,
      },
    },
  },
  DataTable: {
    thPaddingMedium:    '0 16px',
    tdPaddingMedium:    '0 16px',
    thFontWeight:       '600',
    thTextColor:        colors.textMuted,
    borderRadius:       '10px',
  },
  Card: {
    borderRadius: '12px',
    boxShadow:    shadow.card,
    paddingMedium:'20px 24px',
  },
  Modal: {
    borderRadius: '14px',
    boxShadow:    shadow.modal,
  },
  Tag: {
    borderRadius: '6px',
  },
}
