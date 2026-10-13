import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Badge, Button, EmptyState, ScrollArea } from "@khaos-systems/ui-core";
import { Activity, RotateCcw } from "lucide-react";
import { api, type Job } from "../api";
import { formatDate } from "../format";

const tone = { done: "success", failed: "danger", running: "info", queued: "dim" } as const;

function took(j: Job): string {
  if (!j.started_at || !j.finished_at) return "";
  const s = (Date.parse(j.finished_at) - Date.parse(j.started_at)) / 1000;
  return s < 60 ? `${s.toFixed(1)}s` : `${(s / 60).toFixed(1)}m`;
}

export function JobsPage() {
  const qc = useQueryClient();
  const jobs = useQuery({ queryKey: ["jobs"], queryFn: api.jobs, refetchInterval: 3000 });
  const rerun = useMutation({
    mutationFn: (j: Job) => api.reprocess(j.recording_id, j.stage),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["jobs"] }),
  });
  const list = jobs.data ?? [];
  if (!jobs.isPending && !list.length) {
    return <EmptyState icon={<Activity className="h-5 w-5" />} title="No jobs yet" detail="Jobs are queued when the scanner finds recordings." />;
  }
  return (
    <ScrollArea className="h-full">
      <table className="w-full text-xs">
        <thead className="sticky top-0 bg-surface-panel text-left text-text-muted">
          <tr>
            {["Recording", "Stage", "Status", "Attempts", "Took", "Worker", "Queued", "Result", ""].map((h) => (
              <th key={h} className="px-3 py-2 font-medium">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {list.map((j) => (
            <tr key={j.id} className="border-t border-border-default align-top">
              <td className="px-3 py-1.5">{j.recording}</td>
              <td className="px-3 py-1.5">
                {j.stage} <span className="text-text-muted">v{j.version}{j.model ? ` · ${j.model}` : ""}</span>
              </td>
              <td className="px-3 py-1.5">
                <Badge size="xs" tone={tone[j.status]}>
                  {j.status}
                </Badge>
              </td>
              <td className="px-3 py-1.5">{j.attempts}</td>
              <td className="px-3 py-1.5">{took(j)}</td>
              <td className="px-3 py-1.5 text-text-muted">{j.worker ?? ""}</td>
              <td className="px-3 py-1.5 text-text-muted">{formatDate(j.created_at)}</td>
              <td className="max-w-md px-3 py-1.5">
                {j.error ? (
                  <span className="text-status-danger">{j.error}</span>
                ) : j.result ? (
                  <code className="break-all text-text-muted">{JSON.stringify(j.result)}</code>
                ) : null}
              </td>
              <td className="px-3 py-1.5">
                {j.status === "done" || j.status === "failed" ? (
                  <Button size="small" variant="ghost" kind="icon" tooltip="Re-run" onClick={() => rerun.mutate(j)}>
                    <RotateCcw className="h-3.5 w-3.5" />
                  </Button>
                ) : null}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </ScrollArea>
  );
}
