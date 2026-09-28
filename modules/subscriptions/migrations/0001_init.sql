CREATE TABLE subscriptions (
    id          bigserial PRIMARY KEY,
    user_id     bigint      NOT NULL,
    plan_id     text        NOT NULL,
    panel       text        NOT NULL,
    ref         text        NOT NULL DEFAULT '',
    url         text        NOT NULL DEFAULT '',
    label       text        NOT NULL,
    status      text        NOT NULL DEFAULT 'pending',
    expires_at  timestamptz NOT NULL,
    traffic_gb  integer     NOT NULL DEFAULT 0,
    devices     integer     NOT NULL DEFAULT 0,
    reminded_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX subscriptions_user ON subscriptions (user_id);
CREATE INDEX subscriptions_active ON subscriptions (expires_at) WHERE status = 'active';

-- Every grant (a paid order, a trial) is recorded once by its key.
CREATE TABLE grants (
    key             text PRIMARY KEY,
    subscription_id bigint      NOT NULL REFERENCES subscriptions (id),
    created_at      timestamptz NOT NULL DEFAULT now()
);
