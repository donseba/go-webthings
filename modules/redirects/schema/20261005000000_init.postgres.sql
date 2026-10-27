-- Redirects send an old path to a new address. from_path is normalised (see
-- package redirects); to_url is a path on the site or an absolute URL.
CREATE TABLE wt_redirect (
    id          UUID PRIMARY KEY,
    instance    VARCHAR(64) NOT NULL,
    from_path   VARCHAR(500) NOT NULL,
    to_url      VARCHAR(2000) NOT NULL,
    status      INTEGER NOT NULL DEFAULT 301,
    hits        BIGINT NOT NULL DEFAULT 0,
    last_hit_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL,
    created_by  UUID,
    updated_at  TIMESTAMPTZ NOT NULL,
    updated_by  UUID,
    deleted_at  TIMESTAMPTZ,
    deleted_by  UUID
);

-- Redirects in the trash keep their path, but leave it free for a new one.
CREATE UNIQUE INDEX wt_redirect_from ON wt_redirect (instance, from_path) WHERE deleted_at IS NULL;

-- Missed paths are public GET requests that ended in 404, counted so editors
-- can add redirects for them.
CREATE TABLE wt_redirect_miss (
    instance    VARCHAR(64) NOT NULL,
    path        VARCHAR(500) NOT NULL,
    hits        BIGINT NOT NULL DEFAULT 0,
    last_hit_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (instance, path)
);
