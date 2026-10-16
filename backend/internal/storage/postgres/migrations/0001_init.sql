CREATE TABLE users (
    telegram_id BIGINT PRIMARY KEY,
    first_name  TEXT NOT NULL,
    username    TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE expenses (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users (telegram_id),
    amount     NUMERIC(12, 2) NOT NULL CHECK (amount > 0),
    category   TEXT NOT NULL CHECK (category IN
               ('food', 'transport', 'housing', 'entertainment', 'health', 'shopping', 'other')),
    spent_at   DATE NOT NULL,
    comment    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX expenses_user_spent_idx ON expenses (user_id, spent_at DESC);
CREATE INDEX expenses_spent_idx ON expenses (spent_at DESC);
