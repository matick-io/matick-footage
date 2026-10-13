"""Run a worker: claim jobs for the configured stages until stopped."""

from __future__ import annotations

import logging
import os
import signal
import socket
import sys
import time
from pathlib import Path

import psycopg

from . import queue
from .stages import STAGES, Context

log = logging.getLogger("footage_worker")


class Stopped(Exception):
    pass


def connect(dsn: str) -> psycopg.Connection:
    """Waits for the database and for the core to have migrated it."""
    for attempt in range(60):
        try:
            conn = psycopg.connect(dsn, autocommit=True)
            if conn.execute("SELECT to_regclass('jobs')").fetchone()[0]:
                return conn
            conn.close()
            log.info("waiting for the core to create the schema")
        except psycopg.OperationalError as err:
            log.info("waiting for database: %s", err)
        time.sleep(2)
    raise SystemExit("database not ready")


def main() -> None:
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    dsn = os.environ["FOOTAGE_DATABASE_URL"]
    names = [s.strip() for s in os.environ.get("WORKER_STAGES", ",".join(STAGES)).split(",") if s.strip()]
    unknown = [n for n in names if n not in STAGES]
    if unknown:
        raise SystemExit(f"unknown stages {unknown}; this worker runs {list(STAGES)}")
    keys = [f"{n}:{STAGES[n].version}" for n in names]
    worker = os.environ.get("WORKER_NAME") or f"worker@{socket.gethostname()}"

    def stop(*_):
        raise Stopped()

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)

    conn = connect(dsn)
    ctx = Context(conn, Path(os.environ.get("FOOTAGE_ROOT", "/footage")),
                  Path(os.environ.get("FOOTAGE_DATA", "/data")))
    log.info("%s claiming %s", worker, keys)
    try:
        while True:
            job = queue.claim(conn, keys, worker)
            if job is None:
                time.sleep(2)
                continue
            log.info("job %d: %s for recording %d", job.id, job.stage, job.recording_id)
            started = time.monotonic()
            try:
                with queue.Heartbeat(dsn, job):
                    result = STAGES[job.stage].run(ctx, job)
            except Stopped:
                queue.release(conn, job)
                raise
            except Exception as err:  # a failed stage must not stop the worker
                log.exception("job %d failed", job.id)
                queue.fail(conn, job, f"{type(err).__name__}: {err}")
                continue
            queue.complete(conn, job, result)
            log.info("job %d done in %.1fs: %s", job.id, time.monotonic() - started, result)
    except Stopped:
        log.info("stopping")
        sys.exit(0)


if __name__ == "__main__":
    main()
