export function median(values: number[]): number {
  const ordered = [...values].sort((a, b) => a - b);
  const n = ordered.length;
  if (n === 0) {
    throw new Error("values must not be empty");
  }
  if (n % 2 === 1) {
    return ordered[(n - 1) / 2];
  }
  return (ordered[n / 2 - 1] + ordered[n / 2]) / 2;
}
