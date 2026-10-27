CREATE TABLE wt_users (
    id            VARCHAR(36) PRIMARY KEY,
    email         VARCHAR(254) NOT NULL UNIQUE,
    name          VARCHAR(200) NOT NULL DEFAULT '',
    role          VARCHAR(50) NOT NULL,
    status        VARCHAR(20) NOT NULL DEFAULT 'active',
    password_hash VARCHAR(200) NOT NULL,
    created_at    DATETIME(6) NOT NULL,
    created_by    VARCHAR(36) NULL,
    updated_at    DATETIME(6) NOT NULL,
    updated_by    VARCHAR(36) NULL
) ENGINE=InnoDB;

CREATE TABLE wt_sessions (
    id           VARCHAR(36) PRIMARY KEY,
    user_id      VARCHAR(36) NOT NULL,
    token_hash   VARCHAR(64) NOT NULL UNIQUE,
    expires_at   DATETIME(6) NOT NULL,
    revoked_at   DATETIME(6) NULL,
    created_at   DATETIME(6) NOT NULL,
    last_seen_at DATETIME(6) NULL,
    FOREIGN KEY (user_id) REFERENCES wt_users (id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE wt_login_attempts (
    key_hash          VARCHAR(64) PRIMARY KEY,
    failure_count     INTEGER NOT NULL DEFAULT 0,
    blocked_until     DATETIME(6) NULL,
    window_expires_at DATETIME(6) NOT NULL
) ENGINE=InnoDB;

-- One-time password reset links: only digests of their tokens are stored.
CREATE TABLE wt_password_resets (
    token_hash VARCHAR(64) PRIMARY KEY,
    user_id    VARCHAR(36) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    FOREIGN KEY (user_id) REFERENCES wt_users (id) ON DELETE CASCADE
) ENGINE=InnoDB;

-- Groups give editors their rights: one row per allowed section and action.
-- Administrators (wt_users.role = 'admin') may do everything.
CREATE TABLE wt_groups (
    id         VARCHAR(36) PRIMARY KEY,
    name       VARCHAR(100) NOT NULL UNIQUE,
    created_at DATETIME(6) NOT NULL,
    created_by VARCHAR(36) NULL,
    updated_at DATETIME(6) NOT NULL,
    updated_by VARCHAR(36) NULL
) ENGINE=InnoDB;

CREATE TABLE wt_group_members (
    group_id VARCHAR(36) NOT NULL,
    user_id  VARCHAR(36) NOT NULL,
    PRIMARY KEY (group_id, user_id),
    FOREIGN KEY (group_id) REFERENCES wt_groups (id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES wt_users (id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE wt_group_permissions (
    group_id VARCHAR(36) NOT NULL,
    section  VARCHAR(100) NOT NULL,
    action   VARCHAR(20) NOT NULL,
    PRIMARY KEY (group_id, section, action),
    FOREIGN KEY (group_id) REFERENCES wt_groups (id) ON DELETE CASCADE
) ENGINE=InnoDB;
