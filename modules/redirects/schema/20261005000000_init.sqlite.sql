-- Redirects send an old path to a new address. from_path is normalised (see
-- package redirects); to_url is a path on the site or an absolute URL.
CREATE TABLE wt_redirect (
    id          TEXT PRIMARY KEY,
    instance    TEXT NOT NULL,
    from_path   TEXT NOT NULL,
    to_url      TEXT NOT NULL,
    status      INTEGER NOT NULL DEFAULT 301,
    hits        INTEGER NOT NULL DEFAULT 0,
    last_hit_at TIMESTAMP,
    created_at  TIMESTAMP NOT NULL,
    created_by  TEXT,
    updated_at  TIMESTAMP NOT NULL,
    updated_by  TEXT,
    deleted_at  TIMESTAMP,
    deleted_by  TEXT
);

-- Redirects in the trash keep their path, but leave it free for a new one.
CREATE UNIQUE INDEX wt_redirect_from ON wt_redirect (instance, from_path) WHERE deleted_at IS NULL;

-- Missed paths are public GET requests that ended in 404, counted so editors
-- can add redirects for them.
CREATE TABLE wt_redirect_miss (
    instance    TEXT NOT NULL,
    path        TEXT NOT NULL,
    hits        INTEGER NOT NULL DEFAULT 0,
    last_hit_at TIMESTAMP NOT NULL,
    PRIMARY KEY (instance, path)
);
