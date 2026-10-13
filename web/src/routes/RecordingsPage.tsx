import { useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Badge, EmptyState } from "@khaos-systems/ui-core";
import { Tile, VirtualTileGrid } from "@khaos-systems/ui-assets";
import { Film, Loader2 } from "lucide-react";
import { api, type Recording } from "../api";
import { formatClock, formatDate } from "../format";
import { SpriteScrub } from "../components/SpriteScrub";

function pending(rec: Recording): string | null {
  const busy = Object.entries(rec.stages).find(([, s]) => s.status === "queued" || s.status === "running");
  return busy ? `${busy[0]}…` : null;
}

export function RecordingsPage() {
  const navigate = useNavigate();
  const list = useQuery({
    queryKey: ["recordings"],
    queryFn: api.recordings,
    refetchInterval: (q) => (q.state.data?.recordings.some((r) => pending(r)) ? 3000 : 15000),
  });
  const recordings = list.data?.recordings ?? [];
  return (
    <div className="flex h-full flex-col">
      <VirtualTileGrid
        items={recordings}
        tileSize={260}
        getKey={(r) => String(r.id)}
        renderItem={(r) => {
          const busy = pending(r);
          const failed = Object.entries(r.stages).filter(([, s]) => s.status === "failed").map(([n]) => n);
          const dur = r.duration ?? 0;
          return (
            <Tile
              label={r.name}
              title={r.path}
              fallback={busy ? <Loader2 className="h-6 w-6 animate-spin text-text-muted" /> : <Film className="h-6 w-6 text-text-muted" />}
              thumbnailContent={
                r.sprite ? (
                  <div className="flex h-full w-full flex-col">
                    <div className="flex items-center gap-1 p-1.5">
                      {r.session ? <Badge size="xs" tone="dim">{r.session.build}</Badge> : <Badge size="xs" tone="dim">no sidecar</Badge>}
                      {failed.length ? <Badge size="xs" tone="danger">{failed.join(", ")} failed</Badge> : null}
                      {busy ? <Badge size="xs" tone="info">{busy}</Badge> : null}
                      <span className="ml-auto font-mono text-[10px] text-text-muted">{formatClock(dur)}</span>
                    </div>
                    <div className="flex flex-1 items-center">
                      <SpriteScrub sprite={r.sprite} tIn={0} tOut={dur} poster={dur * 0.1} />
                    </div>
                  </div>
                ) : undefined
              }
              details={<p className="truncate text-[11px] text-text-muted">{formatDate(r.recorded_at)}{r.session ? ` · ${r.session.tester}` : ""}</p>}
              busy={!r.sprite && busy ? { label: busy } : undefined}
              onClick={() => void navigate({ to: "/recordings/$id", params: { id: String(r.id) } })}
            />
          );
        }}
        empty={
          list.isPending ? null : (
            <EmptyState
              icon={<Film className="h-5 w-5" />}
              title="No recordings yet"
              detail="Put video files (with optional .events.jsonl sidecars) in the footage folder; the scanner picks them up."
            />
          )
        }
      />
    </div>
  );
}
