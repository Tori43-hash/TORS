CREATE TABLE orders (
    id         bigserial PRIMARY KEY,
    user_id    bigint      NOT NULL,
    plan_id    text        NOT NULL,
    amount     integer     NOT NULL,
    currency   text        NOT NULL DEFAULT 'XTR',
    status     text        NOT NULL DEFAULT 'pending',
    meta       jsonb       NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    paid_at    timestamptz
);

CREATE INDEX orders_user ON orders (user_id, created_at DESC);

CREATE TABLE payments (
    id         bigserial PRIMARY KEY,
    order_id   bigint      NOT NULL UNIQUE REFERENCES orders (id),
    gateway    text        NOT NULL,
    charge_id  text        NOT NULL,
    amount     integer     NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (gateway, charge_id)
);
