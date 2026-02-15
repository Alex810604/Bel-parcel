DB_HOST ?= localhost
DB_PORT ?= 5432
DB_USER ?= user
DB_NAME ?= reference_db

apply-schema:
	psql -h $(DB_HOST) -p $(DB_PORT) -U $(DB_USER) -d $(DB_NAME) -f services/reference-service/internal/infra/db/schema.sql

apply-indexes:
	psql -h $(DB_HOST) -p $(DB_PORT) -U $(DB_USER) -d $(DB_NAME) -f services/reference-service/internal/infra/db/migrate_fts_indexes.sql

create-topics:
	docker exec bp-kafka bash -lc "kafka-topics.sh --bootstrap-server kafka:9092 --create --if-not-exists --topic events.reference_updated --partitions 3 --replication-factor 1"
	docker exec bp-kafka bash -lc "kafka-topics.sh --bootstrap-server kafka:9092 --create --if-not-exists --topic commands.trip.reassign --partitions 3 --replication-factor 1"
	docker exec bp-kafka bash -lc "kafka-topics.sh --bootstrap-server kafka:9092 --create --if-not-exists --topic orders.created --partitions 3 --replication-factor 1"
	docker exec bp-kafka bash -lc "kafka-topics.sh --bootstrap-server kafka:9092 --create --if-not-exists --topic batches.formed --partitions 3 --replication-factor 1"
	docker exec bp-kafka bash -lc "kafka-topics.sh --bootstrap-server kafka:9092 --create --if-not-exists --topic dlq.batching --partitions 3 --replication-factor 1"
