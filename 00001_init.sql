-- +goose Up
CREATE TABLE accounts (
    id SERIAL,
    slack_user_id TEXT UNIQUE,
    ethereum_address TEXT
);

CREATE TABLE balances (
    id SERIAL,
    slack_user_id TEXT UNIQUE,
    balance INTEGER
);
-- +goose Down
DROP TABLE accounts;
DROP TABLE balances;
