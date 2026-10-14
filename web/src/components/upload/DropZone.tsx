import { useEffect, useRef, useState, type ReactNode } from "react";
import { Upload } from "lucide-react";

interface DropZoneProps {
  onFiles: (files: File[]) => void;
  title: string;
  detail?: ReactNode;
}

const carriesFiles = (e: DragEvent) => e.dataTransfer?.types.includes("Files") ?? false;

/** Takes files dropped anywhere on the page, with an overlay while they're
 * dragged over the window. Render it once, near the root. */
export function DropZone({ onFiles, title, detail }: DropZoneProps) {
  const [over, setOver] = useState(false);
  // dragenter and dragleave fire for every element crossed; count to know when
  // the drag has really left the window.
  const depth = useRef(0);

  useEffect(() => {
    const enter = (e: DragEvent) => {
      if (!carriesFiles(e)) return;
      e.preventDefault();
      depth.current += 1;
      setOver(true);
    };
    const leave = (e: DragEvent) => {
      if (!carriesFiles(e)) return;
      depth.current = Math.max(0, depth.current - 1);
      if (depth.current === 0) setOver(false);
    };
    const dragOver = (e: DragEvent) => {
      if (!carriesFiles(e)) return;
      e.preventDefault();
      if (e.dataTransfer) e.dataTransfer.dropEffect = "copy";
    };
    const drop = (e: DragEvent) => {
      if (!carriesFiles(e)) return;
      e.preventDefault();
      depth.current = 0;
      setOver(false);
      const files = Array.from(e.dataTransfer?.files ?? []);
      if (files.length) onFiles(files);
    };
    window.addEventListener("dragenter", enter);
    window.addEventListener("dragleave", leave);
    window.addEventListener("dragover", dragOver);
    window.addEventListener("drop", drop);
    return () => {
      window.removeEventListener("dragenter", enter);
      window.removeEventListener("dragleave", leave);
      window.removeEventListener("dragover", dragOver);
      window.removeEventListener("drop", drop);
    };
  }, [onFiles]);

  if (!over) return null;
  return (
    <div className="pointer-events-none fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-6 backdrop-blur-[2px]">
      <div className="flex max-w-md flex-col items-center gap-2 rounded-lg border-2 border-dashed border-input-ring-focus bg-surface-overlay px-10 py-8 text-center">
        <Upload className="h-6 w-6 text-text-muted" />
        <p className="text-sm font-medium">{title}</p>
        {detail ? <p className="text-xs text-text-muted">{detail}</p> : null}
      </div>
    </div>
  );
}
