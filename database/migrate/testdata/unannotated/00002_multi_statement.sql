-- Two statements in one file, so the splitter has real work to do.
CREATE TABLE migrate_bare_widgets (
    id INTEGER NOT NULL PRIMARY KEY,
    label TEXT NOT NULL
);

CREATE INDEX migrate_bare_widgets_by_label ON migrate_bare_widgets (label);
