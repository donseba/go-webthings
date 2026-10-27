-- Site settings as key/value pairs. scope is '' for site-wide values or a
-- language code for translated text, so new settings need no schema change.
CREATE TABLE wt_setting (
    scope      VARCHAR(35) NOT NULL,
    name       VARCHAR(100) NOT NULL,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    updated_by UUID,
    PRIMARY KEY (scope, name)
);
