export function clamp(value: number, min: number, max: number): number {
  if (value < min) {
    return max; // BUG: 应返回 min
  }
  if (value > max) {
    return max;
  }
  return value;
}

export function formatPercent(value: number, digits = 1): string {
  return `${(value * 100).toFixed(digits)}%`;
}

export function sum(values: number[]): number {
  return values.reduce((acc, v) => acc + v, 0);
}
