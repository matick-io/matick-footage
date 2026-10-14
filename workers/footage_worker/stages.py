"""The stages this worker runs. Versions must match server/internal/pipeline."""

from __future__ import annotations

import json
import logging
import math
import os
import shutil
import subprocess
import tempfile
import urllib.parse
import urllib.request
from dataclasses import dataclass
from fractions import Fraction
from pathlib import Path
from typing import Callable

import psycopg

from .queue import Job

SPRITE_COLS, SPRITE_ROWS = 10, 10
SPRITE_W, SPRITE_H = 160, 90
# Proxies drop every other frame (or more) above this; scrubbing doesn't need 60.
MAX_PROXY_FPS = 30
PROXY_DEVICES = ("cpu", "cuda")

log = logging.getLogger("footage_worker")


@dataclass
class Context:
    conn: psycopg.Connection
    core: str  # the core's base URL
    data: Path

    def recording(self, rid: int) -> dict:
        cur = self.conn.execute(
            """SELECT duration, fps, audio_tracks, session_id FROM recordings WHERE id = %s""",
            (rid,),
        )
        row = cur.fetchone()
        if row is None:
            raise LookupError(f"recording {rid} is gone")
        return {"id": rid, **dict(zip(("duration", "fps", "audio_tracks", "session_id"), row))}

    def original(self, rec: dict) -> str:
        """Where ffmpeg reads the recording: a path on the mount the core shares
        with workers, or a signed URL. The core decides, so workers never hold
        storage credentials."""
        url = f"{self.core}/api/v1/recordings/{rec['id']}/original"
        with urllib.request.urlopen(url, timeout=30) as res:
            original = json.load(res)["url"]
        parsed = urllib.parse.urlparse(original)
        return urllib.parse.unquote(parsed.path) if parsed.scheme == "file" else original


@dataclass
class Stage:
    version: int
    run: Callable[[Context, Job], dict]


def run(cmd: list[str]) -> str:
    proc = subprocess.run(cmd, capture_output=True, text=True, stdin=subprocess.DEVNULL)
    if proc.returncode != 0:
        tail = "\n".join(proc.stderr.strip().splitlines()[-15:])
        raise RuntimeError(f"{cmd[0]} exited {proc.returncode}: {tail}")
    return proc.stdout


def parse_rate(rate: str | None) -> float | None:
    try:
        value = float(Fraction(rate)) if rate else 0.0
    except (ValueError, ZeroDivisionError):
        return None
    return value if value > 0 else None


def probe(ctx: Context, job: Job) -> dict:
    rec = ctx.recording(job.recording_id)
    info = json.loads(
        run(["ffprobe", "-v", "error", "-print_format", "json", "-show_format", "-show_streams",
             ctx.original(rec)])
    )
    streams = info.get("streams", [])
    video = next((s for s in streams if s.get("codec_type") == "video"), None)
    if video is None:
        raise ValueError("no video stream")
    audio = [s for s in streams if s.get("codec_type") == "audio"]
    fmt = info.get("format", {})
    duration = float(fmt.get("duration") or video.get("duration") or 0) or None
    fps = parse_rate(video.get("avg_frame_rate")) or parse_rate(video.get("r_frame_rate"))
    created = (fmt.get("tags") or {}).get("creation_time")
    result = {
        "duration": duration, "fps": fps, "width": video.get("width"), "height": video.get("height"),
        "video_codec": video.get("codec_name"), "audio_tracks": len(audio),
    }
    ctx.conn.execute(
        """
        UPDATE recordings SET duration = %(duration)s, fps = %(fps)s, width = %(width)s,
          height = %(height)s, video_codec = %(video_codec)s, audio_tracks = %(audio_tracks)s,
          -- the sidecar's start time wins; the container's creation time beats the file mtime
          recorded_at = CASE WHEN session_id IS NULL AND %(created)s::timestamptz IS NOT NULL
                             THEN %(created)s::timestamptz ELSE recorded_at END,
          updated_at = now()
        WHERE id = %(id)s
        """,
        {**result, "created": created, "id": job.recording_id},
    )
    return result


def proxy_rate(fps: float | None) -> Fraction | None:
    """The proxy frame rate when the source's is too high: the source's divided
    by a whole number, so 59.94 becomes 29.97 and 50 becomes 25. None keeps it."""
    if not fps:
        return None
    rate = Fraction(fps).limit_denominator(1001)
    n = math.ceil(rate / MAX_PROXY_FPS)
    return rate / n if n > 1 else None


def proxy_command(device: str, source: str, fps: float | None, interval: float,
                  out: Path, sheets: Path) -> list[str]:
    """One decode feeds the proxy and the thumbnail sheets. With cuda, decoding,
    scaling and encoding stay on the GPU; only the few thumbnail frames come
    back to the CPU to be tiled."""
    rate = proxy_rate(fps)
    cap = f"fps={rate.numerator}/{rate.denominator}," if rate else ""
    gop = str(max(1, round(rate or fps or 30)))  # a keyframe every second
    height = "'min(720,trunc(ih/2)*2)'"
    tiles = (
        f"scale={SPRITE_W}:{SPRITE_H}:force_original_aspect_ratio=decrease,"
        f"pad={SPRITE_W}:{SPRITE_H}:(ow-iw)/2:(oh-ih)/2,tile={SPRITE_COLS}x{SPRITE_ROWS}"
    )
    if device == "cuda":
        # No -extra_hw_frames: with 4 or more, a Quadro K2200 can't create the decoder.
        decode = ["-hwaccel", "cuda", "-hwaccel_output_format", "cuda"]
        scale = f"scale_cuda=-2:{height}"
        thumbs = f"fps=1/{interval},hwdownload,format=nv12,{tiles}"
        encode = ["-c:v", "h264_nvenc", "-preset", "p4", "-rc", "vbr", "-cq", "23", "-b:v", "0",
                  "-g", gop, "-forced-idr", "1"]
    else:
        decode = []
        scale = f"scale=-2:{height},format=yuv420p"
        thumbs = f"fps=1/{interval},{tiles}"
        encode = ["-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
                  "-force_key_frames", "expr:gte(t,n_forced*1)"]
    return [
        "ffmpeg", "-hide_banner", "-nostdin", "-loglevel", "error", "-y",
        *decode, "-i", source,
        "-filter_complex", f"[0:v:0]{cap}split=2[p][s];[p]{scale}[pv];[s]{thumbs}[sv]",
        "-map", "[pv]", "-map", "0:a:0?", *encode,
        "-c:a", "aac", "-b:a", "128k", "-ac", "2", "-movflags", "+faststart", str(out),
        "-map", "[sv]", "-q:v", "4", str(sheets / "sheet_%03d.jpg"),
    ]


def proxy(ctx: Context, job: Job) -> dict:
    """A 720p H.264 proxy of at most 30 fps with a keyframe every second, for
    scrubbing, plus thumbnail sheets for hover-scrub. FOOTAGE_PROXY_DEVICE=cuda
    uses an NVIDIA GPU; if that fails, the CPU makes this proxy and the result
    says why."""
    rec = ctx.recording(job.recording_id)
    duration = rec["duration"] or 0
    interval = 1.0 if duration and duration < 120 else 2.0
    device = os.environ.get("FOOTAGE_PROXY_DEVICE", "cpu")

    proxies = ctx.data / "proxies"
    sprites = ctx.data / "sprites"
    proxies.mkdir(parents=True, exist_ok=True)
    sprites.mkdir(parents=True, exist_ok=True)
    out = proxies / f"{job.recording_id}.mp4"
    sheets = sprites / str(job.recording_id)

    fallback = None
    source = ctx.original(rec)
    with tempfile.TemporaryDirectory(dir=ctx.data) as tmp:
        tmp_proxy = Path(tmp) / "proxy.mp4"
        tmp_sheets = Path(tmp) / "sheets"
        for attempt in dict.fromkeys([device, "cpu"]):
            shutil.rmtree(tmp_sheets, ignore_errors=True)
            tmp_sheets.mkdir()
            try:
                run(proxy_command(attempt, source, rec["fps"], interval, tmp_proxy, tmp_sheets))
            except RuntimeError as err:
                if attempt == "cpu":
                    raise
                log.warning("proxy on %s failed, falling back to the CPU: %s", attempt, err)
                fallback = f"{attempt}: {str(err)[-500:]}"
                continue
            device = attempt
            break
        os.replace(tmp_proxy, out)
        if sheets.exists():
            shutil.rmtree(sheets)
        tmp_sheets.rename(sheets)

    count = math.ceil(duration / interval) if duration else 0
    sprite = {
        "interval": interval, "cols": SPRITE_COLS, "rows": SPRITE_ROWS,
        "width": SPRITE_W, "height": SPRITE_H, "count": count,
        "sheets": len(list(sheets.glob("sheet_*.jpg"))),
    }
    size = out.stat().st_size
    cur = ctx.conn.execute(
        "UPDATE recordings SET proxy_path = %s, sprite = %s, updated_at = now() WHERE id = %s",
        (f"proxies/{job.recording_id}.mp4", json.dumps(sprite), job.recording_id),
    )
    if cur.rowcount == 0:  # deleted while this ran
        out.unlink(missing_ok=True)
        shutil.rmtree(sheets, ignore_errors=True)
    result = {"proxy_bytes": size, "device": device, **sprite}
    if fallback:
        result["fallback"] = fallback
    return result


_models: dict[str, object] = {}


def _whisper(name: str):
    if name not in _models:
        from faster_whisper import WhisperModel  # heavy import, only when transcribing

        device = os.environ.get("FOOTAGE_WHISPER_DEVICE", "cpu")
        compute = os.environ.get("FOOTAGE_WHISPER_COMPUTE", "int8" if device == "cpu" else "float16")
        _models[name] = WhisperModel(name, device=device, compute_type=compute)
    return _models[name]


def transcribe(ctx: Context, job: Job) -> dict:
    rec = ctx.recording(job.recording_id)
    tracks = rec["audio_tracks"] or 0
    language = os.environ.get("FOOTAGE_WHISPER_LANGUAGE") or None
    lines: list[tuple[int, float, float, str]] = []
    detected = None
    if tracks:
        import numpy as np

        model = _whisper(job.model)
        source = ctx.original(rec)
        for track in range(tracks):
            # Decode with ffmpeg rather than faster-whisper's PyAV path, which
            # breaks across PyAV releases: 16 kHz mono float samples on stdout.
            pcm = subprocess.run(
                ["ffmpeg", "-hide_banner", "-nostdin", "-loglevel", "error",
                 "-i", source, "-map", f"0:a:{track}",
                 "-ac", "1", "-ar", "16000", "-f", "f32le", "-"],
                capture_output=True, check=True,
            ).stdout
            audio = np.frombuffer(pcm, dtype=np.float32)
            if audio.size == 0:
                continue
            # Word timestamps: segment bounds stretch over the silence before a line.
            segments, info = model.transcribe(
                audio, language=language, vad_filter=True, word_timestamps=True)
            detected = detected or info.language
            for seg in segments:
                text = seg.text.strip()
                if text:
                    start = seg.words[0].start if seg.words else seg.start
                    end = seg.words[-1].end if seg.words else seg.end
                    lines.append((track, start, end, text))
    with ctx.conn.transaction():
        ctx.conn.execute("DELETE FROM transcripts WHERE recording_id = %s", (job.recording_id,))
        with ctx.conn.cursor() as cur:
            cur.executemany(
                """INSERT INTO transcripts (recording_id, track, t_in, t_out, text)
                   VALUES (%s, %s, %s, %s, %s)""",
                [(job.recording_id, *line) for line in lines],
            )
    return {"tracks": tracks, "lines": len(lines), "language": detected}


STAGES: dict[str, Stage] = {
    "probe": Stage(1, probe),
    "proxy": Stage(2, proxy),
    "transcribe": Stage(1, transcribe),
}
