import { useState } from "react";
import { getRouteApi, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, EmptyState, Input, Menu, ScrollArea, Select, Textarea, Tree, type TreeItem } from "@khaos-systems/ui-core";
import { CountBadge } from "@khaos-systems/ui-assets";
import { Download, ListVideo, MoreHorizontal, Play, Plus, Trash2 } from "lucide-react";
import { api, type SelectDetail, type SelectItem } from "../api";
import { formatDuration, formatRange } from "../format";
import { spriteStyle } from "../components/SpriteScrub";
import { setActiveSelect, useActiveSelect } from "../components/useActiveSelect";

const route = getRouteApi("/selects");

export function SelectsPage() {
  const { id } = route.useSearch();
  const navigate = useNavigate({ from: "/selects" });
  const { selects, active } = useActiveSelect();
  const qc = useQueryClient();
  const shown = id ?? active?.id ?? selects[0]?.id;

  const create = useMutation({
    mutationFn: () => api.createSelect("Untitled select"),
    onSuccess: (sel) => {
      void qc.invalidateQueries({ queryKey: ["selects"] });
      setActiveSelect(sel.id);
      void navigate({ search: { id: sel.id } });
    },
  });

  return (
    <div className="flex h-full">
      <aside className="flex w-64 shrink-0 flex-col border-r border-border-default">
        <div className="flex items-center justify-between p-2">
          <span className="text-xs font-medium text-text-muted">Selects</span>
          <Button size="small" variant="ghost" kind="icon" tooltip="New select" onClick={() => create.mutate()}>
            <Plus className="h-4 w-4" />
          </Button>
        </div>
        <ScrollArea className="min-h-0 flex-1">
          <ul className="flex flex-col gap-0.5 p-1">
            {selects.map((s) => (
              <li key={s.id}>
                <Button
                  variant="panel"
                  selected={s.id === shown}
                  className="w-full justify-start gap-2"
                  onClick={() => void navigate({ search: { id: s.id } })}
                >
                  <ListVideo className="h-4 w-4 shrink-0" />
                  <span className="min-w-0 flex-1 truncate text-left">{s.name}</span>
                  <CountBadge tone="dim">{s.item_count}</CountBadge>
                </Button>
              </li>
            ))}
          </ul>
        </ScrollArea>
      </aside>
      <div className="min-w-0 flex-1">
        {shown ? (
          <SelectView key={shown} id={shown} />
        ) : (
          <EmptyState
            icon={<ListVideo className="h-5 w-5" />}
            title="No selects yet"
            detail="A select is a basket of clips that exports as a timeline. Add clips from search or the player."
            action={<Button variant="accent" onClick={() => create.mutate()}>New select</Button>}
          />
        )}
      </div>
    </div>
  );
}

function SelectView({ id }: { id: number }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const sel = useQuery({ queryKey: ["select", id], queryFn: () => api.select(id) });
  const config = useQuery({ queryKey: ["config"], queryFn: api.config });
  const [target, setTarget] = useState("");
  const [picked, setPicked] = useState<string | undefined>();
  const { active } = useActiveSelect();

  const saved = (next: SelectDetail) => {
    qc.setQueryData(["select", id], next);
    void qc.invalidateQueries({ queryKey: ["selects"] });
  };
  const setItems = useMutation({
    mutationFn: (items: SelectItem[]) => api.setSelectItems(id, items),
    onMutate: (items) => {
      const prev = qc.getQueryData<SelectDetail>(["select", id]);
      if (prev) qc.setQueryData(["select", id], { ...prev, items });
    },
    onSuccess: saved,
  });
  const update = useMutation({
    mutationFn: (patch: { name?: string; notes?: string }) => api.updateSelect(id, patch),
    onSuccess: saved,
  });
  const remove = useMutation({
    mutationFn: () => api.deleteSelect(id),
    onSuccess: () => {
      if (active?.id === id) setActiveSelect(null);
      void qc.invalidateQueries({ queryKey: ["selects"] });
      void navigate({ to: "/selects", search: {} });
    },
  });

  const data = sel.data;
  if (!data) return null;
  const items = data.items;
  const keyOf = (it: SelectItem, i: number) => String(it.id ?? `new-${i}`);
  const tree: TreeItem<SelectItem>[] = items.map((it, i) => ({ id: keyOf(it, i), label: "", data: it }));
  const pickedIndex = items.findIndex((it, i) => keyOf(it, i) === picked);
  const pickedItem = pickedIndex >= 0 ? items[pickedIndex] : undefined;
  const targets = config.data?.export_targets ?? [];
  const total = items.reduce((sum, it) => sum + (it.t_out - it.t_in), 0);

  const patchItem = (i: number, patch: Partial<SelectItem>) =>
    setItems.mutate(items.map((it, j) => (j === i ? { ...it, ...patch } : it)));

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-2 border-b border-border-default p-3">
        <Input
          key={data.name}
          defaultValue={data.name}
          className="max-w-sm"
          aria-label="Select name"
          onBlur={(e) => {
            const name = e.target.value.trim();
            if (name && name !== data.name) update.mutate({ name });
          }}
        />
        <span className="text-xs text-text-muted">
          {items.length} clips · {formatDuration(total)}
        </span>
        <div className="ml-auto flex items-center gap-2">
          {active?.id !== id ? (
            <Button size="small" variant="ghost" onClick={() => setActiveSelect(id)}>
              Add new clips here
            </Button>
          ) : null}
          <Select
            size="small"
            value={target}
            onValueChange={setTarget}
            options={[{ value: "", label: "Server paths" }, ...targets.map((t) => ({ value: t.name, label: t.name }))]}
          />
          <Button
            size="small"
            variant="accent"
            disabled={!items.length}
            onClick={() => {
              window.location.href = api.exportUrl(id, target || undefined);
            }}
          >
            <Download className="h-4 w-4" /> Export .otio
          </Button>
          <Menu
            trigger={
              <Button size="small" variant="ghost" kind="icon" aria-label="More">
                <MoreHorizontal className="h-4 w-4" />
              </Button>
            }
            items={[{ label: "Delete select", icon: <Trash2 className="h-4 w-4" />, tone: "destructive", onSelect: () => remove.mutate() }]}
          />
        </div>
      </div>
      <div className="flex min-h-0 flex-1">
        <div className="flex min-w-0 flex-1 flex-col">
          {items.length ? (
            <ScrollArea className="min-h-0 flex-1">
              <Tree
                className="p-2"
                items={tree}
                selectedId={picked}
                onSelectedIdChange={(sid) => setPicked(sid)}
                onReorder={(_parent, ids) => {
                  const byKey = new Map(items.map((it, i) => [keyOf(it, i), it]));
                  setItems.mutate(ids.flatMap((k) => byKey.get(k) ?? []));
                }}
                renderItem={(node) => {
                  const it = node.data;
                  if (!it) return null;
                  const rec = data.recordings[String(it.recording_id)];
                  return (
                    <span className="flex min-w-0 flex-1 items-center gap-3">
                      <span className="block aspect-video w-28 shrink-0 overflow-hidden rounded bg-black">
                        {rec?.sprite ? <span className="block h-full w-full" style={spriteStyle(rec.sprite, it.t_in)} /> : null}
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm">{rec?.name ?? `Recording ${it.recording_id}`}</span>
                        <span className="block truncate text-xs text-text-muted">
                          {formatRange(it.t_in, it.t_out)} · {formatDuration(it.t_out - it.t_in)}
                          {it.comment ? ` · ${it.comment}` : ""}
                        </span>
                      </span>
                    </span>
                  );
                }}
              />
            </ScrollArea>
          ) : (
            <EmptyState
              icon={<ListVideo className="h-5 w-5" />}
              title="Empty select"
              detail="Right-click a search result, or mark in and out in the player, to add clips."
            />
          )}
          <div className="border-t border-border-default p-3">
            <Textarea
              key={data.notes}
              defaultValue={data.notes}
              placeholder="Notes for the editor"
              rows={2}
              onBlur={(e) => {
                if (e.target.value !== data.notes) update.mutate({ notes: e.target.value });
              }}
            />
          </div>
        </div>
        {pickedItem ? (
          <aside className="flex w-72 shrink-0 flex-col gap-3 border-l border-border-default p-3 text-xs">
            <ItemEditor
              key={keyOf(pickedItem, pickedIndex)}
              item={pickedItem}
              onChange={(patch) => patchItem(pickedIndex, patch)}
              onRemove={() => {
                setItems.mutate(items.filter((_, j) => j !== pickedIndex));
                setPicked(undefined);
              }}
              onOpen={() =>
                void navigate({
                  to: "/recordings/$id",
                  params: { id: String(pickedItem.recording_id) },
                  search: { in: pickedItem.t_in, out: pickedItem.t_out },
                })
              }
            />
          </aside>
        ) : null}
      </div>
    </div>
  );
}

function ItemEditor({
  item,
  onChange,
  onRemove,
  onOpen,
}: {
  item: SelectItem;
  onChange: (patch: Partial<SelectItem>) => void;
  onRemove: () => void;
  onOpen: () => void;
}) {
  const [tIn, setIn] = useState(String(item.t_in));
  const [tOut, setOut] = useState(String(item.t_out));
  const commit = () => {
    const a = Number(tIn);
    const b = Number(tOut);
    if (Number.isFinite(a) && Number.isFinite(b) && a >= 0 && b > a && (a !== item.t_in || b !== item.t_out)) {
      onChange({ t_in: a, t_out: b });
    }
  };
  return (
    <>
      <p className="font-medium">Clip</p>
      <label className="flex flex-col gap-1">
        <span className="text-text-muted">In (seconds)</span>
        <Input type="number" step="0.1" min="0" value={tIn} onChange={(e) => setIn(e.target.value)} onBlur={commit} />
      </label>
      <label className="flex flex-col gap-1">
        <span className="text-text-muted">Out (seconds)</span>
        <Input type="number" step="0.1" min="0" value={tOut} onChange={(e) => setOut(e.target.value)} onBlur={commit} />
      </label>
      <label className="flex flex-col gap-1">
        <span className="text-text-muted">Comment</span>
        <Input
          defaultValue={item.comment}
          onBlur={(e) => {
            if (e.target.value !== item.comment) onChange({ comment: e.target.value });
          }}
        />
      </label>
      <div className="flex gap-2">
        <Button size="small" onClick={onOpen}>
          <Play className="h-3.5 w-3.5" /> Open in player
        </Button>
        <Button size="small" variant="danger" onClick={onRemove}>
          <Trash2 className="h-3.5 w-3.5" /> Remove
        </Button>
      </div>
    </>
  );
}
