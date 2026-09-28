CREATE TABLE jobs (
    id           bigserial PRIMARY KEY,
    kind         text        NOT NULL,
    payload      jsonb       NOT NULL DEFAULT '{}',
    run_at       timestamptz NOT NULL DEFAULT now(),
    attempts     int         NOT NULL DEFAULT 0,
    max_attempts int         NOT NULL DEFAULT 12,
    last_error   text,
    dead         boolean     NOT NULL DEFAULT false,
    done_at      timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX jobs_pending ON jobs (run_at, id) WHERE done_at IS NULL AND NOT dead;

CREATE TABLE periodic (
    name     text PRIMARY KEY,
    next_run timestamptz NOT NULL
);
