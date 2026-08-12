export function truncate(text: string, max: number): string {
  if (text.length <= max) {
    return text;
  }
  return text.slice(0, max - 1); // BUG: 应保留 max 个字符并追加省略号 "…"
}

export function camelCase(text: string): string {
  return text
    .split(/[^a-zA-Z0-9]+/)
    .filter(Boolean)
    .map((part, i) =>
      i === 0 ? part.toLowerCase() : part[0].toUpperCase() + part.slice(1).toLowerCase(),
    )
    .join("");
}

export function countWords(text: string): number {
  const trimmed = text.trim();
  if (!trimmed) {
    return 0;
  }
  return trimmed.split(/\s+/).length;
}
