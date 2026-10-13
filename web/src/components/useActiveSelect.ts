import { useCallback, useSyncExternalStore } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useToast } from "@matick-io/ui";
import { api, type SelectItem } from "../api";
import { formatRange } from "../format";

// The select that "Add to select" fills, remembered per browser.
const KEY = "matick-footage.active-select";
const listeners = new Set<() => void>();

function read(): number | null {
  try {
    const v = Number(localStorage.getItem(KEY));
    return Number.isInteger(v) && v > 0 ? v : null;
  } catch {
    return null;
  }
}

export function setActiveSelect(id: number | null) {
  try {
    if (id === null) localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, String(id));
  } catch {
    // storage unavailable: the choice just won't persist
  }
  for (const l of listeners) l();
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function useActiveSelect() {
  const id = useSyncExternalStore(subscribe, read, () => null);
  const selects = useQuery({ queryKey: ["selects"], queryFn: api.selects });
  const active = selects.data?.find((s) => s.id === id) ?? null;
  const qc = useQueryClient();
  const toast = useToast();

  const add = useMutation({
    mutationFn: async (item: SelectItem) => {
      let target = active;
      if (!target) {
        target = await api.createSelect("Untitled select");
        setActiveSelect(target.id);
      }
      return api.addToSelect(target.id, item);
    },
    onSuccess: (sel, item) => {
      void qc.invalidateQueries({ queryKey: ["selects"] });
      qc.setQueryData(["select", sel.id], sel);
      toast?.toast(`Added ${formatRange(item.t_in, item.t_out)} to ${sel.name}`, { variant: "success" });
    },
    onError: (err) => toast?.toast("Could not add the clip", { variant: "error", description: String(err) }),
  });

  const addClip = useCallback((item: SelectItem) => add.mutate(item), [add]);
  return { active, selects: selects.data ?? [], addClip, adding: add.isPending };
}
