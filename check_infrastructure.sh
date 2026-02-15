#!/bin/bash
set -e

echo "=== Проверка инфраструктуры ==="
echo ""

echo "1. Проверка здоровья сервисов..."
SERVICES=("reference-service:8084" "operator-api:8090" "batching-service:8082" "routing-service:8081")
for svc in "${SERVICES[@]}"; do
  name="${svc%%:*}"
  port="${svc##*:}"
  if curl -s "http://localhost:${port}/healthz" > /dev/null; then
    echo " ✓ ${name} здоров"
  else
    echo " ✗ ${name} недоступен"
    exit 1
  fi
done

echo ""
echo "2. Проверка мониторинга..."
if curl -s http://localhost:9090/-/healthy > /dev/null; then
  echo " ✓ Prometheus работает"
else
  echo " ✗ Prometheus недоступен"
  exit 1
fi
if curl -s http://localhost:9093/-/healthy > /dev/null; then
  echo " ✓ Alertmanager работает"
else
  echo " ✗ Alertmanager недоступен"
  exit 1
fi
if curl -s http://localhost:3000/api/health > /dev/null; then
  echo " ✓ Grafana работает"
else
  echo " ✗ Grafana недоступна"
  exit 1
fi

echo ""
echo "3. Проверка подключения к БД..."
if psql -h localhost -p 5435 -U user -d reference_db -c "SELECT 1" > /dev/null 2>&1; then
  echo " ✓ Подключение к reference-db успешно"
else
  echo " ✗ Не удалось подключиться к reference-db"
  exit 1
fi

echo ""
echo "4. Проверка Redpanda..."
if docker-compose exec -T redpanda rpk cluster health > /dev/null 2>&1; then
  echo " ✓ Redpanda работает"
else
  echo " ✗ Redpanda недоступна"
  exit 1
fi

echo ""
echo "=== Все проверки пройдены успешно ==="
