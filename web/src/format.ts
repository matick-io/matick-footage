import { formatClock } from "@matick-io/ui-timeline";
import type { Match } from "./api";

export { formatClock };

export function formatRange(tIn: number, tOut: number): string {
  return `${formatClock(tIn)}–${formatClock(tOut)}`;
}

export function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds.toFixed(1)}s`;
  return formatClock(seconds);
}

export function formatDate(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** "v0.3.0" for a release, "main · 4f9cc60" for a branch build, "dev" locally. */
export function formatVersion(version: string, commit: string): string {
  if (/^\d+\.\d+\.\d+/.test(version)) return `v${version}`;
  return commit ? `${version} · ${commit.slice(0, 7)}` : version;
}

/** One line describing why a clip matched. */
export function describeMatch(m: Match): string {
  if (m.source === "transcript") return `“${m.text ?? ""}”`;
  if (m.type === "marker") return `Tester marker${m.tags?.length ? ` · ${m.tags.join(", ")}` : ""}`;
  return m.label ? `${m.label}` : (m.kind ?? "event");
}
