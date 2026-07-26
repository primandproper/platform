-- +goose Up
CREATE TABLE migrate_test_orders (
    id INTEGER NOT NULL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    total REAL NOT NULL
);

CREATE TABLE migrate_test_order_items (
    id INTEGER NOT NULL PRIMARY KEY,
    order_id INTEGER NOT NULL,
    widget_id INTEGER NOT NULL
);

CREATE INDEX migrate_test_order_items_by_order ON migrate_test_order_items (order_id);

-- +goose Down
DROP TABLE migrate_test_order_items;
DROP TABLE migrate_test_orders;
