/*
Package migrate provides the platform's standard database.Migrator: embedded
goose SQL migrations with the operational discipline consumers otherwise
hand-roll — an instance-based provider (no global goose state, so parallel
tests never race), and on Postgres a session advisory lock that serializes
concurrently booting replicas, with probe timeouts tightened so a waiting
replica notices the winner promptly instead of goose's leisurely default.

The advisory lock ID is derived from a caller-supplied lock key: deployments
sharing a database serialize on the same key, while schema-isolated parallel
tests pass distinct keys (their schema name) and migrate concurrently instead
of queueing on a global ID.

Wire it through database/config by passing the Migrator to NewDatabase with
RunMigrations enabled, or call Migrate directly with any *sql.DB.
*/
package migrate
