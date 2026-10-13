import type { ReactNode } from "react";
import { Button, ContextMenu, type MenuItemData } from "@matick-io/ui";
import { ListPlus } from "lucide-react";

interface ClipCardProps {
  /** The 16:9 picture, e.g. a SpriteScrub. */
  picture: ReactNode;
  title: string;
  subtitle?: ReactNode;
  /** Leading mark before the title, e.g. an importance dot. */
  mark?: ReactNode;
  /** Top-left over the picture. */
  badges?: ReactNode;
  /** Top-right over the picture, e.g. a time range. */
  corner?: ReactNode;
  /** Native tooltip with the full text. */
  tooltip?: string;
  onOpen: () => void;
  /** Shows an add button on hover. */
  onAdd?: () => void;
  addLabel?: string;
  menuItems?: MenuItemData[];
}

/** A clip in a grid: picture, title and an add action, with the same actions on
 * right-click. */
export function ClipCard({
  picture,
  title,
  subtitle,
  mark,
  badges,
  corner,
  tooltip,
  onOpen,
  onAdd,
  addLabel = "Add to select",
  menuItems,
}: ClipCardProps) {
  const card = (
    <div
      role="button"
      tabIndex={0}
      title={tooltip}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
      className="group flex h-full cursor-pointer flex-col gap-1.5 rounded-md outline-none focus-visible:ring-2 focus-visible:ring-input-ring-focus"
    >
      <div className="relative overflow-hidden rounded-md border border-border-default bg-black transition group-hover:border-border-subtle">
        {picture}
        <div className="pointer-events-none absolute inset-x-0 top-0 flex items-start gap-1 p-1.5">
          <div className="flex min-w-0 flex-wrap gap-1">{badges}</div>
          <div className="ml-auto rounded bg-black/60 px-1 font-mono text-[10px] text-white/80">{corner}</div>
        </div>
        {onAdd ? (
          <Button
            size="small"
            variant="panel"
            kind="icon"
            aria-label={addLabel}
            tooltip={addLabel}
            className="absolute bottom-1.5 right-1.5 opacity-0 transition group-hover:opacity-100 group-focus-within:opacity-100"
            onClick={(e) => {
              e.stopPropagation();
              onAdd();
            }}
          >
            <ListPlus className="h-4 w-4" />
          </Button>
        ) : null}
      </div>
      <div className="min-w-0 px-0.5">
        <p className="flex items-center gap-1.5 truncate text-sm font-medium">
          {mark}
          <span className="truncate">{title}</span>
        </p>
        {subtitle ? <p className="truncate text-[11px] text-text-muted">{subtitle}</p> : null}
      </div>
    </div>
  );
  return menuItems?.length ? (
    <ContextMenu items={menuItems} triggerClassName="contents">
      {card}
    </ContextMenu>
  ) : (
    card
  );
}
