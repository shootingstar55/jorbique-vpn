import { computed, ref } from 'vue';
import { darkTheme, lightTheme } from 'naive-ui';

/**
 * The app's Naive UI theme.
 *
 * Naive supplies the components; this file supplies the identity — the emerald/slate dark
 * palette the client has always used, plus pill buttons and soft, translucent surfaces so
 * cards read as glass over the radial gradient on <body>.
 *
 * Colours are the literal Tailwind values the design started from, kept as hex so they stay
 * comparable with anything still written in utility classes:
 *   emerald-600 #059669 / emerald-500 #10b981   primary
 *   rose-600    #e11d48 / rose-500    #f43f5e   danger
 *   slate-900   #0f172a / slate-600   #475569   surfaces and borders
 */
const overrides = {
  common: {
    primaryColor: '#059669',
    primaryColorHover: '#10b981',
    primaryColorPressed: '#047857',
    primaryColorSuppl: '#10b981',

    errorColor: '#e11d48',
    errorColorHover: '#f43f5e',
    errorColorPressed: '#be123c',
    errorColorSuppl: '#f43f5e',

    successColor: '#10b981',
    successColorHover: '#34d399',
    successColorPressed: '#059669',
    successColorSuppl: '#34d399',

    warningColor: '#f59e0b',
    warningColorHover: '#fbbf24',
    warningColorPressed: '#d97706',
    warningColorSuppl: '#fbbf24',

    infoColor: '#0ea5e9',
    infoColorHover: '#38bdf8',
    infoColorPressed: '#0284c7',
    infoColorSuppl: '#38bdf8',

    // Transparent body keeps the radial gradient on <body> visible behind every surface.
    bodyColor: 'transparent',
    cardColor: 'rgba(15, 23, 42, 0.62)',
    modalColor: 'rgba(15, 23, 42, 0.92)',
    popoverColor: 'rgba(15, 23, 42, 0.96)',
    tableColor: 'rgba(15, 23, 42, 0.62)',
    tableColorHover: 'rgba(30, 41, 59, 0.5)',
    inputColor: 'rgba(2, 6, 23, 0.65)',
    inputColorDisabled: 'rgba(2, 6, 23, 0.35)',
    actionColor: 'rgba(30, 41, 59, 0.5)',
    hoverColor: 'rgba(51, 65, 85, 0.45)',

    borderColor: 'rgba(71, 85, 105, 0.55)',
    dividerColor: 'rgba(51, 65, 85, 0.55)',

    textColorBase: '#f1f5f9',
    textColor1: '#f8fafc',
    textColor2: '#cbd5e1',
    textColor3: '#94a3b8',
    textColorDisabled: '#64748b',
    placeholderColor: '#64748b',
    placeholderColorDisabled: '#475569',
    iconColor: '#94a3b8',
    iconColorHover: '#cbd5e1',
    closeIconColor: '#94a3b8',
    closeIconColorHover: '#f1f5f9',

  },

  Card: {
    color: 'rgba(15, 23, 42, 0.62)',
    borderColor: 'rgba(148, 163, 184, 0.14)',
  },

  Button: {
    colorTertiary: 'rgba(30, 41, 59, 0.75)',
    colorHoverTertiary: 'rgba(51, 65, 85, 0.9)',
    colorPressedTertiary: 'rgba(30, 41, 59, 0.95)',
    textColorTertiary: '#e2e8f0',
    borderTertiary: '1px solid rgba(100, 116, 139, 0.4)',
    borderHoverTertiary: '1px solid rgba(148, 163, 184, 0.5)',
    borderPressedTertiary: '1px solid rgba(148, 163, 184, 0.5)',
    borderFocusTertiary: '1px solid rgba(148, 163, 184, 0.5)',
  },

  Input: {
    border: '1px solid rgba(71, 85, 105, 0.6)',
    borderHover: '1px solid rgba(100, 116, 139, 0.8)',
    borderFocus: '1px solid rgba(16, 185, 129, 0.55)',
    boxShadowFocus: '0 0 0 2px rgba(16, 185, 129, 0.25)',
    caretColor: '#10b981',
  },

  Checkbox: {
    colorChecked: '#10b981',
    checkMarkColor: '#022c22',
    border: '1px solid rgba(100, 116, 139, 0.7)',
    borderChecked: '1px solid #10b981',
    borderFocus: '1px solid #10b981',
    boxShadowFocus: '0 0 0 2px rgba(16, 185, 129, 0.28)',
  },

  Switch: {
    railColorActive: '#059669',
    loadingColor: '#10b981',
    boxShadowFocus: '0 0 0 2px rgba(16, 185, 129, 0.28)',
  },

  Progress: {
    fillColor: '#10b981',
    railColor: 'rgba(51, 65, 85, 0.6)',
  },

  Scrollbar: {
    color: 'rgba(100, 116, 139, 0.5)',
    colorHover: 'rgba(148, 163, 184, 0.7)',
  },

  Message: {
    colorInfo: 'rgba(12, 74, 110, 0.92)',
    colorSuccess: 'rgba(6, 78, 59, 0.92)',
    colorError: 'rgba(76, 5, 25, 0.92)',
    textColorInfo: '#e0f2fe',
    textColorSuccess: '#d1fae5',
    textColorError: '#ffe4e6',
    iconColorInfo: '#38bdf8',
    iconColorSuccess: '#34d399',
    iconColorError: '#fb7185',
  },

  Empty: {
    textColor: '#94a3b8',
    iconColor: '#475569',
  },

  Divider: {
    color: 'rgba(51, 65, 85, 0.55)',
  },
};

/**
 * Which theme the window paints in: the system's, or one the user pinned.
 *
 * The dark overrides above are the client's identity — translucent slate
 * surfaces over a radial gradient — and none of that reads on a light
 * background, so the light theme takes only the palette (the emerald primary,
 * the rose danger) and lets Naive supply its own surfaces.
 */
const MODE_KEY = 'jorbiquevpn.theme';
const MODES = ['system', 'dark', 'light'];

function storedMode() {
  // macOS serves every window from its own loopback port — a separate origin
  // with its own localStorage — so the client passes the choice in the URL and
  // that wins. Windows has one origin and uses localStorage.
  const fromURL = new URLSearchParams(window.location.search).get('theme');
  if (MODES.includes(fromURL)) return fromURL;
  try {
    const saved = localStorage.getItem(MODE_KEY);
    if (MODES.includes(saved)) return saved;
  } catch {
    /* private windows and file:// origins throw on access; the default covers it */
  }
  return 'system';
}

export const themeMode = ref(storedMode());

const media = typeof window.matchMedia === 'function' ? window.matchMedia('(prefers-color-scheme: dark)') : null;
const systemDark = ref(media ? media.matches : true);
media?.addEventListener('change', (event) => {
  systemDark.value = event.matches;
});

export const isDark = computed(() => themeMode.value === 'dark' || (themeMode.value === 'system' && systemDark.value));

export function setThemeMode(mode) {
  if (!MODES.includes(mode)) return;
  themeMode.value = mode;
  try {
    localStorage.setItem(MODE_KEY, mode);
  } catch {
    /* the choice still applies to this window; it just will not be remembered */
  }
}

export const naiveTheme = computed(() => (isDark.value ? darkTheme : lightTheme));
/**
 * The light theme keeps the brand palette and nothing else: the rest of the
 * overrides above are slate surfaces and near-white text, which on a light
 * background produced dark grey input boxes on a white card.
 *
 * Which is why the overrides carry colours only. A size, a padding or a radius
 * among them is dropped by this filter and kept by the dark theme, so the two
 * would lay out differently — a card padding, a card radius and a font size did
 * exactly that, and the dark window came out a hundred pixels shorter than the
 * light one. Sizing lives in the components' own CSS, which both themes share.
 */
const lightCommon = Object.fromEntries(
  Object.entries(overrides.common).filter(([key]) =>
    /^(primary|error|success|warning|info)Color/.test(key),
  ),
);

// Both themes, because a radius is not a colour and the filter above would drop
// it: Naive's own default is 3px, which draws the buttons and the theme switch
// as rectangles.
const shape = { borderRadius: '12px', borderRadiusSmall: '9px' };

export const naiveThemeOverrides = computed(() =>
  isDark.value
    ? { ...overrides, common: { ...overrides.common, ...shape } }
    : { common: { ...lightCommon, ...shape, bodyColor: 'transparent' } },
);
