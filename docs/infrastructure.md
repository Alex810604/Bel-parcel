# Инфраструктура проекта

## Локальное развёртывание

### Требования
- Docker 20.10+
- Docker Compose 2.0+
- psql (для подключения к БД)

### Запуск
docker-compose up -d

### Доступные сервисы
| Сервис            | Порт | URL                       |
|-------------------|------|---------------------------|
| reference-service | 8084 | http://localhost:8084     |
| operator-api      | 8090 | http://localhost:8090     |
| reference-db      | 5435 | psql -h localhost -p 5435 |

### Проверка
./check_infrastructure.sh

### Применение миграций
1. Первоначальная настройка БД:
psql -h localhost -U user -d reference_db -f services/reference-service/internal/infra/db/schema.sql

2. Применение индексов полнотекстового поиска:
psql -h localhost -U user -d reference_db -f services/reference-service/internal/infra/db/migrate_fts_indexes.sql

Важно: Индексы применяются отдельно, потому что CREATE INDEX CONCURRENTLY нельзя выполнять внутри транзакции.

## Мониторинг

Мониторинг развёртывается в Kubernetes через официальный kube‑prometheus‑stack и наш overlays‑чарт.

### Установка
- Добавить репозиторий:
  - helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
- Установить kube‑prometheus‑stack:
  - helm install kps prometheus-community/kube-prometheus-stack -n monitoring -f charts/monitoring-overlays/values-kube-prometheus-stack.yaml
- Установить overlays:
  - helm install mon-overlays charts/monitoring-overlays -n default

### Дашборды Grafana
- Reference Service: дашборд из charts/monitoring-overlays/dashboards/reference-service.json
- Подхватываются sidecar’ом Grafana по лейблу grafana_dashboard=1

### Алерты
Правила PrometheusRule находятся в charts/monitoring-overlays/templates/prometheus-rules.yaml.
Alertmanager конфиг развёрнут отдельным чартом и читает Slack webhook из Kubernetes Secret.
