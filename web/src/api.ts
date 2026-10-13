// Types and calls for /api/v1. The UI has no access the API doesn't give
// everyone else.

export interface Session {
  project: string;
  session_id: string;
  engine: string;
  build: string;
  platform: string;
  tester: string;
}

export interface Sprite {
  interval: number;
  cols: number;
  rows: number;
  width: number;
  height: number;
  count: number;
  sheets: number;
  url: string;
}

export interface Stage {
  status: "queued" | "running" | "done" | "failed";
  version: number;
  model?: string;
  attempts: number;
  error?: string;
}

export interface Recording {
  id: number;
  path: string;
  name: string;
  size: number;
  recorded_at: string;
  duration: number | null;
  fps: number | null;
  width: number | null;
  height: number | null;
  audio_tracks: number | null;
  sync_mode: string;
  has_sidecar: boolean;
  missing: boolean;
  session: Session | null;
  proxy_url?: string;
  sprite: Sprite | null;
  stages: Record<string, Stage>;
}

export interface Match {
  source: "event" | "transcript";
  type?: "event" | "span" | "marker";
  kind?: string;
  label?: string;
  text?: string;
  tags?: string[];
  importance?: number;
  t: number;
  dur?: number;
}

export interface Clip {
  recording_id: number;
  t_in: number;
  t_out: number;
  score: number;
  matches: Match[];
}

export interface SearchResult {
  clips: Clip[];
  recordings: Record<string, Recording>;
}

export interface Facet {
  value: string;
  count: number;
}

export type Facets = Record<
  "kinds" | "tags" | "maps" | "builds" | "testers" | "platforms" | "projects",
  Facet[]
>;

export interface Event {
  id: number;
  type: "event" | "span" | "marker";
  t: number;
  dur?: number;
  kind: string;
  label?: string;
  source?: string;
  tags: string[] | null;
  importance: number;
  data: Record<string, unknown>;
}

export interface Line {
  track: number;
  t_in: number;
  t_out: number;
  text: string;
}

export interface SelectItem {
  id?: number;
  recording_id: number;
  t_in: number;
  t_out: number;
  comment: string;
}

export interface SelectSummary {
  id: number;
  name: string;
  owner: string;
  notes: string;
  item_count: number;
  duration: number;
  created_at: string;
  updated_at: string;
}

export interface SelectDetail extends SelectSummary {
  items: SelectItem[];
  recordings: Record<string, Recording>;
}

export interface Job {
  id: number;
  recording_id: number;
  recording: string;
  stage: string;
  version: number;
  model?: string;
  status: Stage["status"];
  attempts: number;
  worker: string | null;
  error: string | null;
  result: Record<string, unknown> | null;
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
}

export interface Config {
  export_targets: { name: string; prefix: string }[];
  transcribe_model: string;
}

/** Filters as they appear in the URL and the API query string. */
export interface SearchParams {
  q?: string;
  kind?: string[];
  tag?: string[];
  map?: string[];
  build?: string[];
  tester?: string[];
  importance?: number;
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    ...init,
    headers: init?.body ? { "Content-Type": "application/json" } : undefined,
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => null)) as { error?: string } | null;
    throw new ApiError(res.status, body?.error ?? res.statusText);
  }
  return (res.status === 204 || res.status === 202 ? undefined : await res.json()) as T;
}

export function searchQuery(p: SearchParams): string {
  const qs = new URLSearchParams();
  if (p.q) qs.set("q", p.q);
  for (const key of ["kind", "tag", "map", "build", "tester"] as const) {
    for (const v of p[key] ?? []) qs.append(key, v);
  }
  if (p.importance) qs.set("importance", String(p.importance));
  return qs.toString();
}

export const api = {
  config: () => call<Config>("/config"),
  search: (p: SearchParams) => call<SearchResult>(`/search?${searchQuery(p)}`),
  facets: () => call<Facets>("/facets"),
  recordings: () => call<{ recordings: Recording[]; total: number }>("/recordings?limit=500"),
  recording: (id: number) => call<Recording>(`/recordings/${id}`),
  events: (id: number) => call<Event[]>(`/recordings/${id}/events`),
  transcript: (id: number) => call<Line[]>(`/recordings/${id}/transcript`),
  reprocess: (id: number, stage: string) =>
    call<void>(`/recordings/${id}/reprocess?stage=${encodeURIComponent(stage)}`, { method: "POST" }),
  jobs: () => call<Job[]>("/jobs"),
  selects: () => call<SelectSummary[]>("/selects"),
  select: (id: number) => call<SelectDetail>(`/selects/${id}`),
  createSelect: (name: string) =>
    call<SelectDetail>("/selects", { method: "POST", body: JSON.stringify({ name }) }),
  updateSelect: (id: number, patch: { name?: string; notes?: string }) =>
    call<SelectDetail>(`/selects/${id}`, { method: "PATCH", body: JSON.stringify(patch) }),
  deleteSelect: (id: number) => call<void>(`/selects/${id}`, { method: "DELETE" }),
  addToSelect: (id: number, item: SelectItem) =>
    call<SelectDetail>(`/selects/${id}/items`, { method: "POST", body: JSON.stringify(item) }),
  setSelectItems: (id: number, items: SelectItem[]) =>
    call<SelectDetail>(`/selects/${id}/items`, {
      method: "PUT",
      body: JSON.stringify(
        items.map(({ id, recording_id, t_in, t_out, comment }) => ({ id, recording_id, t_in, t_out, comment })),
      ),
    }),
  exportUrl: (id: number, target?: string) =>
    `/api/v1/selects/${id}/export?format=otio${target ? `&target=${encodeURIComponent(target)}` : ""}`,
};
