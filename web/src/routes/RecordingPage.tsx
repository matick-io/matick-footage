import { useEffect, useMemo, useRef, useState } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Badge, Button, EmptyState, Kbd, ScrollArea, Tabs } from "@khaos-systems/ui-core";
import {
  ReviewMediaProvider,
  ReviewStage,
  ReviewVideo,
  Timeline,
  Transport,
  useReviewHotkeys,
  useReviewMedia,
  type TimelineTrack,
} from "@khaos-systems/ui-review";
import { Flag, ListPlus, Loader2, RotateCcw } from "lucide-react";
import { api, type Event, type Line, type Recording } from "../api";
import { formatClock, formatDate, formatDuration, formatRange } from "../format";
import { eventColor } from "../components/importance";
import { SelectPicker } from "../components/SelectPicker";
import { useActiveSelect } from "../components/useActiveSelect";

const route = getRouteApi("/recordings/$id");

export function RecordingPage() {
  const { id } = route.useParams();
  const rid = Number(id);
  const recording = useQuery({
    queryKey: ["recording", rid],
    queryFn: () => api.recording(rid),
    refetchInterval: (q) => (stagesPending(q.state.data) ? 3000 : false),
  });
  const events = useQuery({ queryKey: ["events", rid], queryFn: () => api.events(rid) });
  const transcript = useQuery({ queryKey: ["transcript", rid], queryFn: () => api.transcript(rid) });

  const rec = recording.data;
  if (recording.isPending) return null;
  if (!rec) {
    return <EmptyState title="Recording not found" detail={String(recording.error ?? "")} />;
  }
  if (!rec.proxy_url) {
    return (
      <div className="flex h-full flex-col">
        <Header rec={rec} />
        <EmptyState
          icon={<Loader2 className="h-5 w-5 animate-spin" />}
          title="Preparing a proxy"
          detail="The player streams a 720p proxy; this recording's is still being made."
        />
      </div>
    );
  }
  return (
    <ReviewMediaProvider fps={rec.fps ?? 30} loopRegion>
      <Player rec={rec} events={events.data ?? []} lines={transcript.data ?? []} />
    </ReviewMediaProvider>
  );
}

function stagesPending(rec: Recording | undefined) {
  return !!rec && Object.values(rec.stages).some((s) => s.status === "queued" || s.status === "running");
}

function Header({ rec, children }: { rec: Recording; children?: React.ReactNode }) {
  return (
    <div className="flex items-center gap-3 border-b border-border-default px-3 py-2">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium">{rec.name}</p>
        <p className="truncate text-xs text-text-muted">
          {formatDate(rec.recorded_at)}
          {rec.session ? ` · ${rec.session.build} · ${rec.session.tester} · ${rec.session.platform}` : " · no sidecar"}
          {rec.duration ? ` · ${formatClock(rec.duration)}` : ""}
        </p>
      </div>
      <div className="ml-auto flex items-center gap-2">{children}</div>
    </div>
  );
}

/** Seeks to the clip the page was opened for, once the video knows its length. */
function useInitialClip() {
  const { in: tIn, out: tOut } = route.useSearch();
  const media = useReviewMedia();
  const done = useRef(false);
  useEffect(() => {
    if (done.current || media.duration <= 0) return;
    done.current = true;
    if (tIn !== undefined) {
      media.setLoop({ in: tIn, out: tOut ?? null });
      media.seek(tIn);
    }
  }, [media, tIn, tOut]);
}

function Player({ rec, events, lines }: { rec: Recording; events: Event[]; lines: Line[] }) {
  const media = useReviewMedia();
  useReviewHotkeys();
  useInitialClip();
  const { addClip, active, adding } = useActiveSelect();
  const { in: loopIn, out: loopOut } = media.loop;
  const canAdd = loopIn !== null && loopOut !== null && loopOut > loopIn;

  const tracks = useMemo<TimelineTrack[]>(() => {
    const levels = events.filter((e) => e.kind === "level");
    const points = events.filter((e) => e.type !== "span" && e.type !== "marker");
    const markers = events.filter((e) => e.type === "marker");
    const spans = events.filter((e) => e.type === "span" && e.kind !== "level");
    const out: TimelineTrack[] = [];
    if (levels.length)
      out.push({
        id: "levels",
        label: "Levels",
        type: "segments",
        items: levels.map((e) => ({ id: String(e.id), in: e.t, out: e.t + (e.dur ?? 0), label: e.label, color: "var(--color-text-muted)" })),
      });
    if (points.length)
      out.push({
        id: "events",
        label: "Events",
        type: "markers",
        items: points.map((e) => ({ id: String(e.id), time: e.t, label: e.label || e.kind, color: eventColor(e) })),
      });
    if (spans.length)
      out.push({
        id: "spans",
        label: "Spans",
        type: "segments",
        items: spans.map((e) => ({ id: String(e.id), in: e.t, out: e.t + (e.dur ?? 0), label: e.label || e.kind, color: eventColor(e) })),
      });
    if (markers.length)
      out.push({
        id: "markers",
        label: "Markers",
        type: "markers",
        items: markers.map((e) => ({ id: String(e.id), time: e.t, label: e.tags?.join(", ") || "marker", color: eventColor(e) })),
      });
    if (lines.length)
      out.push({
        id: "speech",
        label: "Speech",
        type: "segments",
        items: lines.map((l, i) => ({ id: String(i), in: l.t_in, out: l.t_out, label: l.text, color: "var(--color-status-info)" })),
      });
    if (canAdd)
      out.push({
        id: "clip",
        label: "Clip",
        type: "segments",
        items: [{ id: "clip", in: loopIn, out: loopOut, label: formatRange(loopIn, loopOut), color: "var(--color-status-success)" }],
      });
    return out;
  }, [events, lines, canAdd, loopIn, loopOut]);

  const seekTo = (trackId: string, itemId: string) => {
    if (trackId === "speech") {
      const l = lines[Number(itemId)];
      if (l) media.seek(l.t_in);
      return;
    }
    const e = events.find((x) => String(x.id) === itemId);
    if (e) media.seek(e.t);
  };

  return (
    <div className="flex h-full flex-col">
      <Header rec={rec}>
        <SelectPicker />
        <Button
          size="small"
          variant="accent"
          disabled={!canAdd || adding}
          tooltip={canAdd ? `Add ${formatRange(loopIn, loopOut)}` : "Mark in and out first (I and O)"}
          onClick={() => {
            if (canAdd) addClip({ recording_id: rec.id, t_in: loopIn, t_out: loopOut, comment: "" });
          }}
        >
          <ListPlus className="h-4 w-4" />
          {active ? `Add to ${active.name}` : "Add to select"}
        </Button>
      </Header>
      <div className="flex min-h-0 flex-1">
        <div className="flex min-w-0 flex-1 flex-col gap-2 p-3">
          <ReviewStage aspectRatio={(rec.width ?? 16) / (rec.height ?? 9)} className="rounded">
            <ReviewVideo src={rec.proxy_url ?? ""} />
          </ReviewStage>
          <Timeline
            tracks={tracks}
            onItemClick={seekTo}
            onSegmentResize={(trackId, _item, tIn, tOut) => {
              if (trackId === "clip") media.setLoop({ in: tIn, out: tOut });
            }}
          />
          <Transport showRate showVolume>
            <span className="hidden items-center gap-1 text-[11px] text-text-muted lg:flex">
              <Kbd>I</Kbd>/<Kbd>O</Kbd> in/out · <Kbd>J</Kbd>
              <Kbd>K</Kbd>
              <Kbd>L</Kbd> shuttle
            </span>
          </Transport>
        </div>
        <aside className="flex w-80 shrink-0 flex-col border-l border-border-default">
          <SidePanel rec={rec} events={events} lines={lines} />
        </aside>
      </div>
    </div>
  );
}

function SidePanel({ rec, events, lines }: { rec: Recording; events: Event[]; lines: Line[] }) {
  const [tab, setTab] = useState(lines.length && !events.length ? "speech" : "events");
  return (
    <Tabs value={tab} onValueChange={(v) => setTab(String(v))} className="flex min-h-0 flex-1 flex-col">
      <Tabs.List className="shrink-0 px-2 pt-2">
        <Tabs.Tab value="events">Events ({events.length})</Tabs.Tab>
        <Tabs.Tab value="speech">Speech ({lines.length})</Tabs.Tab>
        <Tabs.Tab value="info">Info</Tabs.Tab>
      </Tabs.List>
      <Tabs.Panel value="events" className="min-h-0 flex-1">
        <EventList events={events} />
      </Tabs.Panel>
      <Tabs.Panel value="speech" className="min-h-0 flex-1">
        <TranscriptList lines={lines} />
      </Tabs.Panel>
      <Tabs.Panel value="info" className="min-h-0 flex-1">
        <Info rec={rec} />
      </Tabs.Panel>
    </Tabs>
  );
}

function EventList({ events }: { events: Event[] }) {
  const media = useReviewMedia();
  if (!events.length) return <EmptyState compact title="No events" detail="This recording has no sidecar." />;
  return (
    <ScrollArea className="h-full">
      <ul className="flex flex-col p-1">
        {events.map((e) => (
          <li key={e.id}>
            <button
              type="button"
              onClick={() => media.seek(e.t)}
              className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-surface-subtle"
            >
              <span className="w-12 shrink-0 font-mono text-text-muted">{formatClock(e.t)}</span>
              {e.type === "marker" ? (
                <Flag className="h-3 w-3 shrink-0" style={{ color: eventColor(e) }} />
              ) : (
                <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: eventColor(e) }} />
              )}
              <span className="min-w-0 flex-1 truncate">{e.label || e.tags?.join(", ") || e.kind}</span>
              <span className="shrink-0 text-[10px] text-text-muted">{e.type === "marker" ? "marker" : e.kind}</span>
            </button>
          </li>
        ))}
      </ul>
    </ScrollArea>
  );
}

/** The transcript follows playback: the spoken line is highlighted and kept in view. */
function TranscriptList({ lines }: { lines: Line[] }) {
  const media = useReviewMedia();
  const now = media.currentTime;
  const current = lines.findIndex((l) => now >= l.t_in && now < l.t_out);
  const refs = useRef<(HTMLLIElement | null)[]>([]);
  useEffect(() => {
    if (current >= 0) refs.current[current]?.scrollIntoView({ block: "nearest" });
  }, [current]);
  if (!lines.length) return <EmptyState compact title="No speech" detail="Nothing was transcribed for this recording." />;
  return (
    <ScrollArea className="h-full">
      <ul className="flex flex-col p-1">
        {lines.map((l, i) => (
          <li key={i} ref={(el) => void (refs.current[i] = el)}>
            <button
              type="button"
              onClick={() => media.seek(l.t_in)}
              className={`flex w-full gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-surface-subtle ${i === current ? "bg-surface-subtle text-text-primary" : "text-text-secondary"}`}
            >
              <span className="w-12 shrink-0 font-mono text-text-muted">{formatClock(l.t_in)}</span>
              <span className="min-w-0 flex-1">{l.text}</span>
            </button>
          </li>
        ))}
      </ul>
    </ScrollArea>
  );
}

function Info({ rec }: { rec: Recording }) {
  const qc = useQueryClient();
  const rerun = useMutation({
    mutationFn: (stage: string) => api.reprocess(rec.id, stage),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["recording", rec.id] }),
  });
  const rows: [string, string][] = [
    ["File", rec.path],
    ["Duration", rec.duration ? formatDuration(rec.duration) : "—"],
    ["Video", rec.width ? `${rec.width}×${rec.height} @ ${rec.fps?.toFixed(2)} fps` : "—"],
    ["Audio tracks", String(rec.audio_tracks ?? "—")],
    ["Sync", rec.sync_mode || "—"],
  ];
  if (rec.session) {
    rows.push(
      ["Project", rec.session.project],
      ["Session", rec.session.session_id],
      ["Build", rec.session.build],
      ["Platform", rec.session.platform],
      ["Tester", rec.session.tester],
    );
  }
  return (
    <ScrollArea className="h-full">
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5 p-3 text-xs">
        {rows.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-text-muted">{k}</dt>
            <dd className="min-w-0 break-words">{v}</dd>
          </div>
        ))}
      </dl>
      <p className="px-3 pb-1 pt-2 text-xs font-medium">Stages</p>
      <ul className="flex flex-col gap-1 px-3 pb-3 text-xs">
        {Object.entries(rec.stages).map(([name, s]) => (
          <li key={name} className="flex items-center gap-2">
            <span className="w-20">{name}</span>
            <Badge size="xs" tone={s.status === "done" ? "success" : s.status === "failed" ? "danger" : "info"}>
              {s.status}
            </Badge>
            {s.model ? <span className="text-text-muted">{s.model}</span> : null}
            {s.status === "done" || s.status === "failed" ? (
              <Button
                className="ml-auto"
                size="small"
                variant="ghost"
                kind="icon"
                tooltip={`Re-run ${name}`}
                onClick={() => rerun.mutate(name)}
              >
                <RotateCcw className="h-3.5 w-3.5" />
              </Button>
            ) : null}
          </li>
        ))}
      </ul>
    </ScrollArea>
  );
}
