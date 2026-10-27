CREATE TABLE wt_users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active',
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMP NOT NULL,
    created_by    TEXT,
    updated_at    TIMESTAMP NOT NULL,
    updated_by    TEXT
);

CREATE TABLE wt_sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES wt_users (id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    expires_at   TIMESTAMP NOT NULL,
    revoked_at   TIMESTAMP,
    created_at   TIMESTAMP NOT NULL,
    last_seen_at TIMESTAMP
);

CREATE INDEX wt_sessions_user ON wt_sessions (user_id);

CREATE TABLE wt_login_attempts (
    key_hash          TEXT PRIMARY KEY,
    failure_count     INTEGER NOT NULL DEFAULT 0,
    blocked_until     TIMESTAMP,
    window_expires_at TIMESTAMP NOT NULL
);

-- One-time password reset links: only digests of their tokens are stored.
CREATE TABLE wt_password_resets (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES wt_users (id) ON DELETE CASCADE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX wt_password_resets_user ON wt_password_resets (user_id);

-- Groups give editors their rights: one row per allowed section and action.
-- Administrators (wt_users.role = 'admin') may do everything.
CREATE TABLE wt_groups (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL,
    created_by TEXT,
    updated_at TIMESTAMP NOT NULL,
    updated_by TEXT
);

CREATE TABLE wt_group_members (
    group_id TEXT NOT NULL REFERENCES wt_groups (id) ON DELETE CASCADE,
    user_id  TEXT NOT NULL REFERENCES wt_users (id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);

CREATE TABLE wt_group_permissions (
    group_id TEXT NOT NULL REFERENCES wt_groups (id) ON DELETE CASCADE,
    section  TEXT NOT NULL,
    action   TEXT NOT NULL,
    PRIMARY KEY (group_id, section, action)
);
