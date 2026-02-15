Title: Пояснительная записка по проекту BelParcel

Table of Contents
- 0. Суть проекта простыми словами
- 1. Общая структура проекта
- 2. Детальное описание разделов
- 3. Полное описание работы системы
- 4. Требования к окружению (environment)
- 5. FAQ (Frequently Asked Questions)
- 6. Приложения: диаграммы и схемы
 - 7. Варианты решений при поломке машины
 - 8. Перегрузка товара: как это делать и автоматизировать
 - 9. Пошаговые инструкции для каждого сервиса
 - 10. Работа с паролями
 - 11. Система доступа
 - 12. Интеграции
 - 13. Техническое руководство (коротко и по делу)

0. Суть проекта простыми словами
- Что это
  - BelParcel — система, которая организует доставку посылок со склада до пункта выдачи. Она помогает операторам, перевозчикам и сотрудникам пунктов работать согласованно, чтобы посылки приходили вовремя.
- Какие вопросы закрывает
  - Как быстро собрать посылки в удобные для перевозки партии.
  - Как назначить подходящего перевозчика на рейс и при необходимости заменить его.
  - Как видеть задержки и вовремя на них реагировать.
  - Как упорядоченно принимать партии в пункте выдачи.
  - Как вести учёт действий и изменений без путаницы.
- Как это работает внутри
  - Есть несколько отдельных служб. Каждая занимается своей частью: одна отвечает за заказы, другая собирает партии, третья помогает назначать и менять перевозчика, ещё одна фиксирует приём партий в пункте выдачи.
  - Службы обмениваются сообщениями: когда происходит важное событие (создан заказ, сформирована партия, партия принята), об этом узнают другие части системы и выполняют свои задачи.
  - Данные хранятся в базе. Службы читают и записывают ровно то, что им нужно, не мешая друг другу.
  - Для оператора есть отдельный удобный интерфейс: он видит рейсы, детали, задержки, справочники и может переназначить перевозчика.
  - Система наблюдает за собой: записывает работу, показывает графики и предупреждает, если цели по скорости и доступности начинают не выполняться.

1. Общая структура проекта
- Иерархия модулей и взаимосвязи
  - services: микросервисы на Go (го)
    - operator-api: API для операторов [operator-api/main.go](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/operator-api/cmd/operator-api/main.go)
    - order-service: управление заказами [order-service/main.go](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/order-service/cmd/order-service/main.go)
    - batching-service: формирование партий [batching-service](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/batching-service)
    - routing-service: маршрутизация [routing-service](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/routing-service)
    - reassignment-service: переназначения перевозчика [reassignment-service](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/reassignment-service)
    - pickup-point-service: операции ПВЗ [pickup-point-service/main.go](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/pickup-point-service/cmd/pickup-point-service/main.go)
  - web: фронтенд SPA (эс-пи-эй) на React (риэкт) [operator-panel](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/web/operator-panel)
  - charts: Helm (хелм) чарты для деплоя [charts/operator-api](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/charts/operator-api)
- Принципы организации кодовой базы (codebase)
  - Структура директорий: каждое приложение в отдельной папке, cmd/internal разбиение для Go (го)
  - Импорты и именование: lower-case модульные пути, без подчеркиваний; handler/service/infra
  - Система контроля версий (version control): Git (гит), ветвление по feature/hotfix, pull-request (пул-реквест)
- Диаграмма компонентов (UML Component Diagram)
  - Диаграмма иллюстрирует обмен через Kafka (кафка) и доступ к PostgreSQL (постгрес)
  - Компоненты: Services, Kafka, PostgreSQL, Grafana (графана), Prometheus (прометеус), Jaeger (яегер), Loki (локи), Kubernetes (кубернетис)
  - Mermaid:
    ```mermaid
    graph LR
      subgraph Services
        A[operator-api]
        B[order-service]
        C[batching-service]
        D[routing-service]
        E[reassignment-service]
        F[pickup-point-service]
      end
      K[(Kafka)]
      P[(PostgreSQL)]
      G[Grafana]
      M[Prometheus]
      J[Jaeger]
      L[Loki]
      A--commands-->K
      B--events-->K
      C--events-->K
      D--events-->K
      E--events-->K
      F--events-->K
      A--read-->P
      B--read/write-->P
      C--read/write-->P
      F--read/write-->P
      M-->G
      Services-->J
      Services-->L
    ```

2. Детальное описание разделов
- Назначение модулей
  - operator-api: UI-ориентированный API для оператора, просмотр рейсов, переназначение перевозчика, задержки, справочники
  - order-service: генерация и жизненный цикл заказов
  - batching-service: группировка заказов в партии
  - routing-service: расчёт маршрутов и рейсов
  - reassignment-service: обработка команд переназначения
  - pickup-point-service: приём партий в ПВЗ, фиксация статуса
- Технологический стек (tech stack)
  - Языки: Go (го) 1.24.0; TypeScript (тайпскрипт) 5.6.3
  - Библиотеки: pgx/v5 (пиг-икс), kafka-go (кафка-го), viper (вайпер), OpenTelemetry (оупентелеметри) v1.39.0, Prometheus client (прометеус клиент) v1.23.2, JWT (джей-дабл-ю-ти) v5.3.0, MUI (эм-ю-ай) 5.15.14, React (риэкт) 18.2, Vite (вайт) 5.0.12
  - Внешние сервисы: Apache Kafka (апач кафка), PostgreSQL (постгрес), Grafana (графана), Prometheus (прометеус), Jaeger (яегер), Loki (локи), Kubernetes (кубернетис), Helm (хелм)
- Схемы взаимодействия (sequence diagrams)
  - Поток: Заказ → Партия → Рейс → Доставка:
    ```mermaid
    sequenceDiagram
      participant User as Пользователь
      participant Order as order-service
      participant Batch as batching-service
      participant Route as routing-service
      participant Carrier as carrier-app
      participant PVP as pickup-point-service
      User->>Order: Создать заказ
      Order-->>Kafka: events.order_created
      Kafka-->>Batch: consume order_created
      Batch-->>Kafka: events.batch_formed
      Kafka-->>Route: consume batch_formed
      Route-->>Carrier: Назначить перевозчика
      Carrier-->>PVP: Доставить партию
      PVP-->>Kafka: events.batch_received_by_pvp
    ```
  - Переназначение перевозчика:
    ```mermaid
    sequenceDiagram
      participant Operator as operator-api
      participant Kafka as Kafka
      participant Reassign as reassignment-service
      Operator->>Kafka: commands.trip.reassign
      Kafka-->>Reassign: consume reassign
      Reassign->>Carrier: Назначить нового перевозчика
    ```
- Ключевые алгоритмы
  - Формирование партий: группировка по (склад, ПВЗ), размер/время флашинга
    - Псевдокод:
      ```
      // Событие заказов -> группировка в карту по ключу (warehouseID, pvpID)
      // Если размер >= maxSize или flushInterval истёк -> сформировать партию
      ```
  - Переназначение: публикация envelope (энвелоуп) в Kafka (кафка) с event_id, correlation_id
  - Мониторинг задержек: выборка из trips по NOW()-assigned_at > threshold
- Особенности реализации
  - Паттерны: middleware (мидлвэйр) для JWT (джей-дабл-ю-ти), producer (продьюсер) для Kafka, graceful shutdown (грейсфул шатдаун), structured logging (структурированный логгинг)
  - Нестандартные решения: двойная защита публикаций через таблицу published_events (паблишд_ивентс) от дублирования

3. Полное описание работы системы
- Инициализация
  - Загрузка конфигурации через viper (вайпер) [config.go](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/operator-api/internal/config/config.go)
  - Инициализация OTLP (о-ти-эл-пи) и трейсера [operator-api](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/operator-api/cmd/operator-api/main.go#L222-L247)
  - Настройка пула БД (постгрес), продьюсера Kafka (кафка), сервис-слоя
- Запуск HTTP (эйч-ти-ти-пи)
  - Регистрация хендлеров, healthz/readyz, /metrics (прометеус)
  - JWT (джей-дабл-ю-ти) и RBAC (эр-би-эй-си) обёртка RequireRoles [auth.go](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/operator-api/internal/auth/auth.go)
- Workflow (воркфлоу)
  - Просмотр рейсов, детализация, мониторинг задержек, поиски справочников
  - Переназначение требует роли moderator/admin (модератор/админ), оператор берётся из subject (сабджект) токена
- Error handling (эррор хэндлинг)
  - Валидные коды: 200, 202; ошибки: 400 (валидация), 401/403 (авторизация), 404 (не найдено), 500 (внутренние)
  - Логирование структурировано; ошибки перехватываются в хендлерах
- Graceful shutdown (грейсфул шатдаун)
  - Сигналы SIGTERM/SIGINT; shutdown с таймаутом 5s

4. Требования к окружению (environment)
- Минимальные/рекомендуемые
  - ОС: Linux (линукс)/Windows (виндоз) 64-bit
  - CPU: 2/4 ядра минимум; рекомендовано 8+ для продакшн
  - RAM: минимум 2 ГБ; рекомендовано 8–16 ГБ
  - Диск: минимум 5 ГБ; рекомендовано 50+ ГБ (Kafka/PostgreSQL)
- Зависимости (dependencies)
  - Go (го) 1.24.0; Node.js (ноуд-джс) 18+
  - PostgreSQL (постгрес) 14+; Kafka (кафка) 3.x
  - Prometheus (прометеус) 2.49+; Grafana (графана) 10+
  - Jaeger (яегер), Loki (локи), Kubernetes (кубернетис) 1.27+
  - Helm (хелм) 3.x
- Настройка окружения
  - Dev (дев): запуск локально, переменные окружения через .env; Vite (вайт) для SPA
  - Prod (продакшн): деплой Helm (хелм) чартов, Ingress (ингресс) TLS/mTLS, секреты БД и auth
  - Примеры значений: [values.yaml](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/charts/operator-api/values.yaml)

5. FAQ (Frequently Asked Questions)
- Установка (installation)
  - Сборка: go build ./..., npm install && npm run build
  - Деплой: helm install -f values.yaml
- Настройка (configuration)
  - JWT (джей-дабл-ю-ти): AUTH_HS256SECRET, AUTH_ISSUER, AUTH_AUDIENCE
  - Ingress (ингресс): tlsSecretName, mtls.secretName
- Использование
  - SPA (эс-пи-эй): положить access_token в localStorage, навигация по разделам
- Troubleshooting (траблшутинг)
  - 401/403: проверить токен, роли
  - 500: проверить БД/кафка, логи и метрики
  - Метрики: /metrics доступен для Prometheus
- Рекомендации
  - Переключать режим стабилизации при исчерпании error budget (эррор баджет)
  - Использовать Pyrra (пирра) для управления SLO и burn rate-алертами

6. Приложения: диаграммы и схемы
- ERD (ер-ди): основные таблицы
  ```mermaid
  classDiagram
    class trips {
      id: string
      origin_warehouse_id: string
      pickup_point_id: string
      carrier_id: string
      assigned_at: timestamp
      status: string
    }
    class trip_batches {
      trip_id: string
      batch_id: string
    }
    class batch_orders {
      batch_id: string
      order_id: string
    }
    class published_events {
      event_type: string
      correlation_id: string
      occurred_at: timestamp
    }
    trips --> trip_batches
    trip_batches --> batch_orders
  ```
- Примеры кода (code samples) с комментариями
  - Метрики HTTP (эйч-ти-ти-пи) в operator-api:
    ```go
    // Регистрация гистограммы и счётчика Prometheus
    // Маркируем метод, маршрут и код ответа; сохраняем длительность и количество
    ```
  - Метрика “delivery-time” (деливери-тайм) — пример:
    ```go
    // На завершении партии: вычисляем длительность и отмечаем on_time/late
    // deliveryDuration.WithLabelValues("on_time"|"late").Observe(seconds)
    ```
- SLO (эс-эл-о) через Pyrra (пирра)
  - delivery-time-slo.yaml и api-latency-slo.yaml: объекты ServiceLevelObjective
  - Генерация recording rules и 4 Multi Burn Rate Alerts

Примечание по алертингу Telegram (телеграм): интеграция Alertmanager (алертменеджер) с Telegram пока не настроена; требуется добавить receiver (ресивер) и route (роут) в конфигурацию Alertmanager, а также секреты для бота.

7. Варианты решений при поломке машины
- Цель
  - Снизить срыв доставки из‑за неполадок транспорта: быстро заменить перевозчика, предупредить участников и отследить последствия.
- Базовые принципы
  - Лучше заранее иметь запасной вариант, чем реагировать, когда уже поздно.
  - Чем меньше ручных действий, тем быстрее система восстанавливается.
- Вариант А: Служба аварийного реагирования
  - Что делает: получает сообщение о поломке, автоматически пытается найти замену и запускает переназначение рейса.
  - Как выбирает замену: учитывает близость, свободность машины, вместимость, время в пути, требования пункта выдачи.
  - Если замены нет: сразу сообщает оператору и предлагает варианты — разделить партию, перенести сроки, подключить партнёра.
- Вариант Б: Служба состояния транспорта
  - Что делает: собирает сведения о техсостоянии, плановом обслуживании и текущих проблемах по машинам.
  - Польза: предупреждает заранее о рисках по рейсам, предлагает перестроить план до реальной поломки.
- Вариант В: Служба резервных перевозчиков
  - Что делает: для важных направлений заранее держит список готовых подменных перевозчиков и их условия.
  - Польза: при инциденте не тратим время на поиск — есть готовые контакты и правила.
- Вариант Г: Служба перераспределения партий
  - Что делает: если рейс сорван, разделяет большую партию на несколько частей и распределяет по доступным машинам.
  - Польза: часть грузов всё равно успеет вовремя, даже если целиком вывезти не получается.
- Вариант Д: Служба уведомлений
  - Что делает: при поломке рассылает сообщения оператору, перевозчику, пункту выдачи, при необходимости — получателям.
  - Каналы: телефон, сообщения, мессенджер; формат — чётко и понятно: что случилось, что делаем, когда ждать.
- Минимальный шаг без нового сервиса
  - Добавить правило: при событии “поломка” автоматически сформировать команду на переназначение и отразить это в интерфейсе оператора.
  - Включить уведомления для ответственных людей, чтобы решение принималось быстрее.
  - Настроить отдельный отчёт по таким инцидентам: сколько было, как быстро решали, как это влияло на сроки.
 
8. Перегрузка товара: как это делать и автоматизировать
- Когда нужна перегрузка
  - Машина сломалась, рейс сорван, но рядом есть другая машина.
  - Машина перегружена, требуется часть груза перевезти другой машиной.
  - Нужно ускорить доставку: разделить большую партию на несколько рейсов.
- Как делать перегрузку по шагам
  - Подготовка: выбрать место, где можно безопасно перегрузить (склад, промежуточная точка).
  - Учёт: пересчитать коробки, сверить по списку, исключить пересорт и недостачу.
  - Сканы: отсканировать штрихкоды коробок, чтобы точно знать, что переложили.
  - Оформление: создать новый рейс или добавить к существующему; зафиксировать, что именно и куда переместили.
  - Проверки: вместимость, вес, габариты, допустимость маршрута для новой машины.
  - Уведомления: сообщить пункту выдачи и оператору новый план (время, состав).
  - Отправка: загрузить, закрепить, проверить документы и поехать.
- Что надо фиксировать в системе
  - Событие «перегрузка»: кто инициировал, где, когда, сколько коробок.
  - Перемещение единиц: из какого рейса в какой, список коробок.
  - Обновлённые сроки: когда ожидать прибытие каждой части груза.
  - Ответственные: кто принял решение и кто выполнял.
- Как автоматизировать
  - Создать отдельный раздел в интерфейсе оператора «Перегрузка»:
    - Выбор исходного рейса и машины‑приёмника.
    - Отбор коробок по сканам/списку.
    - Автоматические проверки вместимости/веса и времени в пути.
    - Кнопка «оформить перегрузку»: формирует новый рейс/обновляет старый, меняет привязку коробок.
  - События:
    - «перегрузка_инициирована» — начали процесс, промежуточные данные.
    - «перегрузка_завершена» — все коробки переложены и учтены.
    - «коробка_перемещена» — каждая единица перемещения.
  - Правила:
    - Если перегрузка частичная — разделить партию: часть уезжает сразу, остальное позже.
    - Если перегрузка полная — заменить машину и обновить рейс целиком.
  - Уведомления:
    - Оператор, пункт выдачи, при необходимости — получатели (новые сроки).
  - Отчёт:
    - Сколько перегрузок было, среднее время, влияние на сроки, типы причин.
  - Интеграция со сканерами:
    - Сканировать коробки при выемке и при укладке в новую машину, исключая ошибки.

9. Пошаговые инструкции для каждого сервиса
- operator-api (служба для оператора)
  - Назначение: показать рейсы, детали, задержки, справочники; отправить команду на смену перевозчика.
  - Основные функции: чтение из базы (рейсы, справочники), публикация команд, защита JWT, метрики /metrics.
  - Роль в системе: «фронт» для оператора, единственная точка вмешательства.
  - Технологии: Go 1.24+, pgx/v5, kafka-go, viper, JWT, Prometheus-клиент.
  - Взаимодействие: читает из БД (trip, batch, reference); публикует команду в Kafka (commands.trip.reassign); отдает JSON для SPA.
  - Шаги запуска:
    - Настроить переменные окружения (порты, DSN, Kafka, JWT).
    - Собрать: go build ./...
    - Запустить: бинарник; проверить /healthz, /readyz, /metrics.
  - Шаги работы:
    - GET /trips — список рейсов, фильтры.
    - GET /trips/{id} — детали.
    - GET /delays — рейсы с задержкой.
    - GET /references/{type}?q= — справочники.
    - POST /trips/{id}/reassign — отправка команды на смену перевозчика.
- order-service (заказы)
  - Назначение: принять и учитывать заказы, публиковать события «заказ создан», слушать события партии/доставки.
  - Основные функции: запись заказа, публикация/обработка событий, обновление статуса.
  - Роль: источник данных для формирования партий.
  - Технологии: Go, pgx/v5, kafka-go, viper, OpenTelemetry.
  - Взаимодействие: пишет в БД, публикует «orders.created», слушает «batches.formed» и события доставки.
  - Шаги: настроить DSN и Kafka; запустить; проверить готовность /readyz.
- batching-service (формирование партий)
  - Назначение: объединять заказы в партии по складу/ПВЗ с ограничениями размера/времени.
  - Основные функции: накопление, флаш по размеру/времени, запись партии, публикация «batches.formed».
  - Роль: готовит груз к погрузке в рейсы.
  - Технологии: Go, pgx/v5, kafka-go.
  - Взаимодействие: слушает «orders.created»; пишет партии; публикует «batches.formed».
  - Шаги: настроить DSN/Kafka; запустить; наблюдать метрики.
- routing-service (маршруты/рейсы)
  - Назначение: назначить перевозчика, создать рейс, рассчитывать путь.
  - Основные функции: подбор перевозчика, создание рейса, обновление статуса.
  - Роль: переводит партии из «готово» в «в пути».
  - Взаимодействие: слушает «batches.formed»; пишет рейс в БД; может публиковать события назначения.
  - Шаги: настроить доступ к справочникам и БД; запустить.
- reassignment-service (переназначение перевозчика)
  - Назначение: обработать команду смены перевозчика и внести изменения.
  - Основные функции: слушать commands.trip.reassign; подобрать нового перевозчика; обновить рейс; при необходимости уведомить.
  - Роль: спасение рейсов при проблемах.
  - Взаимодействие: слушает Kafka; пишет в БД; может публиковать события «перевозчик сменён».
  - Шаги: настроить Kafka/БД; запустить воркер; наблюдать логи и метрики.
  - Параметры окружения (пример):
    - DB_TRIPDSN, DB_REFDSN
    - KAFKA_BROKERS, KAFKA_TIMEOUT
    - KAFKA_COMMANDTOPIC=commands.trip.reassign
  - Консьюмер:
    - GroupID: reassignment-service
    - Топик берётся из KAFKA_COMMANDTOPIC
    - При получении сообщения вызывает HandleManualReassign(trip_id, new_carrier_id, reason, operator_id)
  - Graceful shutdown:
    - Отмена контекста консьюмера и воркера, закрытие reader, остановка HTTP
- pickup-point-service (пункт выдачи)
  - Назначение: принять партию, учесть факт доставки.
  - Основные функции: POST /batches/{id}/receive; запись события «batch_received_by_pvp».
  - Роль: фиксирует завершение пути партии.
  - Взаимодействие: пишет в БД; публикует событие в Kafka.
  - Шаги: настроить БД/Kafka; запустить; проверка /readyz.
- web/operator-panel (панель оператора)
  - Назначение: удобный интерфейс для просмотра и действий.
  - Основные функции: таблицы рейсов, фильтры, детали, задержки, справочники; отправка переназначения.
  - Роль: лицо системы для оператора.
  - Технологии: React, TypeScript, MUI, Vite.
  - Взаимодействие: HTTP JSON с operator-api; хранит токен в браузере.
  - Шаги: npm install; npm run build/dev; настроить VITE_API_BASE_URL.

10. Работа с паролями
- Как есть сейчас
  - Система использует вход по токену (пропуск), а не хранит пароли внутри. Токены дают внешний поставщик или внутренний выпускатель (в упрощённом режиме).
- Вариант А: внешний поставщик (например, готовая система входа)
  - Мы не держим пароли у себя.
  - Пользователь входит в сторонний сервис; он выдаёт токен.
  - Мы проверяем подпись и срок действия токена и читаем роль.
- Вариант Б: внутренний вход (если потребуется)
  - Хранение: только «след» пароля (хэш), а не сам пароль.
  - Метод: стойкие хэши (например, bcrypt), с «солью» для каждого пользователя; длина пароля от 12 символов.
  - Правила: сложность (буквы/цифры), запрет простых слов, срок жизни пароля, история паролей.
  - Смена: старый пароль + новый; хэш обновляется.
  - Сброс доступа: код одноразовый с коротким сроком, отправка на почту/телефон; хэшируем код; ставим ограничение по попыткам.
  - Никогда не хранить пароли в открытом виде; не передавать пароли по незащищённым каналам.

11. Система доступа
- Уровни и роли
  - Пользователь: просмотр данных (рейсы, задержки, справочники).
  - Модератор: как пользователь + операции (переназначение).
  - Администратор: всё выше + управление доступом и настройками.
- Вход и разрешения
  - Вход по токену: проверяем подпись, срок, издателя; читаем роль.
  - Проверка ролей на каждом запросе: «только просмотр» или «разрешены изменения».
- Выдача и отзыв прав
  - Выдача: администратор назначает роль.
  - Отзыв: смена роли или прекращение доступа; принудительный отзыв токена — смена ключа подписи или запрет конкретного токена (чёрный список).
- Логирование действий
  - Записывать: кто, когда, что сделал (оператор, время, действие, результат).
  - Хранить логи достаточный срок; разбирать спорные случаи.

12. Интеграции
- Точки входа (пример для operator-api)
  - GET /trips — список рейсов (фильтры: статус, пункт выдачи, перевозчик, дата).
  - GET /trips/{id} — детали (рейс, партии, заказы).
  - GET /delays — рейсы с задержкой.
  - GET /references/{type}?q= — справочники (склады, ПВЗ, перевозчики).
  - POST /trips/{id}/reassign — переназначить перевозчика (в теле: новый перевозчик, причина).
- Форматы данных
  - JSON для запросов/ответов; простые поля: строки, числа, дата/время.
  - События в сообщениях: «чехол» с полями (идентификатор события, тип, время, связка с объектом, данные).
- Протоколы взаимодействия
  - Службы общаются через очередь сообщений: публикуют и слушают события/команды.
  - Обмен с панелью оператора — обычные веб‑запросы.
- Ошибки и исключения
  - Плохие данные — возвращаем «ошибка запроса».
  - Нет доступа — «вход не выполнен» или «недостаточно прав».
  - Не найдено — «нет такого».
  - Внутренняя ошибка — «не получилось внутри»; записываем в журнал.

13. Техническое руководство (коротко и по делу)
- Структура проекта
  - services/ — исходники микросервисов (каждый сервис: cmd/ — вход, internal/ — логика)
    - operator-api: [cmd](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/operator-api/cmd/operator-api/main.go), [auth](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/operator-api/internal/auth/auth.go), [service](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/operator-api/internal/app/service.go), [config](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/operator-api/internal/config/config.go)
    - order-service: [cmd](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/order-service/cmd/order-service/main.go)
    - batching-service: [infra/kafka](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/batching-service/internal/infra/kafka/kafka.go)
    - routing-service: исходники маршрутизации
    - reassignment-service: [worker](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/reassignment-service/internal/worker/worker.go)
    - pickup-point-service: [cmd](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/services/pickup-point-service/cmd/pickup-point-service/main.go)
  - web/operator-panel — SPA: [src/api.ts](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/web/operator-panel/src/api.ts), [pages](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/web/operator-panel/src/pages/Trips.tsx)
  - charts/ — Helm-чарты: [operator-api](file:///c:/Users/loban_h3g0vgo/GolandProjects/Bel-parcel/charts/operator-api)
- Запуск проекта (локально)
  - Зависимости
    - Go 1.24+, Node.js 18+, PostgreSQL 14+, Kafka API (Redpanda 23.x)
    - Prometheus, Grafana, Jaeger, Loki (необязательно для dev)
  - Настройки окружения (пример для operator-api)
    - SERVER_PORT=8090
    - DB_TRIPDSN=postgres://user:pass@localhost:5432/trip_db?sslmode=disable
    - DB_BATCHDSN=postgres://user:pass@localhost:5432/batch_db?sslmode=disable
    - DB_REFERENCEDSN=postgres://user:pass@localhost:5432/reference_db?sslmode=disable
    - KAFKA_BROKERS=localhost:9092
    - KAFKA_TIMEOUT=5s
    - KAFKA_COMMANDTOPIC=commands.trip.reassign
    - AUTH_HS256SECRET=<секрет>
    - AUTH_ISSUER= (опционально)
    - AUTH_AUDIENCE= (опционально)
  - Команды запуска (Windows PowerShell)
    - Operator API:
      ```powershell
      cd services/operator-api
      go mod tidy
      go run ./cmd/operator-api
      ```
    - Pickup Point Service:
      ```powershell
      cd services/pickup-point-service
      go run ./cmd/pickup-point-service
      ```
    - Order/Batching/Reassignment аналогично: перейти в каталог и запустить main.go
    - SPA:
      ```powershell
      cd web/operator-panel
      npm install
      $env:VITE_API_BASE_URL="http://localhost:8090"
      npm run dev
      ```
  - Требования:
    - PostgreSQL и Redpanda/Kafka должны быть запущены; DSN и брокеры — доступны.
    - Для защищённых маршрутов нужен действительный JWT-токен с ролью.
- Запуск в Kubernetes (Helm)
  - Пример: operator-api
    ```bash
    helm install operator-api charts/operator-api -f charts/operator-api/values.yaml
    ```
  - Обязательные секреты: DSN’ы и auth_hs256secret (см. templates/secret.yaml)
- Функционал и взаимодействие (конкретно)
  - operator-api
    - GET /trips — список рейсов, фильтры по статусу/ПВЗ/перевозчику/дате.
    - GET /trips/{id} — детали рейса: партии и заказы.
    - GET /delays — рейсы с задержкой относительно порога.
    - GET /references/{type}?q= — поиск по справочникам.
    - POST /trips/{id}/reassign — команда переназначения перевозчика.
    - /metrics — метрики для наблюдения.
  - События
    - commands.trip.reassign — команда на смену перевозчика; ключ — trip_id; данные: новый перевозчик, причина, оператор.
    - events.batch_received_by_pvp — приём партии в ПВЗ.
    - trip.reassigned — переназначение выполнено; ключ — batch_id; данные: trip_id, old_trip_id, batch_id, carrier_id, assigned_at, reason, operator_id, manual_action
- Входные/выходные данные
  - Вход: HTTP-запросы от панели оператора; события из Kafka для воркеров.
  - Выход: JSON-ответы; новые события в Kafka; записи в базах.
- Примеры использования (реальные кейсы)
  - Переназначение перевозчика:
    ```bash
    curl -H "Authorization: Bearer <token>" \
      -H "Content-Type: application/json" \
      -X POST http://localhost:8090/trips/TRIP123/reassign \
      -d '{"new_carrier_id":"CARRIER42","reason":"поломка"}'
    ```
  - Просмотр задержек за последние 2 часа:
    ```bash
    curl -H "Authorization: Bearer <token>" \
      "http://localhost:8090/delays?hours=2"
    ```
  - Поиск пункта выдачи по «Минск»:
    ```bash
    curl -H "Authorization: Bearer <token>" \
      "http://localhost:8090/references/pvz?q=Минск"
    ```
- Диаграммы
  - Компоненты и последовательности — см. разделы 1 и 2 (Mermaid-графы уже включены).
