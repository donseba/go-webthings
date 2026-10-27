-- Testimonials are a collection (see package content).
CREATE TABLE wt_testimonial (
    id         UUID PRIMARY KEY,
    instance   VARCHAR(64) NOT NULL,
    published  BOOLEAN NOT NULL DEFAULT FALSE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    created_by UUID,
    updated_at TIMESTAMPTZ NOT NULL,
    updated_by UUID,
    deleted_at TIMESTAMPTZ,
    deleted_by UUID,
    photo_id   UUID,
    author     VARCHAR(200) NOT NULL DEFAULT ''
);

CREATE INDEX wt_testimonial_instance ON wt_testimonial (instance, position);

CREATE TABLE wt_testimonial_translation (
    item_id      UUID NOT NULL REFERENCES wt_testimonial (id) ON DELETE CASCADE,
    locale       VARCHAR(35) NOT NULL,
    quote        TEXT NOT NULL DEFAULT '',
    author_role  VARCHAR(500) NOT NULL DEFAULT '',
    organisation VARCHAR(500) NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
