import { useRef } from "react";
import { Button } from "@matick-io/ui";
import { Upload } from "lucide-react";

/** Opens the file picker; the drop zone's counterpart for people who'd rather click. */
export function UploadButton({ onFiles }: { onFiles: (files: File[]) => void }) {
  const input = useRef<HTMLInputElement>(null);
  return (
    <>
      <input
        ref={input}
        type="file"
        multiple
        accept="video/*,.mkv,.mxf,.jsonl"
        className="hidden"
        onChange={(e) => {
          const files = Array.from(e.target.files ?? []);
          e.target.value = "";
          if (files.length) onFiles(files);
        }}
      />
      <Button size="small" variant="ghost" tooltip="Upload recordings" onClick={() => input.current?.click()}>
        <Upload className="h-4 w-4" />
        Upload
      </Button>
    </>
  );
}
