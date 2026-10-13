# matick-footage

A self-hosted footage index. Point it at a folder of gameplay recordings and it
turns them into a searchable clip library: by game event, tag, tester marker and
speech. Picked clips export as timelines that open in an editor, pointing at the
original files.

Engines log what happened next to each recording in an
[event sidecar](schema/README.md), so search knows that boss phase 2 started at
14:32 before any AI runs. Original files are never modified, and nothing leaves
the server.

> Experimental. Built in the open, but no support is promised yet.

## How it works

```
recordings/ (read-only)  ──▶  core (Go)  ──▶  Postgres  ◀──  worker-media (Python)
  session_0142.mp4             scanner           events        probe
  session_0142.events.jsonl    events stage      transcripts   proxy + thumbnails
                               /api/v1 + UI      jobs          transcribe
```

- The **core** rescans the recordings folder, queues each new or changed file's
  stages, reads sidecars, and serves the API and the web UI.
- **Workers** claim jobs from a Postgres table (`FOR UPDATE SKIP LOCKED`), so any
  number can run anywhere that reaches the database and the footage. A job is one
  stage at one version and model, so changing a model re-runs only that stage.
- **Search** ranks events and transcript lines into clips, a recording plus in
  and out times, which is what the UI, export and scripts all work with.
- **Selects** are baskets of trimmed clips. They export as OpenTimelineIO, with
  paths rewritten for each editing machine (`FOOTAGE_EXPORT_TARGETS`).

## Running it

Needs Docker. The stack in [`deploy/compose.yml`](deploy/compose.yml) is the
whole deployment:

```bash
FOOTAGE_DIR=/mnt/footage docker compose -f deploy/compose.yml up -d
```

Then open http://localhost:8420. Settings are `FOOTAGE_*` variables, listed in
the compose file.

## Developing

Local development runs the same stack against synthetic recordings:

```bash
make samples   # writes test recordings with sidecars and speech to dev/footage
make up        # builds the UI and starts the stack
make web-dev   # optional: Vite with hot reload on :5173, using the stack's API
make test
```

The UI is built with the matick-ui design system. Until its packages are on npm,
`web/` links them from a matick checkout: put `MATICK_DIR := /path/to/matick` in
`local.mk`.

## Layout

| Path | What |
|---|---|
| `schema/` | Event sidecar format: JSON Schema and examples |
| `server/` | Go core: scanner, job queue, events stage, API, OTIO export |
| `workers/` | Python media worker: probe, proxy, transcribe |
| `web/` | Web UI |
| `deploy/` | Compose stack |
| `dev/` | Synthetic sample generator |

## License

[Apache-2.0](LICENSE)
