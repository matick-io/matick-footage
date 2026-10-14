# matick-footage

A self-hosted footage index. Point it at a folder of gameplay recordings, or
upload them, and it turns them into a searchable clip library: by game event,
tag, tester marker and speech. Picked clips export as timelines that open in an
editor, pointing at the original files.

Engines log what happened next to each recording in an
[event sidecar](schema/README.md), so search knows that boss phase 2 started at
14:32 before any AI runs. Watched folders are never written to, and nothing
leaves the server.

> Experimental. Built in the open, but no support is promised yet.

## How it works

```
watched folders (read-only) ──▶  core (Go)  ──▶  Postgres  ◀──  worker-media (Python)
  session_0142.mp4                scanner           events        probe
  session_0142.events.jsonl       uploads           transcripts   proxy + thumbnails
library (uploads) ◀─────────────  events stage      jobs          transcribe
                                  /api/v1 + UI
```

- Recordings live in **sources**. Watched sources are folders something else
  records to, like a camera share, and are indexed in place. The **library** is
  the app's own: uploads land there, and only its recordings can be deleted. Each
  source is a Go CDK bucket URL, so a folder can later become an S3 bucket.
- The **core** rescans every source, queues each new or changed file's stages,
  reads sidecars, takes uploads, and serves the API and the web UI.
- **Workers** claim jobs from a Postgres table (`FOR UPDATE SKIP LOCKED`), so any
  number can run anywhere that reaches the database and the footage. They ask the
  core where each original is, so they never hold storage credentials. A job is
  one stage at one version and model, so changing a model re-runs only that stage.
- **Search** ranks events and transcript lines into clips, a recording plus in
  and out times, which is what the UI, export and scripts all work with.
- **Selects** are baskets of trimmed clips. They export as OpenTimelineIO, with
  paths rewritten for each editing machine (`FOOTAGE_EXPORT_TARGETS`).

## Running it

Needs Docker. [`deploy/compose.yml`](deploy/compose.yml) is the whole
deployment and pulls the published images from GitHub's container registry:

```bash
FOOTAGE_DIR=/mnt/footage docker compose -f deploy/compose.yml up -d
```

Then open http://localhost:8420. `FOOTAGE_DIR` is optional: without it, the app
holds only what you upload, by dropping files on the page or from a script:

```bash
curl -T session_0142.mp4 http://localhost:8420/api/v1/uploads/session_0142.mp4
```

Settings are `FOOTAGE_*` variables, listed in the compose file. On TrueNAS SCALE, paste the compose file into a custom app
("Install via YAML") with the `${…}` values replaced by your dataset paths.

Images are published by CI: `:main` for every push to main, and `:X.Y.Z` plus
`:latest` for a tag `vX.Y.Z`.

## Developing

Local development builds the same stack from source
([`dev/compose.yml`](dev/compose.yml)) and runs it against synthetic recordings:

```bash
make samples   # writes test recordings with sidecars and speech to dev/footage
make up        # builds the images and starts the stack
make web-dev   # optional: Vite with hot reload on :5173, using the stack's API
make test
```

The UI is built with the [`@matick-io/ui`](https://www.npmjs.com/package/@matick-io/ui)
design system.

## Layout

| Path | What |
|---|---|
| `schema/` | Event sidecar format: JSON Schema and examples |
| `server/` | Go core: scanner, job queue, events stage, API, OTIO export |
| `workers/` | Python media worker: probe, proxy, transcribe |
| `web/` | Web UI |
| `deploy/` | Compose stack |
| `dev/` | Build override and synthetic sample generator |

## License

[Apache-2.0](LICENSE)
