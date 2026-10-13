"""The job table, as the Go core's server/internal/queue sees it."""

from __future__ import annotations

import json
import threading
from dataclasses import dataclass

import psycopg

MAX_ATTEMPTS = 3
HEARTBEAT_SECONDS = 30


@dataclass
class Job:
    id: int
    recording_id: int
    stage: str
    version: int
    model: str
    input_key: str
    attempts: int


def claim(conn: psycopg.Connection, keys: list[str], worker: str) -> Job | None:
    row = conn.execute(
        """
        UPDATE jobs SET status = 'running', attempts = attempts + 1, worker = %(worker)s,
               started_at = now(), heartbeat_at = now(), error = NULL
        WHERE id = (
            SELECT j.id FROM jobs j
            WHERE j.status = 'queued' AND j.run_after <= now()
              AND j.stage || ':' || j.version = ANY (%(keys)s)
              AND (j.after IS NULL OR EXISTS (
                    SELECT 1 FROM jobs p
                    WHERE p.recording_id = j.recording_id AND p.stage = j.after
                      AND p.input_key = j.input_key AND p.status = 'done'))
            ORDER BY j.created_at, j.id
            LIMIT 1
            FOR UPDATE SKIP LOCKED)
        RETURNING id, recording_id, stage, version, model, input_key, attempts
        """,
        {"keys": keys, "worker": worker},
    ).fetchone()
    return Job(*row) if row else None


def complete(conn: psycopg.Connection, job: Job, result: dict) -> None:
    conn.execute(
        """UPDATE jobs SET status = 'done', result = %s, finished_at = now(), error = NULL
           WHERE id = %s""",
        (json.dumps(result), job.id),
    )


def fail(conn: psycopg.Connection, job: Job, error: str) -> None:
    conn.execute(
        """
        UPDATE jobs SET
          status = CASE WHEN attempts < %(max)s THEN 'queued' ELSE 'failed' END,
          run_after = now() + attempts * interval '1 minute',
          finished_at = CASE WHEN attempts < %(max)s THEN NULL ELSE now() END,
          error = %(error)s
        WHERE id = %(id)s
        """,
        {"max": MAX_ATTEMPTS, "error": error[-4000:], "id": job.id},
    )


def release(conn: psycopg.Connection, job: Job) -> None:
    """Hand a job back untouched, when the worker is stopped mid-run."""
    conn.execute(
        """UPDATE jobs SET status = 'queued', attempts = attempts - 1, worker = NULL
           WHERE id = %s AND status = 'running'""",
        (job.id,),
    )


class Heartbeat:
    """Keeps a running job's heartbeat fresh from a thread with its own connection,
    so long stages aren't mistaken for dead workers."""

    def __init__(self, dsn: str, job: Job):
        self._dsn, self._job = dsn, job
        self._stop = threading.Event()
        self._thread = threading.Thread(target=self._run, daemon=True)

    def __enter__(self) -> Heartbeat:
        self._thread.start()
        return self

    def __exit__(self, *exc) -> None:
        self._stop.set()
        self._thread.join(timeout=5)

    def _run(self) -> None:
        with psycopg.connect(self._dsn, autocommit=True) as conn:
            while not self._stop.wait(HEARTBEAT_SECONDS):
                conn.execute("UPDATE jobs SET heartbeat_at = now() WHERE id = %s", (self._job.id,))
