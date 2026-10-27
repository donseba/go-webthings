-- Page views are counted per day and stored only as totals: no addresses,
-- visitor hashes or cookies. Days are YYYY-MM-DD in the site's timezone.
CREATE TABLE wt_stats_day (
    instance VARCHAR(64) NOT NULL,
    day      CHAR(10) NOT NULL,
    views    INTEGER NOT NULL DEFAULT 0,
    visitors INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (instance, day)
) ENGINE=InnoDB;

-- Title is the page's HTML title when it was last viewed that day.
CREATE TABLE wt_stats_page (
    instance VARCHAR(64) NOT NULL,
    day      CHAR(10) NOT NULL,
    path     VARCHAR(500) NOT NULL,
    title    VARCHAR(300) NOT NULL DEFAULT '',
    views    INTEGER NOT NULL DEFAULT 0,
    visitors INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (instance, day, path),
    INDEX wt_stats_page_path (instance, path)
) ENGINE=InnoDB;

-- Dimension is "referrer" (host), "device" (desktop, mobile, tablet) or
-- "locale" (the page's language).
CREATE TABLE wt_stats_dimension (
    instance  VARCHAR(64) NOT NULL,
    day       CHAR(10) NOT NULL,
    dimension VARCHAR(20) NOT NULL,
    name      VARCHAR(255) NOT NULL,
    views     INTEGER NOT NULL DEFAULT 0,
    visitors  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (instance, day, dimension, name)
) ENGINE=InnoDB;

-- Retention 0 keeps every day.
CREATE TABLE wt_stats_setting (
    instance       VARCHAR(64) PRIMARY KEY,
    retention_days INTEGER NOT NULL DEFAULT 0,
    updated_at     DATETIME(6) NOT NULL,
    updated_by     VARCHAR(36) NULL
) ENGINE=InnoDB;
