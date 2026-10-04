/**
 * Renders a byte count the way both the main screen and the dashboard show it:
 * the largest unit that leaves a number below 1024, one decimal below ten.
 */
export function formatBytes(value) {
  let amount = Math.max(0, Number(value) || 0);
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let unit = 0;
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024;
    unit += 1;
  }
  return `${amount >= 10 || unit === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[unit]}`;
}
