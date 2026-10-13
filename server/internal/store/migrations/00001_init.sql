-- +goose Up

-- One play session, from a sidecar's header line.
CREATE TABLE sessions (
    id          bigserial PRIMARY KEY,
    project     text  NOT NULL DEFAULT '',
    session_key text  NOT NULL,
    engine      text  NOT NULL DEFAULT '',
    build       text  NOT NULL DEFAULT '',
    platform    text  NOT NULL DEFAULT '',
    tester      text  NOT NULL DEFAULT '',
    header      jsonb NOT NULL,
    UNIQUE (project, session_key)
);

-- One video file under the footage root. Files are referenced, never copied.
CREATE TABLE recordings (
    id            bigserial PRIMARY KEY,
    path          text        NOT NULL UNIQUE, -- relative to the footage root
    size          bigint      NOT NULL,
    mtime         timestamptz NOT NULL,
    sidecar_path  text,
    sidecar_key   text,  -- size and mtime of the sidecar when last read
    session_id    bigint REFERENCES sessions (id) ON DELETE SET NULL,
    sync_mode     text        NOT NULL DEFAULT '',
    sync_offset   double precision NOT NULL DEFAULT 0, -- added to every event time
    recorded_at   timestamptz NOT NULL,
    duration      double precision,
    fps           double precision,
    width         integer,
    height        integer,
    video_codec   text,
    audio_tracks  integer,
    proxy_path    text,  -- relative to the data dir
    sprite        jsonb, -- thumbnail sheets for hover-scrub
    missing_since timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX recordings_recorded_at ON recordings (recorded_at DESC);

-- Events, spans and markers from sidecars. Times are on the recording's clock.
CREATE TABLE events (
    id           bigserial PRIMARY KEY,
    recording_id bigint   NOT NULL REFERENCES recordings (id) ON DELETE CASCADE,
    type         text     NOT NULL CHECK (type IN ('event', 'span', 'marker')),
    t            double precision NOT NULL,
    dur          double precision,
    kind         text     NOT NULL,
    label        text     NOT NULL DEFAULT '',
    source       text     NOT NULL DEFAULT '',
    tags         text[]   NOT NULL DEFAULT '{}',
    importance   smallint NOT NULL DEFAULT 0,
    data         jsonb    NOT NULL DEFAULT '{}',
    search       tsvector NOT NULL
);
CREATE INDEX events_recording ON events (recording_id, t);
CREATE INDEX events_search ON events USING gin (search);
CREATE INDEX events_tags ON events USING gin (tags);
CREATE INDEX events_kind ON events (kind text_pattern_ops);

-- Timed speech, one row per line per audio track.
CREATE TABLE transcripts (
    id           bigserial PRIMARY KEY,
    recording_id bigint  NOT NULL REFERENCES recordings (id) ON DELETE CASCADE,
    track        integer NOT NULL DEFAULT 0,
    t_in         double precision NOT NULL,
    t_out        double precision NOT NULL,
    text         text    NOT NULL,
    search       tsvector GENERATED ALWAYS AS (to_tsvector('english', text)) STORED
);
CREATE INDEX transcripts_recording ON transcripts (recording_id, track, t_in);
CREATE INDEX transcripts_search ON transcripts USING gin (search);

-- The job queue and processing history. A job is one stage of one recording at
-- one version, model and input; changing any of them queues a new job, so only
-- that stage re-runs. Workers claim with FOR UPDATE SKIP LOCKED.
CREATE TABLE jobs (
    id           bigserial PRIMARY KEY,
    recording_id bigint  NOT NULL REFERENCES recordings (id) ON DELETE CASCADE,
    stage        text    NOT NULL,
    version      integer NOT NULL,
    model        text    NOT NULL DEFAULT '',
    input_key    text    NOT NULL DEFAULT '',
    after        text, -- stage that must be done for the same input first
    status       text    NOT NULL DEFAULT 'queued'
                 CHECK (status IN ('queued', 'running', 'done', 'failed')),
    attempts     integer NOT NULL DEFAULT 0,
    run_after    timestamptz NOT NULL DEFAULT now(),
    worker       text,
    error        text,
    result       jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    started_at   timestamptz,
    heartbeat_at timestamptz,
    finished_at  timestamptz,
    UNIQUE (recording_id, stage, version, model, input_key)
);
CREATE INDEX jobs_queued ON jobs (created_at, id) WHERE status = 'queued';
CREATE INDEX jobs_recording ON jobs (recording_id, stage);

-- Named baskets of trimmed clips, exported as timelines.
CREATE TABLE selects (
    id         bigserial PRIMARY KEY,
    name       text NOT NULL,
    owner      text NOT NULL DEFAULT '',
    notes      text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE select_items (
    id           bigserial PRIMARY KEY,
    select_id    bigint  NOT NULL REFERENCES selects (id) ON DELETE CASCADE,
    recording_id bigint  NOT NULL REFERENCES recordings (id) ON DELETE CASCADE,
    t_in         double precision NOT NULL,
    t_out        double precision NOT NULL,
    position     integer NOT NULL,
    comment      text    NOT NULL DEFAULT '',
    CHECK (t_out > t_in)
);
CREATE INDEX select_items_select ON select_items (select_id, position);

-- +goose Down
DROP TABLE select_items;
DROP TABLE selects;
DROP TABLE jobs;
DROP TABLE transcripts;
DROP TABLE events;
DROP TABLE recordings;
DROP TABLE sessions;
