-- Page views are counted per day and stored only as totals: no addresses,
-- visitor hashes or cookies. Days are YYYY-MM-DD in the site's timezone.
CREATE TABLE wt_stats_day (
    instance TEXT NOT NULL,
    day      TEXT NOT NULL,
    views    INTEGER NOT NULL DEFAULT 0,
    visitors INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (instance, day)
);

-- Title is the page's HTML title when it was last viewed that day.
CREATE TABLE wt_stats_page (
    instance TEXT NOT NULL,
    day      TEXT NOT NULL,
    path     TEXT NOT NULL,
    title    TEXT NOT NULL DEFAULT '',
    views    INTEGER NOT NULL DEFAULT 0,
    visitors INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (instance, day, path)
);

CREATE INDEX wt_stats_page_path ON wt_stats_page (instance, path);

-- Dimension is "referrer" (host), "device" (desktop, mobile, tablet) or
-- "locale" (the page's language).
CREATE TABLE wt_stats_dimension (
    instance  TEXT NOT NULL,
    day       TEXT NOT NULL,
    dimension TEXT NOT NULL,
    name      TEXT NOT NULL,
    views     INTEGER NOT NULL DEFAULT 0,
    visitors  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (instance, day, dimension, name)
);

-- Retention 0 keeps every day.
CREATE TABLE wt_stats_setting (
    instance       TEXT PRIMARY KEY,
    retention_days INTEGER NOT NULL DEFAULT 0,
    updated_at     TIMESTAMP NOT NULL,
    updated_by     TEXT
);
