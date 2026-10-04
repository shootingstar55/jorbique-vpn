/** The TUN stacks sing-box offers, in the order both clients show them. */
export const STACK_OPTIONS = [
  { mode: 'system', title: 'System', subtitle: 'Recommended · stable' },
  { mode: 'gvisor', title: 'Gvisor', subtitle: 'Isolation · sandbox' },
  { mode: 'mixed', title: 'Mixed', subtitle: 'Hybrid mode' },
];

/**
 * How a stack is written in the UI: "System", not "system".
 *
 * The name comes from three places — a running profile file, a stored
 * preference, the macOS config — and each spells it its own way. The options
 * above are the spelling both windows show, so they are what a value is matched
 * against; anything unrecognised is capitalised and passed through.
 */
export function stackLabel(value) {
  const raw = String(value || '').trim();
  if (!raw) return '';
  const known = STACK_OPTIONS.find((option) => raw.toLowerCase().includes(option.mode));
  return known ? known.title : raw[0].toUpperCase() + raw.slice(1);
}
