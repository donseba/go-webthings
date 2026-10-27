-- Site settings as key/value pairs. scope is '' for site-wide values or a
-- language code for translated text, so new settings need no schema change.
CREATE TABLE wt_setting (
    scope      TEXT NOT NULL,
    name       TEXT NOT NULL,
    value      TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    updated_by TEXT,
    PRIMARY KEY (scope, name)
);
