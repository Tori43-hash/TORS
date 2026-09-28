CREATE TABLE users (
    id           bigserial PRIMARY KEY,
    telegram_id  bigint      NOT NULL UNIQUE,
    first_name   text        NOT NULL DEFAULT '',
    username     text        NOT NULL DEFAULT '',
    lang         text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);
