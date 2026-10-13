"""Writes synthetic playtest recordings: a test pattern with on-screen captions,
synthesized speech, and an events sidecar for some of them."""

import json
import subprocess
import sys
import tempfile
from pathlib import Path

FONT = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"

SAMPLES = [
    {
        "name": "playtest/2026-10-13/session_0001",
        "duration": 90,
        "source": "testsrc2",
        "session": {"session_id": "0001", "build": "0.1.0-cl100", "tester": "t01",
                    "started_at": "2026-10-13T14:32:05.120Z"},
        "lines": [
            (6, "Okay, entering the forest now."),
            (14, "Oh no, I fell off the bridge."),
            (50, "Here comes the Warden. Phase two!"),
            (62, "That was hilarious."),
        ],
        "events": [
            {"type": "span", "t": 0.0, "dur": 45.0, "kind": "level", "label": "Forest 1-1"},
            {"type": "event", "t": 15.2, "kind": "player.death", "label": "Fell off the bridge",
             "tags": ["fail"], "importance": 1},
            {"type": "span", "t": 45.0, "dur": 45.0, "kind": "level", "label": "Forest 1-2"},
            {"type": "event", "t": 52.0, "kind": "boss.phase", "label": "Warden phase 2",
             "tags": ["boss", "combat"], "importance": 3, "data": {"boss": "Warden", "phase": 2}},
            {"type": "event", "t": 58.0, "kind": "player.death", "label": "Crushed by the Warden",
             "tags": ["boss", "fail"], "importance": 2},
            {"type": "marker", "t": 64.0, "source": "tester", "tags": ["funny"]},
        ],
    },
    {
        "name": "playtest/2026-10-14/session_0002",
        "duration": 60,
        "source": "smptehdbars",
        "session": {"session_id": "0002", "build": "0.1.1-cl104", "tester": "t02",
                    "started_at": "2026-10-14T10:05:00Z"},
        "lines": [
            (4, "Testing the new grappling hook."),
            (20, "The door is closing. Slide under it!"),
            (40, "Found a bug. The wall has no collision."),
        ],
        "events": [
            {"type": "span", "t": 0.0, "dur": 60.0, "kind": "level", "label": "Castle Gate"},
            {"type": "event", "t": 8.0, "kind": "ability.unlock", "label": "Grappling hook",
             "importance": 1},
            {"type": "event", "t": 22.5, "kind": "player.slide", "label": "Slid under the closing door",
             "tags": ["epic"], "importance": 3},
            {"type": "marker", "t": 44.0, "source": "tester", "tags": ["bug"]},
        ],
    },
    {
        "name": "external/capture_0003",
        "duration": 40,
        "source": "testsrc",
        "session": None,  # no sidecar: found by its transcript only
        "lines": [
            (5, "This capture has no sidecar, only speech."),
            (20, "The boss music is far too loud."),
        ],
        "events": [],
    },
]


def sh(*cmd):
    subprocess.run(cmd, check=True, stdin=subprocess.DEVNULL)


def escape(text):
    return text.replace("\\", "\\\\").replace(":", "\\:").replace("'", "’")


def make(out: Path, s: dict, tmp: Path):
    video = out / f"{s['name']}.mp4"
    video.parent.mkdir(parents=True, exist_ok=True)
    dur = s["duration"]

    inputs, delays = [], []
    for i, (t, text) in enumerate(s["lines"]):
        wav = tmp / f"line{i}.wav"
        sh("espeak-ng", "-v", "en-us", "-s", "150", "-w", str(wav), text)
        inputs += ["-i", str(wav)]
        delays.append(f"[{i + 1}:a]adelay={t * 1000}:all=1[l{i}]")

    captions = []
    for e in s["events"]:
        kind = e.get("kind", "marker")
        label = e.get("label") or ", ".join(e.get("tags", []))
        start = e["t"]
        end = start + (e["dur"] if e["type"] == "span" else 3)
        y = "h-80" if e["type"] == "span" else "60"
        captions.append(
            f"drawtext=fontfile={FONT}:text='{escape(kind + '  ' + label)}':"
            f"x=40:y={y}:fontsize=36:fontcolor=white:box=1:boxcolor=black@0.6:boxborderw=12:"
            f"enable='between(t,{start},{end})'"
        )
    captions.append(
        f"drawtext=fontfile={FONT}:text='%{{pts\\:hms}}':x=w-tw-40:y=40:fontsize=32:"
        "fontcolor=white:box=1:boxcolor=black@0.6:boxborderw=10"
    )
    n = len(s["lines"])
    graph = ";".join(
        [f"[0:v]{','.join(captions)}[v]"]
        + delays
        + [f"anullsrc=r=48000:cl=mono,atrim=0:{dur}[bed]",
           "[bed]" + "".join(f"[l{i}]" for i in range(n)) + f"amix=inputs={n + 1}:duration=first:normalize=0[a]"]
    )
    sh("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
       "-f", "lavfi", "-i", f"{s['source']}=size=1280x720:rate=30:duration={dur}",
       *inputs, "-filter_complex", graph, "-map", "[v]", "-map", "[a]",
       "-c:v", "libx264", "-preset", "veryfast", "-crf", "28", "-pix_fmt", "yuv420p",
       "-c:a", "aac", "-b:a", "96k", "-movflags", "+faststart", str(video))

    if s["session"]:
        sess = s["session"]
        header = {
            "type": "session", "schema": "matick-footage/events@1",
            "session_id": sess["session_id"], "engine": "unreal", "project": "SampleGame",
            "build": sess["build"], "platform": "Win64", "tester": sess["tester"],
            "recording": {"file": video.name, "sync": "obs", "started_at": sess["started_at"]},
        }
        lines = [json.dumps(header)] + [json.dumps(e) for e in s["events"]]
        (out / f"{s['name']}.events.jsonl").write_text("\n".join(lines) + "\n")
    print(f"wrote {video.relative_to(out)}")


def main():
    out = Path(sys.argv[1])
    for s in SAMPLES:
        with tempfile.TemporaryDirectory() as tmp:
            make(out, s, Path(tmp))


if __name__ == "__main__":
    main()
