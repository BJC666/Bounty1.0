export function relativeTime(secondsAgo: number): string {
  if (secondsAgo < 60) {
    return `${Math.floor(secondsAgo)} 秒前`;
  }
  if (secondsAgo < 3600) {
    return `${Math.floor(secondsAgo / 60)} 分钟前`;
  }
  if (secondsAgo < 86400) {
    return `${Math.floor(secondsAgo / 3600)} 小时前`;
  }
  return `${Math.floor(secondsAgo / 86400)} 天前`;
}
