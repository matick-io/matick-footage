import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";

const GAP = 12;
const PAD = 12;
/** Room under the 16:9 picture for a card's two caption lines. */
const CAPTION = 48;

interface VideoGridProps<T> {
  items: T[];
  /** Smallest card width; columns grow to fill the row. */
  minWidth?: number;
  getKey: (item: T, index: number) => string;
  renderItem: (item: T, index: number) => ReactNode;
  empty?: ReactNode;
}

/** A virtualized grid of 16:9 cards. It is its own scroll container, so give it
 * a parent with a bounded height. */
export function VideoGrid<T>({ items, minWidth = 260, getKey, renderItem, empty }: VideoGridProps<T>) {
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const [width, setWidth] = useState(0);

  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const observer = new ResizeObserver(([entry]) => setWidth(entry?.contentRect.width ?? 0));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const inner = Math.max(0, width - PAD * 2);
  const columns = Math.max(1, Math.floor((inner + GAP) / (minWidth + GAP)));
  const cardWidth = (inner - GAP * (columns - 1)) / columns;
  const rowHeight = (cardWidth * 9) / 16 + CAPTION;
  const rows = Math.ceil(items.length / columns);

  const virtualizer = useVirtualizer({
    count: rows,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeight + GAP,
    overscan: 3,
  });
  useLayoutEffect(() => virtualizer.measure(), [virtualizer, rowHeight]);

  return (
    <div ref={scrollRef} className="min-h-0 flex-1 overflow-auto" style={{ padding: PAD }}>
      {items.length === 0 ? (
        empty
      ) : (
        <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
          {virtualizer.getVirtualItems().map((row) => (
            <div
              key={row.key}
              className="absolute inset-x-0 grid"
              style={{
                top: row.start,
                height: rowHeight,
                gap: GAP,
                gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))`,
              }}
            >
              {items.slice(row.index * columns, row.index * columns + columns).map((item, i) => {
                const index = row.index * columns + i;
                return <div key={getKey(item, index)}>{renderItem(item, index)}</div>;
              })}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
