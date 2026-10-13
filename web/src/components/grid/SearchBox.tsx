import { useEffect, useRef, useState } from "react";
import { Kbd, Search } from "@matick-io/ui";

interface SearchBoxProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  debounceMs?: number;
}

/** A search input that commits after typing pauses. `/` focuses it from
 * anywhere outside a text field; Escape clears it. */
export function SearchBox({ value, onChange, placeholder, debounceMs = 250 }: SearchBoxProps) {
  const [draft, setDraft] = useState(value);
  const [seen, setSeen] = useState(value);
  const [sent, setSent] = useState(value);
  const box = useRef<HTMLDivElement | null>(null);
  // Follow the value only when it changes from outside (back button, a link),
  // not when it catches up with what this box sent, or typing would be lost.
  if (value !== seen) {
    setSeen(value);
    if (value !== sent) setDraft(value);
  }

  useEffect(() => {
    if (draft === value) return;
    const t = setTimeout(() => {
      setSent(draft);
      onChange(draft);
    }, debounceMs);
    return () => clearTimeout(t);
  }, [draft, value, onChange, debounceMs]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null;
      const typing = target?.closest("input, textarea, select, [contenteditable='true']");
      if (e.key === "/" && !typing) {
        e.preventDefault();
        box.current?.querySelector("input")?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div ref={box} className="relative">
      <Search
        className="w-full!"
        value={draft}
        placeholder={placeholder}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            setDraft("");
            setSent("");
            onChange("");
          }
        }}
      />
      {draft ? null : (
        <Kbd className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2">/</Kbd>
      )}
    </div>
  );
}
