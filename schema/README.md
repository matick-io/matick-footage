# Event sidecar

Every recording can have a JSON-lines sidecar with the same base name:
`session_0142.mp4` + `session_0142.events.jsonl`. Any engine or tool can write one.
[`events.schema.json`](events.schema.json) validates one line.

The first line describes the session. Every later line is one `event`, `span` or `marker`.
See [`examples/basic.events.jsonl`](examples/basic.events.jsonl).

## Rules

- `t` is seconds from the first frame of the recording, as a float, on the recording's clock.
- `kind` is a dotted, studio-defined string (`player.death`, `boss.phase`). Reserved kinds:
  `level` (a span naming the map or level), `marker`, `sync.flash`.
- `importance` (0–3) ranks moments. Defaults to 0.
- `data` is free-form and indexed as JSON.
- `recording.sync` says how times were aligned: `obs`, `timecode` or `wallclock`.
- `schema` is `matick-footage/events@1`. Within a major version, changes are additive only,
  so readers ignore fields they don't know.
- A line that doesn't validate is skipped; the rest of the file is still indexed.

Check a file with `footage validate <file>...`.
