import { useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Badge, EmptyState } from "@matick-io/ui";
import { Film, Loader2 } from "lucide-react";
import { api, type Recording } from "../api";
import { formatClock, formatDate } from "../format";
import { SpriteScrub } from "../components/SpriteScrub";
import { ClipCard } from "../components/grid/ClipCard";
import { VideoGrid } from "../components/grid/VideoGrid";

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
      <VideoGrid
        items={recordings}
        getKey={(r) => String(r.id)}
        renderItem={(r) => {
          const busy = pending(r);
          const failed = Object.entries(r.stages)
            .filter(([, s]) => s.status === "failed")
            .map(([n]) => n);
          const dur = r.duration ?? 0;
          return (
            <ClipCard
              picture={
                r.sprite ? (
                  <SpriteScrub sprite={r.sprite} tIn={0} tOut={dur} poster={dur * 0.1} />
                ) : (
                  <div className="flex aspect-video items-center justify-center text-text-muted">
                    {busy ? <Loader2 className="h-6 w-6 animate-spin" /> : <Film className="h-6 w-6" />}
                  </div>
                )
              }
              title={r.name}
              tooltip={r.path}
              subtitle={`${formatDate(r.recorded_at)}${r.session ? ` · ${r.session.tester}` : ""}`}
              badges={
                <>
                  <Badge size="xs" tone="dim">
                    {r.session ? r.session.build : "no sidecar"}
                  </Badge>
                  {failed.length ? (
                    <Badge size="xs" tone="danger">
                      {failed.join(", ")} failed
                    </Badge>
                  ) : null}
                  {busy ? (
                    <Badge size="xs" tone="info">
                      {busy}
                    </Badge>
                  ) : null}
                </>
              }
              corner={formatClock(dur)}
              onOpen={() => void navigate({ to: "/recordings/$id", params: { id: String(r.id) } })}
            />
          );
        }}
        empty={
          list.isPending ? null : (
            <EmptyState
              icon={<Film className="h-5 w-5" />}
              title="No recordings yet"
              detail="Drop video files here to upload them, each with its .events.jsonl sidecar if it has one, or record into a watched folder."
            />
          )
        }
      />
    </div>
  );
}
