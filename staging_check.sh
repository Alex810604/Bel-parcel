#!/bin/bash
set -e

echo "1. Проверка здоровья сервиса"
curl -s http://localhost:8084/healthz && echo "✓ healthz OK" || exit 1

echo "2. Проверка готовности (БД)"
curl -s http://localhost:8084/readyz && echo "✓ readyz OK" || exit 1

echo "3. Поиск ПВЗ"
TOKEN=$(generate_test_jwt admin)  # Заменить на реальную генерацию токена
curl -s -H "Authorization: Bearer $TOKEN" "http://localhost:8084/pvz?q=minsk" | jq '.[0].name' && echo "✓ поиск работает" || exit 1

echo "4. Обновление ПВЗ"
curl -s -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"is_hub": true, "reason": "staging test"}' \
  http://localhost:8084/pvp/pvp-minsk-1 && echo "✓ обновление работает" || exit 1

echo "5. Проверка метрик"
curl -s http://localhost:8084/metrics | grep reference_requests_total && echo "✓ метрики доступны" || exit 1

echo "Все проверки пройдены"
