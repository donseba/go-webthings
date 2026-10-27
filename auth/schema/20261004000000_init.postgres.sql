CREATE TABLE wt_users (
    id            UUID PRIMARY KEY,
    email         VARCHAR(254) NOT NULL UNIQUE,
    name          VARCHAR(200) NOT NULL DEFAULT '',
    role          VARCHAR(50) NOT NULL,
    status        VARCHAR(20) NOT NULL DEFAULT 'active',
    password_hash VARCHAR(200) NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL,
    created_by    UUID,
    updated_at    TIMESTAMPTZ NOT NULL,
    updated_by    UUID
);

CREATE TABLE wt_sessions (
    id           UUID PRIMARY KEY,
    user_id      UUID NOT NULL REFERENCES wt_users (id) ON DELETE CASCADE,
    token_hash   VARCHAR(64) NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ
);

CREATE INDEX wt_sessions_user ON wt_sessions (user_id);

CREATE TABLE wt_login_attempts (
    key_hash          VARCHAR(64) PRIMARY KEY,
    failure_count     INTEGER NOT NULL DEFAULT 0,
    blocked_until     TIMESTAMPTZ,
    window_expires_at TIMESTAMPTZ NOT NULL
);

-- One-time password reset links: only digests of their tokens are stored.
CREATE TABLE wt_password_resets (
    token_hash VARCHAR(64) PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES wt_users (id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX wt_password_resets_user ON wt_password_resets (user_id);

-- Groups give editors their rights: one row per allowed section and action.
-- Administrators (wt_users.role = 'admin') may do everything.
CREATE TABLE wt_groups (
    id         UUID PRIMARY KEY,
    name       VARCHAR(100) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL,
    created_by UUID,
    updated_at TIMESTAMPTZ NOT NULL,
    updated_by UUID
);

CREATE TABLE wt_group_members (
    group_id UUID NOT NULL REFERENCES wt_groups (id) ON DELETE CASCADE,
    user_id  UUID NOT NULL REFERENCES wt_users (id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);

CREATE TABLE wt_group_permissions (
    group_id UUID NOT NULL REFERENCES wt_groups (id) ON DELETE CASCADE,
    section  VARCHAR(100) NOT NULL,
    action   VARCHAR(20) NOT NULL,
    PRIMARY KEY (group_id, section, action)
);
