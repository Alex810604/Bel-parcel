# Деплой reference-service

## Требования
- Postgres 15+
- Kafka API (Redpanda 23.x) или совместимый брокер
- Go 1.21+

## Переменные окружения
| Переменная          | Описание                       | По умолчанию                 |
|---------------------|--------------------------------|------------------------------|
| DB_DSN              | Строка подключения к БД        | -                            |
| KAFKA_BROKERS       | Адреса брокеров Kafka API      | redpanda:9092                |
| REDPANDA_BROKERS    | Альтернативный ключ брокеров   | redpanda:9092                |
| KAFKA_TOPIC         | Топик для событий              | events.reference_updated     |
| KAFKA_TIMEOUT       | Таймаут операций продьюсера    | 5s                           |
| SERVER_PORT         | Порт сервиса                   | 8084                         |
| AUTH_HS256SECRET    | Секрет для подписи JWT (HS256) | -                            |
| AUTH_ISSUER         | Эмитент токена                 | bp                           |
| AUTH_AUDIENCE       | Аудитория токена               | operator                     |

## Миграция данных
1. Инициализация схемы:
   psql -h host -U user -d reference_db -f services/reference-service/internal/infra/db/schema.sql
2. Индексы полнотекстового поиска (GIN):
   psql -h host -U user -d reference_db -f services/reference-service/internal/infra/db/migrate_fts_indexes.sql
Примечание: CREATE INDEX CONCURRENTLY выполняется вне транзакции, поэтому вынесено в отдельный скрипт.
