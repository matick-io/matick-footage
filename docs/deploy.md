# Deploying

The app is three containers: Postgres, the **core** (web UI and API on port
8420) and a **media worker**. [`deploy/compose.yml`](../deploy/compose.yml) is
the whole deployment. It pulls public images from GitHub's container registry,
so any Docker host with Compose v2 on linux/amd64 can run it. No account or
registry login is needed.

## Storage

Decide where each of these lives. Each one is a host folder (or a NAS dataset),
or a Docker volume if you leave its setting unset.

| Setting | Mounted at | What | Access |
|---|---|---|---|
| `FOOTAGE_DIR` | `/footage` | A folder that cameras, OBS or the engine record to. Indexed in place. Optional. | read |
| `FOOTAGE_LIBRARY_DIR` | `/library` | Uploads. The only place the app writes recordings, and the only recordings it can delete. | read, write |
| `FOOTAGE_DATA_DIR` | `/data` | Proxies and thumbnails. Rebuilt from the originals if lost. | read, write |
| `FOOTAGE_DB_DIR` | Postgres | The index, selects and job history. | Postgres manages it |

Without `FOOTAGE_DIR`, the app holds only what is uploaded, so it works without
a NAS. To watch more than one folder, mount each one in both `core` and
`worker-media` at the same path, and list them in `FOOTAGE_WATCH`, e.g.
`footage=file:///footage;cams=file:///cams`.

The core and the worker run as **uid 1000**. That user needs read access to the
recordings, and write access to the library and data folders. Give the database
an empty folder of its own.

## Install

```bash
mkdir footage && cd footage
VERSION=0.3.0   # the release to run; see the repository's Releases page
curl -fsSLo compose.yml https://raw.githubusercontent.com/matick-io/matick-footage/v$VERSION/deploy/compose.yml
```

Compose reads settings from a `.env` file next to `compose.yml`:

```ini
FOOTAGE_VERSION=0.3.0
FOOTAGE_DIR=/mnt/tank/recordings
FOOTAGE_LIBRARY_DIR=/mnt/tank/footage/library
FOOTAGE_DATA_DIR=/mnt/tank/footage/data
FOOTAGE_DB_DIR=/mnt/tank/footage/db
POSTGRES_PASSWORD=pick-one
```

```bash
docker compose up -d
```

Then open `http://<host>:8420`. The first transcription downloads the speech
model, so the worker needs internet access once.

**TrueNAS SCALE:** create a custom app with "Install via YAML" and paste in
`compose.yml`, replacing each `${…}` with your own value. `POSTGRES_PASSWORD`
appears twice; use the same value in both. These containers use uid 1000, not
TrueNAS's own apps user (568), so grant uid 1000 access to the datasets.

## Settings

| Variable | Default | |
|---|---|---|
| `FOOTAGE_VERSION` | `latest` | Image tag. Pin a release, such as `0.3.0`. |
| `FOOTAGE_PORT` | `8420` | Port on the host. |
| `POSTGRES_PASSWORD` | `footage` | Only used when the database is first created. |
| `FOOTAGE_WATCH` | `footage=file:///footage` | Watched sources, `name=url;…`. |
| `FOOTAGE_UPLOAD_MAX` | `50G` | Largest upload. |
| `FOOTAGE_SCAN_INTERVAL` | `30s` | How often watched folders are rescanned. |
| `FOOTAGE_STABLE_FOR` | `1m` | How long a file must stop changing before it's indexed. |
| `FOOTAGE_PROXY_DEVICE` | `cpu` | What makes proxies: `cpu`, or `cuda` for an NVIDIA GPU. See [GPU proxies](#gpu-proxies). |
| `FOOTAGE_TRANSCRIBE_MODEL` | `small` | Speech model: `tiny`, `base`, `small`, `medium`, `large-v3`, or `off`. |
| `FOOTAGE_WHISPER_LANGUAGE` | detect | Language code, such as `en`, when detection guesses wrong. |
| `FOOTAGE_EXPORT_TARGETS` | none | See [Exporting](#exporting). |

## GPU proxies

Making proxies is most of the indexing time. On a CPU it runs at about real
time; an NVIDIA GPU (NVENC, Maxwell or newer) makes it much faster. Proxies are
720p at up to 30 fps either way. If a GPU proxy fails, the CPU makes that one
instead, and the job's result on the Jobs page shows the device used and why.

Install the NVIDIA container toolkit on the host, fetch
[`gpu-nvidia.yml`](../deploy/gpu-nvidia.yml) next to `compose.yml`, and start
with both:

```bash
docker compose -f compose.yml -f gpu-nvidia.yml up -d
```

On TrueNAS SCALE, turn on "Install NVIDIA Drivers" in the Apps settings, then
add that file's `environment` and `deploy` lines to `worker-media` in the app's
YAML.

## Adding footage

- **Watched folder:** record or copy into it. A new file appears within about two
  minutes. Put the event sidecar next to the video with the same name:
  `session_0142.mp4` and `session_0142.events.jsonl`.
- **Upload:** drop files on the Recordings page, or use its Upload button.
  Videos dropped together with their sidecars are paired. From a script:

  ```bash
  curl -T session_0142.mp4 http://<host>:8420/api/v1/uploads/session_0142.mp4
  ```

Previews, thumbnails and transcripts follow in the background; the Jobs page
shows progress.

## Checking it works

1. `http://<host>:8420` loads, and the Recordings page lists the watched folder's
   recordings within a few minutes.
2. A short video dropped on the Recordings page appears right away.
3. On the Jobs page, its probe, proxy and transcribe jobs finish as done. With a
   GPU, the proxy job's result shows `"device": "cuda"`.

## Exporting

Exported timelines point at the original files, so each editing machine needs
to know where it sees each source. Define one target per kind of machine, with a
prefix per source:

```ini
FOOTAGE_EXPORT_TARGETS='Resolve (Mac)=footage:/Volumes/recordings|library:/Volumes/library;Premiere (Windows)=footage:\\nas\recordings|library:\\nas\library'
```

Without a target, exports use the paths the server sees.

## Upgrading

Set `FOOTAGE_VERSION` to the new release, then:

```bash
docker compose pull && docker compose up -d
```

The database migrates on start. Read the release notes first: some releases
change settings or mounts.

## Backups

Back up the database and the library. Proxies and thumbnails can be rebuilt, and
watched folders belong to whatever writes them.

```bash
docker compose exec -T postgres pg_dump -U footage footage > footage.sql
```

## Security

There is no sign-in yet. Anyone who can reach the port can browse, upload, and
delete library recordings. Keep the app on a trusted network. To reach it from
elsewhere, put it behind a VPN or a reverse proxy that handles sign-in, and
raise the proxy's request size limit (nginx: `client_max_body_size`) to allow
large uploads.
