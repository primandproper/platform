-- +goose Up
CREATE TABLE migrate_test_widgets (
    id INTEGER NOT NULL PRIMARY KEY,
    label TEXT NOT NULL
);

-- +goose Down
DROP TABLE migrate_test_widgets;
