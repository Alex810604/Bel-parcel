# Reference Service API

## Аутентификация
Все эндпоинты требуют заголовок:
Authorization: Bearer <JWT>

## Поиск справочников
GET /{type}?q={query}
- Типы: pvz, carriers, warehouses
- Пример: GET /pvz?q=minsk
- Параметры: page (целое, >=1, по умолчанию 1)
- Ответ: массив объектов справочника (пагинация по 50 записей)
- Поиск: полнотекстовый (PostgreSQL FTS, язык 'russian')

## Обновление ПВЗ
PUT /pvp/{id}
- Роли: moderator, admin
- Тело: {"is_hub": true, "reason": "Причина"}
- Ответ: 202 Accepted

## Обновление перевозчика
PUT /carriers/{id}
- Роли: admin
- Тело: {"is_active": false, "reason": "Причина"}
- Ответ: 202 Accepted
