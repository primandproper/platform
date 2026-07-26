-- +goose Up
CREATE TABLE migrate_test_users (
    id INTEGER NOT NULL PRIMARY KEY,
    name TEXT NOT NULL
);

-- +goose Down
DROP TABLE migrate_test_users;
