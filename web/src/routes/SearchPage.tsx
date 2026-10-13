import { useMemo } from "react";
import { getRouteApi, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Badge, EmptyState, Select } from "@khaos-systems/ui-core";
import {
  CountBadge,
  FilterChips,
  SearchField,
  Tile,
  VirtualTileGrid,
  formatCount,
  type ActiveFilter,
} from "@khaos-systems/ui-assets";
import { Film, Flag, Search as SearchIcon } from "lucide-react";
import { api, type Clip, type Facet, type Recording, type SearchParams } from "../api";
import { describeMatch, formatRange } from "../format";
import { SpriteScrub } from "../components/SpriteScrub";
import { SelectPicker } from "../components/SelectPicker";
import { eventColor, topMatch } from "../components/importance";
import { useActiveSelect } from "../components/useActiveSelect";

const route = getRouteApi("/");

type ListKey = "kind" | "map" | "tag" | "build" | "tester";

const FILTERS: { key: ListKey; facet: "kinds" | "maps" | "tags" | "builds" | "testers"; label: string }[] = [
  { key: "kind", facet: "kinds", label: "Event" },
  { key: "map", facet: "maps", label: "Map" },
  { key: "tag", facet: "tags", label: "Tag" },
  { key: "build", facet: "builds", label: "Build" },
  { key: "tester", facet: "testers", label: "Tester" },
];

const IMPORTANCE = [
  { value: "0", label: "Any importance" },
  { value: "1", label: "Importance 1+" },
  { value: "2", label: "Importance 2+" },
  { value: "3", label: "Importance 3" },
];

export function SearchPage() {
  const params = route.useSearch();
  const navigate = useNavigate({ from: "/" });
  const set = (patch: Partial<SearchParams>) =>
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true });

  const results = useQuery({
    queryKey: ["search", params],
    queryFn: () => api.search(params),
    placeholderData: (prev) => prev,
  });
  const facets = useQuery({ queryKey: ["facets"], queryFn: api.facets });
  const { addClip, active } = useActiveSelect();

  const chips: ActiveFilter[] = [];
  for (const f of FILTERS) {
    for (const v of params[f.key] ?? []) {
      chips.push({
        id: `${f.key}:${v}`,
        group: f.label,
        value: v,
        onRemove: () => set({ [f.key]: params[f.key]?.filter((x) => x !== v) }),
      });
    }
  }
  if (params.importance) {
    chips.push({
      id: "importance",
      group: "Importance",
      value: `${params.importance}+`,
      onRemove: () => set({ importance: undefined }),
    });
  }

  const clips = results.data?.clips ?? [];
  const recordings = results.data?.recordings ?? {};

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-col gap-2 border-b border-border-default p-3">
        <div className="flex items-center gap-2">
          <div className="min-w-0 flex-1">
            <SearchField
              value={params.q ?? ""}
              onChange={(q) => set({ q: q || undefined })}
              placeholder="Search events, labels and speech: “warden”, “slide under door”"
              busy={results.isFetching}
              resultCount={results.data ? clips.length : undefined}
            />
          </div>
          <SelectPicker />
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {FILTERS.map((f) => (
            <FacetSelect
              key={f.key}
              label={f.label}
              facets={facets.data?.[f.facet] ?? []}
              chosen={params[f.key] ?? []}
              onAdd={(v) => set({ [f.key]: [...(params[f.key] ?? []), v] })}
            />
          ))}
          <Select
            size="small"
            value={String(params.importance ?? 0)}
            onValueChange={(v) => set({ importance: Number(v) || undefined })}
            options={IMPORTANCE}
          />
          <div className="ml-auto flex items-center gap-2 text-xs text-text-muted">
            <CountBadge tone="dim">{formatCount(clips.length)}</CountBadge> clips
          </div>
        </div>
        <FilterChips
          filters={chips}
          onClearAll={() => set({ kind: undefined, map: undefined, tag: undefined, build: undefined, tester: undefined, importance: undefined })}
        />
      </div>
      <VirtualTileGrid
        items={clips}
        tileSize={260}
        getKey={(c, i) => `${c.recording_id}:${c.t_in}:${i}`}
        renderItem={(c) => (
          <ClipTile
            clip={c}
            recording={recordings[String(c.recording_id)]}
            activeName={active?.name}
            onAdd={() => addClip({ recording_id: c.recording_id, t_in: c.t_in, t_out: c.t_out, comment: "" })}
            onOpen={() =>
              void navigate({ to: "/recordings/$id", params: { id: String(c.recording_id) }, search: { in: c.t_in, out: c.t_out } })
            }
          />
        )}
        empty={
          results.isPending ? null : (
            <EmptyState
              icon={<SearchIcon className="h-5 w-5" />}
              title={params.q || chips.length ? "No moments match" : "Nothing indexed yet"}
              detail={
                params.q || chips.length
                  ? "Try fewer words or remove a filter."
                  : "Recordings appear here once the scanner finds them and their sidecars are read."
              }
            />
          )
        }
      />
    </div>
  );
}

function FacetSelect({
  label,
  facets,
  chosen,
  onAdd,
}: {
  label: string;
  facets: Facet[];
  chosen: string[];
  onAdd: (v: string) => void;
}) {
  const options = facets
    .filter((f) => !chosen.includes(f.value))
    .map((f) => ({ value: f.value, label: `${f.value} (${f.count})` }));
  return (
    <Select
      size="small"
      value=""
      placeholder={label}
      onValueChange={onAdd}
      options={options.length ? options : [{ value: "", label: "Nothing to add", disabled: true }]}
    />
  );
}

function ClipTile({
  clip,
  recording,
  activeName,
  onAdd,
  onOpen,
}: {
  clip: Clip;
  recording: Recording | undefined;
  activeName: string | undefined;
  onAdd: () => void;
  onOpen: () => void;
}) {
  const top = useMemo(() => topMatch(clip.matches), [clip.matches]);
  const moment = top?.t ?? clip.t_in;
  return (
    <Tile
      label={top ? describeMatch(top) : (recording?.name ?? "Clip")}
      title={clip.matches.map(describeMatch).join("\n")}
      badge={
        top?.source === "event" ? (
          top.type === "marker" ? (
            <Flag className="h-3.5 w-3.5" style={{ color: eventColor(top) }} />
          ) : (
            <span className="block h-2 w-2 rounded-full" style={{ background: eventColor(top) }} />
          )
        ) : null
      }
      fallback={<Film className="h-6 w-6 text-text-muted" />}
      thumbnailContent={
        <div className="flex h-full w-full flex-col">
          <div className="flex items-center gap-1 p-1.5">
            {top?.kind && top.type !== "marker" ? (
              <Badge size="xs" tone="dim">
                {top.kind}
              </Badge>
            ) : null}
            {clip.matches.length > 1 ? (
              <Badge size="xs" tone="dim">
                +{clip.matches.length - 1}
              </Badge>
            ) : null}
            <span className="ml-auto font-mono text-[10px] text-text-muted">{formatRange(clip.t_in, clip.t_out)}</span>
          </div>
          <div className="flex flex-1 items-center">
            <SpriteScrub sprite={recording?.sprite ?? null} tIn={clip.t_in} tOut={clip.t_out} poster={moment} />
          </div>
        </div>
      }
      details={
        <p className="truncate text-[11px] text-text-muted">
          {recording?.name}
          {recording?.session ? ` · ${recording.session.build} · ${recording.session.tester}` : ""}
        </p>
      }
      onClick={onOpen}
      menuItems={[
        { label: activeName ? `Add to ${activeName}` : "Add to a new select", onSelect: onAdd },
        { label: "Open recording", onSelect: onOpen },
      ]}
    />
  );
}
