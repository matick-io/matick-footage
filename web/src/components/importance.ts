import type { Match } from "../api";

/** Timeline and badge colour for an event, by importance; markers stand apart. */
export function eventColor(e: { type?: string; importance?: number }): string {
  if (e.type === "marker") return "var(--color-status-success)";
  switch (e.importance ?? 0) {
    case 3:
      return "var(--color-status-danger)";
    case 2:
      return "var(--color-status-warning)";
    case 1:
      return "var(--color-status-info)";
    default:
      return "var(--color-text-muted)";
  }
}

export function topMatch(matches: Match[]): Match | undefined {
  return [...matches].sort(
    (a, b) =>
      Number(b.source === "event") - Number(a.source === "event") ||
      (b.importance ?? 0) - (a.importance ?? 0),
  )[0];
}
