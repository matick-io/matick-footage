import { useCallback } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { formatBytes, useToast } from "@matick-io/ui";
import { api } from "../../api";

const VIDEO = /\.(mp4|mov|mkv|m4v|webm|avi|mxf)$/i;
const SIDECAR = ".events.jsonl";

const stem = (name: string) => name.replace(/\.[^.]+$/, "");
const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;
const message = (err: unknown) => (err instanceof Error ? err.message : String(err));

/** Uploads dropped or picked files into the library, one at a time, with one
 * progress toast for the batch. A video's sidecar (same name, .events.jsonl)
 * follows it when both are in the batch. */
export function useUploads() {
  const toaster = useToast();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const config = useQuery({ queryKey: ["config"], queryFn: api.config });
  const max = config.data?.upload_max;

  return useCallback(
    async (files: File[]) => {
      const toast = (msg: string, opts?: Parameters<NonNullable<typeof toaster>["toast"]>[1]) =>
        toaster?.toast(msg, opts);
      const sidecars = new Map(
        files.filter((f) => f.name.endsWith(SIDECAR)).map((f) => [f.name.slice(0, -SIDECAR.length), f]),
      );
      const problems: string[] = [];
      const queue: File[] = [];
      for (const f of files) {
        if (f.name.endsWith(SIDECAR)) continue;
        if (!VIDEO.test(f.name)) problems.push(`${f.name}: not a video`);
        else if (max && f.size > max) problems.push(`${f.name}: larger than ${formatBytes(max)}`);
        else queue.push(f);
      }
      for (const [name, f] of sidecars) {
        if (!queue.some((v) => stem(v.name) === name)) problems.push(`${f.name}: drop it together with its video`);
      }
      if (problems.length) {
        toast(`Skipped ${plural(problems.length, "file")}`, { variant: "error", description: problems.join("\n") });
      }
      if (!queue.length) return;

      const total = queue.reduce((sum, f) => sum + f.size, 0);
      const abort = new AbortController();
      const signal = abort.signal;
      const progress = toast(`Uploading ${plural(queue.length, "recording")}`, {
        progress: 0,
        actions: [{ label: "Cancel", keepOpen: true, onClick: () => abort.abort() }],
      });
      let sent = 0;
      let shown = 0;
      let uploaded = 0;
      const failures: string[] = [];
      for (const [i, file] of queue.entries()) {
        if (signal.aborted) break;
        progress?.update({ description: `${i + 1} of ${queue.length} · ${file.name}` });
        try {
          const rec = await api.uploadRecording(file, {
            signal,
            onProgress: (loaded) => {
              const pct = Math.floor(((sent + loaded) / total) * 100);
              if (pct !== shown) progress?.update({ progress: (shown = pct) });
            },
          });
          uploaded += 1;
          const sidecar = sidecars.get(stem(file.name));
          if (sidecar) {
            await api.uploadSidecar(rec.id, sidecar, { signal }).catch((err: unknown) => {
              if (!signal.aborted) failures.push(`${sidecar.name}: ${message(err)}`);
            });
          }
          void qc.invalidateQueries({ queryKey: ["recordings"] });
        } catch (err) {
          if (!signal.aborted) failures.push(`${file.name}: ${message(err)}`);
        }
        sent += file.size;
      }
      progress?.close();

      if (uploaded) {
        toast(`Uploaded ${plural(uploaded, "recording")}`, {
          variant: "success",
          description: "Proxies and transcripts follow in the background.",
          actions: [{ label: "View", onClick: () => void navigate({ to: "/recordings" }) }],
        });
      }
      if (signal.aborted) toast("Upload cancelled", { description: "The file being sent was not kept." });
      if (failures.length) {
        toast(`${plural(failures.length, "upload")} failed`, { variant: "error", description: failures.join("\n") });
      }
    },
    [toaster, qc, navigate, max],
  );
}
