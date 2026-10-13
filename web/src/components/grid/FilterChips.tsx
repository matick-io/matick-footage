import { Button } from "@matick-io/ui";
import { X } from "lucide-react";

export interface ActiveFilter {
  id: string;
  /** What the filter is, shown dimmed before the value: "Map: Forest 1-2". */
  group: string;
  value: string;
  onRemove: () => void;
}

/** The filters in effect, each removable; nothing when there are none. */
export function FilterChips({ filters, onClearAll }: { filters: ActiveFilter[]; onClearAll: () => void }) {
  if (!filters.length) return null;
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {filters.map((f) => (
        <span
          key={f.id}
          className="flex items-center gap-1 rounded-full border border-border-default bg-surface-subtle py-0.5 pl-2.5 pr-1 text-xs"
        >
          <span className="text-text-muted">{f.group}:</span>
          <span>{f.value}</span>
          <button
            type="button"
            aria-label={`Remove ${f.group} ${f.value}`}
            onClick={f.onRemove}
            className="rounded-full p-0.5 text-text-muted hover:bg-surface-elevated hover:text-text-primary"
          >
            <X className="h-3 w-3" />
          </button>
        </span>
      ))}
      {filters.length > 1 ? (
        <Button size="small" variant="ghost" onClick={onClearAll}>
          Clear all
        </Button>
      ) : null}
    </div>
  );
}
