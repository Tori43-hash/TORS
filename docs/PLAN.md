# TORS — план архитектуры модульного бота продажи подписок

> Telegram-бот для продажи VPN-подписок. Первая панель — **Remnawave**, следующая — **3x-UI**.
> Язык — **Go**. Архитектура — «микроядро + модули» по образцу **Caddy v2**.

---

## 0. Кратко

- **Ядро** (`package tors`, корень репозитория) ничего не знает ни о Telegram, ни о VPN, ни о деньгах.
  Оно умеет только: регистрировать модули, загружать их из конфига, управлять их жизненным циклом,
  связывать их друг с другом (DI через `Context`) и передавать события. Зависимости ядра — только stdlib.
- **Всё остальное — модули**, которые регистрируются в `init()` и подключаются импортом при сборке (как в Caddy/xcaddy):
  Telegram, база данных, очередь задач, панели (Remnawave, 3x-UI), платёжные шлюзы, тарифы, подписки,
  триал, рефералка, промокоды, рассылки, админка.
- Модули делятся на **приложения** (apps, верхний уровень: `telegram`, `panels`, `billing`, …) и
  **гостевые модули** в пространствах имён своих хостов (`panels.providers.remnawave`, `billing.gateways.telegram_stars`,
  `telegram.handlers.shop`, …).
- Панели скрыты за контрактом `panels.Provider` с **декларативным идемпотентным** `EnsureAccount(spec)` и
  **опциональными возможностями** (capability-интерфейсы). Remnawave и 3x-UI — два гостевых модуля одного хоста.
- Бизнес-поток «оплата → выдача подписки» надёжен: подтверждение платежа и постановка события в очередь
  происходят в одной транзакции (transactional outbox), обработчики идемпотентны.
- MVP = фазы 0–5 (ядро → инфраструктура → Telegram → домен + Remnawave → биллинг). Редактор экранов на холсте сделан
  первым (v0.1, `web/ui-editor/`), Mini App — фаза 7,
  3x-UI — фаза 8; обе не требуют изменений ни в ядре, ни в домене.
- **Принятые решения** (§20): оплата — только Telegram Stars; у пользователя может быть несколько подписок;
  внутренний баланс и админка — необязательные модули; тарифы — в конфиге; языки — ru (по умолчанию) и en;
  импорт пользователей из панели не нужен; Mini App планируется; запускается одна копия бота.
- **Стек** (§21, версии сверены на 28.09.2026): Go 1.27, PostgreSQL 18, pgx + sqlc + goose, River, telego (Bot API 10.3),
  OpenTelemetry; Mini App — TypeScript + React + Vite + tma.js. **Границы расширяемости** — §22.

## Статус реализации (28.09.2026)

Сделано: ядро, инфраструктура, Telegram, домен с Remnawave, оплата звёздами, пробный период, поддержка, маркет модулей
и конструктор бота (фазы 0–5, часть фазы 6). Ниже — где реализация разошлась с планом и почему; разделы плана
оставлены как были, решения ниже главнее.

| План | Как сделано | Почему |
|---|---|---|
| Очередь задач на River (§6.2) | Своя очередь в модуле `jobs`: таблица задач, `FOR UPDATE SKIP LOCKED`, повторы с backoff, периодические задачи, надёжные события — задача на каждого подписчика в той же транзакции | Нужны ~300 строк, а не зависимость в версии 0.x; снаружи модуля API тот же |
| Миграции на goose, запросы на sqlc (§6.1) | Свой мигратор в `database` (файлы `migrations/*.sql` модуля, `SET LOCAL search_path` на схему модуля), SQL в коде с явной схемой (`billing.orders`) | Меньше инструментов; схема на модуль соблюдается одинаково |
| Контракт UI-модулей и `telegram.handlers.*` (§9.2) | Нейтральный реестр `ui`: модули объявляют данные, действия с исходами, условия, события и стартовые экраны; `telegram` их только показывает | Доменные модули не импортируют Telegram; тот же реестр пригодится Mini App |
| Формат тем в `modules/telegram/theme` (§9.5) | Пакет `theme/` в корне | Экраны объявляют и доменные модули — им не место в пакете Telegram |
| Рендереры `rich` и `classic` (§9.5) | Только `classic`: `sendMessage`/`sendPhoto` с HTML-разметкой и клавиатурой | Rich messages не проверить без живого Telegram; формат тем от рендерера не зависит, `rich` добавится модулем |
| `i18n` на go-i18n (§6.5) | Тексты по языкам лежат прямо в экранах; языки задаёт `users` | Все тексты бота — экраны; отдельный каталог переводов не нужен |
| `metrics` (§6.6), `referral`, `promo`, `wallet`, `admin` | Не сделаны | Следующие шаги; ядро и домен для них менять не нужно |
| `tors ui`, манифест из бинаря (§9.6) | Маркет: `tors describe` и `tors market-index` пишут описания модулей (настройки, требования, экраны, данные, действия) в `registry/index.json`; конструктор собирает манифест из выбранных модулей | Конструктор работает без запущенного бота и начинает с пустого холста |
| Кнопки хранят параметры в callback_data | Параметры кнопок хранятся на сервере (`kv`, ключ — чат и сообщение), в callback_data только номер кнопки | Параметры нельзя подделать; нет лимита в 64 байта |

Стек фактически: Go 1.27.1, PostgreSQL 16–18, pgx v5.11, telego v1.12 (Bot API 10.3), remnawave-api-go v3.4.4,
go.yaml.in/yaml/v3; конструктор — React 19, Vite 8, TypeScript 7, React Flow 12, шрифт Inter.

---

## 1. Цели и не-цели

**Цели**

1. Ядро — «максимальный полезный минимум»: стабильный, маленький, покрытый тестами, редко меняющийся.
2. Любая функция бота включается/выключается конфигом, заменяется или дописывается сторонним модулем без правки ядра.
3. Новая панель (3x-UI, позже — Marzban и др.) = новый гостевой модуль, без изменений домена и UI.
4. Новый платёжный шлюз = новый гостевой модуль.
5. Надёжность денег: ни одна оплата не теряется и не засчитывается дважды.
6. Telegram Mini App (запланирован) — вторая «витрина» над тем же доменом; домен не зависит от чата Telegram.

**Не-цели (на старте)**

- Динамическая загрузка плагинов в рантайме (`plugin`, go-plugin) — см. ADR-001.
- Мультитенантность (несколько независимых магазинов в одном процессе).
- Горизонтальное масштабирование: работает одна копия процесса. Архитектура не мешает запустить несколько копий позже.
- Импорт существующих пользователей из панели.
- Платёжные шлюзы, кроме Telegram Stars (добавляются потом отдельными модулями без изменения домена).
- Собственный DSL конфигурации (аналог Caddyfile) — достаточно JSON + YAML-адаптера.

---

## 2. Что берём у Caddy и что адаптируем

| Caddy v2 | TORS | Комментарий |
|---|---|---|
| `caddy.RegisterModule(m)` в `init()` | `tors.RegisterModule(m)` в `init()` | Компиляционная модульность |
| `CaddyModule() ModuleInfo{ID, New}` | `TorsModule() ModuleInfo{ID, New}` | |
| ID с пространствами имён: `http.handlers.file_server` | `panels.providers.remnawave`, `billing.gateways.stars` | Namespace = «гнездо» хоста |
| `Provisioner` / `Validator` / `CleanerUpper` | те же | Опциональные интерфейсы жизненного цикла |
| `caddy.App` (`Start/Stop`) | `tors.App` (`Start/Stop`) | Модули верхнего уровня |
| `ctx.LoadModule(s, "Field")` + тег `caddy:"namespace=… inline_key=…"` | `ctx.LoadModule(s, "Field")` + тег `tors:"namespace=… inline_key=…"` | Хост грузит гостей из своего JSON |
| `ctx.App("tls")` — ленивая загрузка и связывание | `ctx.App("panels")` + `tors.AppAs[panels.Service](ctx, "panels")` | + generics; зависимость от интерфейса, а не от типа — модуль можно заменить |
| Нативный JSON + адаптеры (Caddyfile) | Нативный JSON + адаптер YAML | |
| `{env.X}` плейсхолдеры | `{env.X}`, `{file./path}` | Секреты не в конфиге |
| xcaddy (кастомные сборки) | свой `main.go` с импортами → позже `xtors` | |
| Admin API `/load`, атомарная перезагрузка с откатом | `SIGHUP` / `tors reload` (фаза 9) | Жизненный цикл проектируется под reload сразу |
| `caddyevents` — отдельное приложение | **Шина событий в ядре** + гарантированная доставка в модуле `jobs` | В боте события — основной способ интеграции фич, поэтому шина входит в «полезный минимум» (≈200 строк, только stdlib) |

---

## 3. Ядро

### 3.1. Состав ядра

| Компонент | Файл | Зачем в ядре |
|---|---|---|
| Реестр модулей | `modules.go` | Без него нет модульности |
| Жизненный цикл | `lifecycle.go`, `tors.go` | Единый порядок New → Unmarshal → Provision → Validate → Start → Stop → Cleanup, откат при ошибке |
| Context / DI | `context.go` | Загрузка гостевых модулей, получение других приложений, логгер модуля |
| Конфигурация | `config.go`, `adapters.go`, `replacer.go` | Формат, адаптеры, плейсхолдеры `{env.*}`, общие типы `Duration("30d")`, `Size("100GiB")` |
| Шина событий | `events.go` | Слабая связанность модулей (in-memory, синхронная) |
| Логирование | `logging.go` | `log/slog`, логгер с полем `module=<id>` |
| CLI-каркас | `cmd/` | `run`, `validate`, `adapt`, `schema`, `list-modules`, `version` + регистрация команд модулями. Отдельный пакет: ему можно иметь зависимости, корневому пакету `tors` — нет |

**Чего в ядре нет** (и не будет): Telegram, БД, KV, HTTP-сервер, очередь задач, i18n, деньги, пользователи,
панели, тарифы, планировщик. Всё это — модули. Проверка в CI: `go list -deps .` для корневого пакета
не должен содержать ничего, кроме stdlib.

### 3.2. Модель модуля

```go
// modules.go
type ModuleID string // "panels.providers.remnawave"

func (id ModuleID) Namespace() string // "panels.providers"
func (id ModuleID) Name() string      // "remnawave"

type ModuleInfo struct {
	ID  ModuleID
	New func() Module // новый пустой экземпляр; без побочных эффектов
}

type Module interface {
	TorsModule() ModuleInfo
}

func RegisterModule(m Module)                  // из init(); паника при дубликате/невалидном ID
func GetModule(id string) (ModuleInfo, error)
func GetModules(namespace string) []ModuleInfo // для list-modules и сообщений об ошибках
```

```go
// lifecycle.go — все интерфейсы опциональны
type Provisioner  interface{ Provision(Context) error } // зависимости, ресурсы, загрузка гостей
type Validator    interface{ Validate() error }         // только проверки, без побочных эффектов
type CleanerUpper interface{ Cleanup() error }          // освободить то, что взято в Provision

// App — модуль верхнего уровня (пустой namespace): telegram, panels, billing…
type App interface {
	Module
	Start() error // запуск фоновой активности; не блокирует
	Stop() error  // остановка; идемпотентна
}

// HealthChecker — опционально; агрегируется в /readyz модулем http.
type HealthChecker interface{ Health(context.Context) error }
```

**Пространства имён** (namespace = хост, который грузит такие модули):

| Namespace | Хост | Примеры |
|---|---|---|
| `""` (apps) | ядро | `database`, `jobs`, `kv`, `http`, `i18n`, `users`, `catalog`, `panels`, `billing`, `subscriptions`, `telegram`, `trial`, `referral`, … |
| `config.adapters` | ядро | `yaml` |
| `kv.stores` | `kv` | `memory`, `postgres` (позже при необходимости — `redis`) |
| `catalog.sources` | `catalog` | `config`, `db` |
| `panels.providers` | `panels` | `remnawave`, `3xui`, `fake` |
| `billing.gateways` | `billing` | `telegram_stars`, `balance` (из модуля `wallet`), `fake`; позже — любые другие |
| `billing.pricing` | `billing` | `promo`, `referral_discount` |
| `telegram.transports` | `telegram` | `polling`, `webhook` |
| `telegram.middleware` | `telegram` | `recover`, `logging`, `ratelimit`, `users`, `i18n`, `ban`, `require_channel` |
| `telegram.handlers` | `telegram` | `start`, `main_menu`, `shop`, `my_subscriptions`, `trial`, `referral`, `promo`, `wallet`, `support`, `language`, `admin` |
| `webapp.endpoints` | `webapp` | API и экраны Mini App: `shop`, `subscriptions`, `referral`, `wallet`, … |

> Имя пакета Go не может начинаться с цифры, поэтому модуль `panels.providers.3xui` живёт в пакете `xui`.

### 3.3. Жизненный цикл

```mermaid
stateDiagram-v2
    [*] --> New: ModuleInfo.New()
    New --> Unmarshal: json.Unmarshal(raw)
    Unmarshal --> Provision: зависимости, гости, ресурсы
    Provision --> Validate
    Validate --> Start: только apps, после provision ВСЕХ apps
    Start --> Stop: shutdown / reload
    Stop --> Cleanup
    Provision --> Cleanup: ошибка → откат
    Validate --> Cleanup: ошибка → откат
    Cleanup --> [*]
```

`tors.Run(cfg)`:

1. Адаптировать конфиг в JSON (если не JSON), подставить `{env.*}`/`{file.*}`.
2. Создать корневой `Context`.
3. Для каждого приложения из `apps` вызвать `ctx.App(name)`: `New` → `Unmarshal` → `Provision` → `Validate`.
   Внутри `Provision` модуль может запросить другие приложения (`ctx.App`) — они провижинятся лениво,
   циклы детектируются и дают понятную ошибку. **Порядок завершения Provision = топологический порядок зависимостей.**
4. Любая ошибка → `Cleanup` всего, что уже провижинено, в обратном порядке; процесс (или старая конфигурация при reload) не затронут.
5. `Start()` всех приложений в топологическом порядке; ошибка → `Stop` уже запущенных + `Cleanup`.
6. Остановка: `Stop()` в обратном порядке, отмена `Context` → `Cleanup`.

**Контракт для авторов модулей:** после `Provision` модуль обязан быть готов обслуживать вызовы
(другие модули могут звать его методы, HTTP-маршруты уже смонтированы). `Start` — только запуск фоновых
процессов (поллинг, воркеры, cron). Никаких глобальных переменных состояния — иначе не будет hot reload.

**Hot reload (фаза 9):** новая конфигурация провижинится целиком параллельно со старой; при успехе
старая останавливается, новая стартует; при ошибке — новая откатывается, старая продолжает работать.
Для дорогих разделяемых ресурсов (пул БД) — `tors.UsagePool` по аналогии с Caddy.

### 3.4. Context и загрузка модулей

```go
type Context struct {
	context.Context
	// скрыто: текущий конфиг, стек загружаемых модулей, cleanup-функции
}

func (ctx Context) LoadModule(structPtr any, field string) (any, error)
func (ctx Context) LoadModuleByID(id string, raw jsontext.Value) (any, error)
func (ctx Context) App(name string) (any, error)             // ленивый Provision + детект циклов
func (ctx Context) AppIfConfigured(name string) (any, error) // (nil, nil), если приложения нет в конфиге
func (ctx Context) Logger() *slog.Logger                      // уже с полем module=<id>
func (ctx Context) Events() *Events
func (ctx Context) OnCancel(f func())

func AppAs[T any](ctx Context, name string) (T, error)       // типобезопасная обёртка; T — интерфейс-контракт (panels.Service)
```

Хост описывает поле с гостями тегом, ядро по типу поля выбирает форму загрузки:

| Тип поля | Смысл | JSON |
|---|---|---|
| `jsontext.Value` + `inline_key` | один модуль | `"transport": {"transport": "polling"}` |
| `[]jsontext.Value` + `inline_key` | упорядоченный список | `"middleware": [{"middleware": "recover"}, …]` |
| `map[string]jsontext.Value` + `inline_key` | именованные экземпляры | `"providers": {"rw-main": {"provider": "remnawave", …}}` |
| `tors.ModuleMap` (без `inline_key`) | ключ = имя модуля | `"apps": {"telegram": {…}}` |

```go
type App struct {
	ProvidersRaw map[string]jsontext.Value `json:"providers" tors:"namespace=panels.providers inline_key=provider"`
	providers    map[string]Provider
}

func (a *App) Provision(ctx tors.Context) error {
	mods, err := ctx.LoadModule(a, "ProvidersRaw") // map[string]any
	if err != nil {
		return err
	}
	for name, m := range mods.(map[string]any) {
		a.providers[name] = m.(Provider)
	}
	return nil
}
```

### 3.5. Конфигурация

- **Нативный формат — JSON** (строгий, его и разбирают модули). Разбор — `encoding/json/v2` из stdlib
  (в Go 1.27 включён по умолчанию): неизвестное поле в конфиге — ошибка старта, а не молчаливо проигнорированная опечатка;
  «сырой» JSON гостевых модулей — `jsontext.Value`. Человекочитаемый формат — **YAML через адаптер**
  (`config.adapters.yaml`, отдельный модуль). `tors adapt` показывает итоговый JSON.
- **Плейсхолдеры** `{env.BOT_TOKEN}`, `{file./run/secrets/rw_token}` подставляются ядром в строковые значения
  до разбора — секреты не хранятся в конфиге.
- **Общие типы** в ядре: `tors.Duration` (понимает `30d`, `12h`), `tors.Size` (`100GiB`).
- **JSON Schema**: каждый модуль описывает схему своего конфига (генерируется из структуры); `tors schema`
  (генерация — в пакете `cmd`, корневой пакет ядра остаётся на stdlib) собирает общую схему — редактор подсказывает поля и ловит ошибки в YAML ещё до запуска.
- **Разделение**: статический конфиг (инфраструктура, токены, какие модули включены, тарифы на старте) —
  файл; оперативные данные (пользователи, заказы, промокоды, позже — тарифы из админки) — БД модулей.

Верхний уровень:

```json
{
  "logging": { "level": "info", "format": "json" },
  "apps": { "database": {…}, "telegram": {…}, "panels": {…}, … }
}
```

### 3.6. Шина событий

Шина событий — механизм «объявлений»: модуль сообщает «произошло X», а все модули, подписанные на X, реагируют.
Отправитель не знает, кто его слушает. Пример: `billing` объявляет «заказ оплачен» — `subscriptions` продлевает подписку,
`referral` начисляет бонус пригласившему, `notifications` пишет пользователю. Новая реакция — новый модуль с подпиской;
`billing` при этом не меняется.

```go
type Event interface{ EventName() string } // "billing.order.paid"

func On[E Event](bus *Events, h func(context.Context, E) error) (off func())
func (b *Events) OnPattern(pattern string, h func(context.Context, Envelope) error) (off func()) // "billing.*"
func Emit(ctx context.Context, bus *Events, e Event) error
```

- Синхронная in-memory доставка в порядке подписки; паника обработчика перехватывается; ошибка логируется.
- Обработчик может вернуть `tors.ErrAbort` — `Emit` вернёт её эмиттеру (для «before»-событий, например антифрод).
- **Событие — это факт, который уже произошёл.** Шина ядра не гарантирует доставку. Гарантированная
  доставка (at-least-once, ретраи, переживает рестарт) — в модуле `jobs` (см. §6.2), тот же типизированный API.
- **Защита от подделки.** `ctx.Events()` возвращает шину, привязанную к модулю, поэтому ядро знает, кто публикует.
  Событие `X.*` может опубликовать только модуль с ID `X`: чужой модуль не выпустит `billing.order.paid`, а гость
  `billing.gateways.foo` публикует только `billing.gateways.foo.*`. То же правило действует для durable-событий в `jobs`.
- **Проверка у источника.** Обработчики, которые выдают что-то ценное, не доверяют событию вслепую:
  `subscriptions` перед выдачей перепроверяет у `billing`, что заказ действительно оплачен.
- **Без секретов.** Подписаться может любой модуль, поэтому в событиях только ID и факты — никаких токенов и ключей.

### 3.7. CLI

`tors run | validate | adapt | schema | list-modules [--namespace …] | ui (graph, docs, manifest, export, import, preview) | version | reload (фаза 9)`.
Модули могут регистрировать свои подкоманды (`cmd.RegisterCommand`): `tors db migrate`,
`tors panels ping`, `tors catalog check` (проверить тарифы и их размещение на панелях).
`tors version` печатает версии всех вкомпилированных модулей из `debug.ReadBuildInfo`.

---

## 4. Карта модулей

### 4.1. Слои

```mermaid
flowchart TB
    subgraph PRES["Представление"]
        TG["telegram + transports / middleware / handlers"]
        WEB["webapp: Telegram Mini App (фаза 7)"]
    end
    subgraph FEAT["Фичи"]
        TRIAL[trial]
        REF[referral]
        PROMO[promo]
        NOTIF[notifications]
        BC[broadcast]
        ADM[admin]
    end
    subgraph DOM["Домен"]
        USERS[users]
        CAT[catalog]
        SUBS[subscriptions]
        BILL[billing]
        PAN[panels]
    end
    subgraph INT["Интеграции (гостевые модули)"]
        RW[panels.providers.remnawave]
        XUI[panels.providers.3xui]
        GW["billing.gateways.*"]
    end
    subgraph INFRA["Инфраструктура"]
        DB[database]
        JOBS[jobs]
        KV[kv]
        HTTP[http]
        I18N[i18n]
        MET[metrics]
    end
    CORE["Ядро tors: реестр · жизненный цикл · config · Context/DI · события · логи"]

    PRES --> FEAT --> DOM
    PRES --> DOM
    INT --> DOM
    DOM --> INFRA
    FEAT --> INFRA
    PRES --> INFRA
    INFRA --> CORE
```

### 4.2. Модули

| Модуль (ID) | Слой | Назначение |
|---|---|---|
| `database` | инфра | Пул pgx, транзакция в `context`, миграции по модулям |
| `jobs` | инфра | Очередь задач на Postgres (River): ретраи, cron, durable-события |
| `kv` + `kv.stores.*` | инфра | Хранилище «ключ → значение» для короткоживущих данных: шаг диалога, лимиты, токены кнопок (см. §6.3) |
| `http` | инфра | Общий HTTP-сервер: вебхуки Telegram и панелей, Mini App, `/healthz`, `/readyz`, `/metrics` |
| `i18n` | инфра | Переводы, переопределение текстов из конфига/каталога |
| `metrics` | инфра | Метрики и трейсы на OpenTelemetry, `/metrics` для Prometheus |
| `users` | домен | Пользователи и их идентичности (telegram, позже web), язык, блокировки |
| `catalog` + `catalog.sources.*` | домен | Тарифы (из конфига, позже из БД) |
| `panels` + `panels.providers.*` | домен | Контракт панелей, реестр экземпляров, нормализованные события, опрос |
| `billing` + `billing.gateways.*` + `billing.pricing.*` | домен | Заказы, платежи, подтверждение, конвейер цены |
| `subscriptions` | домен | Подписки (несколько на пользователя), `Grant`/продление, сверка с панелью, напоминания |
| `trial`, `referral`, `promo` | фичи | Каждая — домен + опциональный UI-модуль `telegram.handlers.*` (позже и `webapp.endpoints.*`) |
| `wallet` | фичи (опц.) | Внутренний баланс в Stars; даёт шлюз `billing.gateways.balance` |
| `notifications`, `broadcast` | фичи | Уведомления по событиям, рассылки с учётом лимитов Telegram |
| `admin` | фичи/UI (опц.) | Админ-меню в боте; другие модули добавляют в него свои экраны |
| `telegram` + гости | UI | Транспорт, роутер, middleware, меню, сцены, отправка |
| `webapp` + гости | UI | (фаза 7) Telegram Mini App: JSON API + статика, авторизация по `initData` |
| `subaggregator` | фичи | (фаза 8) Единая ссылка подписки для тарифа на нескольких панелях |

### 4.3. Три способа взаимодействия модулей

| Способ | Когда | Пример |
|---|---|---|
| **Сервис приложения** — `tors.AppAs[billing.Service](ctx, "billing")` и вызов метода | Нужен ответ/результат прямо сейчас, связь «использует» | `shop` вызывает `billing.CreateOrder` |
| **Гостевой модуль** — хост грузит из своего конфига | Точка расширения с порядком/конфигом, выбираемая пользователем | `billing.pricing.promo`, `telegram.middleware.ratelimit` |
| **Событие** — `Emit` / durable через `jobs` | Реакция на факт, эмиттер не должен знать о подписчиках | `billing.order.paid` → `subscriptions`, `referral`, `notifications` |

### 4.4. Правила зависимостей (проверяются в CI через `depguard`/`go-arch-lint`)

1. Ядро → только stdlib. Никогда не импортирует `modules/…`.
2. `pkg/…` — чистые библиотеки (деньги, ретраи, форматирование); не импортируют ядро и модули.
3. **Хост никогда не импортирует своих гостей** (как в Caddy): `panels` не знает про `remnawave`.
4. Доменные модули не импортируют `telegram` и конкретные провайдеры/шлюзы.
5. Гость импортирует пакет своего хоста (контракт) + свой SDK; с остальным — через `Context` и события.
6. UI-модули (`telegram.handlers.*`) импортируют домен; домен про UI не знает.
7. Модуль не читает чужие таблицы напрямую — только через сервисы или явно экспортированные представления (§14.1).
   Внешние ключи между модулями — только на `users.users`; прочие ссылки — ID без FK.
8. Зависимость от другого приложения — только через его интерфейс-контракт (`billing.Service`), не через конкретный тип:
   тогда любой стандартный модуль можно заменить своей реализацией (§22).

---

## 5. Структура репозитория

```
tors/
├── go.mod                       # module github.com/tori43-hash/tors  (go 1.27)
├── tors.go                      # Run / Stop / Reload
├── modules.go                   # ModuleID, ModuleInfo, реестр
├── lifecycle.go                 # Provisioner, Validator, CleanerUpper, App, HealthChecker
├── context.go                   # Context, LoadModule, App, AppAs[T]
├── config.go                    # Config, ModuleMap, Duration, Size
├── adapters.go                  # ConfigAdapter + реестр (JSON встроен)
├── replacer.go                  # {env.X}, {file.path}
├── events.go                    # шина событий
├── logging.go                   # slog
├── internal/                    # приватные хелперы ядра
├── cmd/
│   ├── main.go                  # torscmd.Main(), команды, RegisterCommand
│   └── tors/main.go             # стандартный дистрибутив
├── pkg/
│   ├── money/                   # Money{Amount int64 (минорные единицы), Currency}
│   ├── retry/
│   └── tgtext/                  # экранирование, шаблоны HTML для Telegram
├── modules/
│   ├── standard/imports.go      # стандартный набор модулей
│   ├── yamladapter/             # config.adapters.yaml
│   ├── database/                # app: database
│   ├── jobs/                    # app: jobs
│   ├── kv/                      # app: kv  + memory/ postgres/
│   ├── httpserver/              # app: http
│   ├── i18n/                    # app: i18n
│   ├── metrics/                 # app: metrics
│   ├── users/                   # app: users
│   ├── catalog/                 # app: catalog + configsource/ dbsource/
│   ├── panels/                  # app: panels (контракт, watcher, события)
│   │   ├── panelstest/          # общий контрактный тест-сьют провайдеров
│   │   ├── remnawave/           # panels.providers.remnawave
│   │   ├── xui/                 # panels.providers.3xui
│   │   └── fake/                # panels.providers.fake (dev/тесты)
│   ├── billing/                 # app: billing
│   │   ├── billingtest/         # контрактный тест-сьют шлюзов
│   │   ├── stars/ fake/         # billing.gateways.telegram_stars, .fake
│   ├── subscriptions/           # app: subscriptions
│   │   └── tgui/                # telegram.handlers.my_subscriptions
│   ├── telegram/                # app: telegram (роутер, меню, сцены, отправка)
│   │   ├── polling/ webhook/    # telegram.transports.*
│   │   ├── mw/                  # telegram.middleware.*
│   │   └── start/ mainmenu/ language/ support/
│   ├── shop/                    # telegram.handlers.shop (витрина: catalog + billing)
│   ├── trial/      (+ tgui/)
│   ├── referral/   (+ tgui/)    # + billing.pricing.referral_discount
│   ├── promo/      (+ tgui/)    # + billing.pricing.promo
│   ├── wallet/     (+ tgui/)    # внутренний баланс (опц.) + billing.gateways.balance
│   ├── notifications/
│   ├── broadcast/  (+ tgui/)
│   ├── admin/                   # telegram.handlers.admin (опц.)
│   ├── webapp/                  # app: webapp — Mini App (фаза 7)
│   └── subaggregator/           # фаза 8
├── web/
│   └── ui-editor/               # редактор экранов (§9.6), фаза 7
├── configs/examples/            # minimal.yaml, remnawave.yaml, full.yaml
├── deploy/                      # Dockerfile (distroless), docker-compose.yml
└── docs/
    ├── PLAN.md
    ├── adr/
    └── modules.md               # как писать модули
```

**Фича = один каталог**: доменная часть (приложение) + `tgui/` с UI-модулем бота. Включаются в конфиге независимо —
в фазе 7 рядом появится `webui/` для Mini App, а домен фичи не изменится.

---

## 6. Инфраструктурные модули

### 6.1. `database`
- PostgreSQL через `pgx/v5`; запросы — `sqlc` (на модуль свои `queries/`). Запросы параметризованные, поэтому
  SQL-инъекции исключены на уровне генератора.
- **Своя схема на модуль**: `billing.orders`, `subscriptions.bindings`, `users.users`. Модуль получает не общий пул,
  а свой дескриптор `db.For(ctx)`, у которого `search_path` — схема модуля. Об изоляции — §14.1.
- **Миграции по модулям**: каждый модуль встраивает `migrations/*.sql` через `embed.FS` и в своём `Provision`
  вызывает `db.Migrate(ctx, fs)`; `goose` (Provider API) с таблицей версий в схеме модуля.
  Порядок гарантируется зависимостями: модуль сначала берёт `ctx.App("users")`, потом мигрирует.
- **Транзакции**: внутри модуля — `db.InTx(ctx, func(ctx context.Context) error {…})`. Общих транзакций между модулями нет,
  кроме одной: постановки задачи или durable-события в `jobs` внутри транзакции модуля (outbox).
  Всё остальное между модулями — через сервисы и события.

### 6.2. `jobs`
- Очередь на Postgres — **River** (`riverqueue/river`): транзакционная постановка, ретраи с backoff,
  уникальные задачи, периодические задачи (cron) с лидер-выбором.
- Модули регистрируют воркеры в `Provision`, `jobs.Start` запускает клиента.
- **Durable-события**: `jobs.Publish(ctx, e)` внутри транзакции → на каждого durable-подписчика ставится
  своя задача (fan-out на стороне публикации) → изолированные ретраи. Подписка: `jobs.Subscribe[E](app, "subscriptions.on_order_paid", h)`.
  Имя подписки стабильно (это kind задачи). Обработчики обязаны быть идемпотентными.

### 6.3. `kv`
KV (key-value) — хранилище «ключ → значение» со сроком жизни записи (TTL), как словарь, который переживает рестарт.
Для важных данных (пользователи, подписки, платежи) есть таблицы модулей; в KV лежат мелкие временные данные:

| Ключ (пример) | Значение | TTL | Зачем |
|---|---|---|---|
| `scene:user:42` | `promo.enter` | 10 мин | Пользователь нажал «Ввести промокод» — следующее сообщение считается кодом |
| `rl:user:42` | `3` | 1 с | Счётчик нажатий для защиты от спама (rate limit) |
| `cb:x7Kp` | `{"plan":"month","sub":17}` | 1 ч | Данные кнопки, не влезающие в лимит Telegram 64 байта |

Интерфейс `Get/Set(ttl)/Delete/Incr/Lock`. Реализации: `memory` (dev, теряется при рестарте), `postgres` (по умолчанию —
отдельная таблица в той же БД, Redis не нужен). `redis` понадобится, только если запускать несколько копий бота.

### 6.4. `http`
`net/http` + `ServeMux` (паттерны Go 1.22+), graceful shutdown, `public_url` для построения адресов вебхуков.
Нужен с MVP: вебхуки Telegram и Remnawave, позже — Mini App.
Модули монтируют маршруты в своём `Provision`: `h.Mount("POST /hooks/remnawave", handler)`; конфликт путей — ошибка старта.
Встроенные `/healthz`, `/readyz` (опрос `HealthChecker` у приложений), `/metrics` (если включён `metrics`).

### 6.5. `i18n`
`go-i18n`. Каждый модуль встраивает `locales/{ru,en}.yaml`, ключи с префиксом модуля (`shop.choose_plan`).
Переопределение любых текстов — каталог `overrides_dir` в конфиге (продавцы всегда хотят свои тексты).
Язык пользователя — из `language_code`, с возможностью смены (`telegram.handlers.language`).

### 6.6. `metrics`
OpenTelemetry (метрики и трейсы) с Prometheus-экспортером на `/metrics`, OTLP — опционально.
Модули получают его через `ctx.AppIfConfigured("metrics")` — метрики не обязательны.

---

## 7. Панели: Remnawave сейчас, 3x-UI потом

Ключевой раздел: именно здесь решается, будет ли 3x-UI «ещё одним модулем» или переписыванием.

### 7.1. Чем панели отличаются

> Целевая версия — Remnawave 3.x (контракт `@remnawave/backend-contract` 3.4.x на сентябрь 2026). Детали API сверить при реализации:
> Remnawave активно развивается, у 3x-UI много версий и форков.

| Аспект | Remnawave | 3x-UI | Решение в контракте |
|---|---|---|---|
| Топология | Центральная панель + много нод | Одна панель = один сервер | `Provider` = экземпляр панели; тариф может ссылаться на несколько экземпляров |
| Сущность | Глобальный User | Client внутри конкретного inbound | 3x-UI-провайдер создаёт по клиенту на inbound с общим `subId` |
| Доступ к серверам | Internal Squads | Список inbound ID | `Placement` — непрозрачный JSON, валидирует провайдер |
| Уникальный ключ | `username` (+ uuid/shortUuid) | `email` (уникален в панели) | Детерминированный ключ из нашего ID → идемпотентность |
| Срок | `expireAt` | `expiryTime` (мс) | `ExpiresAt time.Time` |
| Трафик | `trafficLimitBytes` + стратегия сброса (NO_RESET/DAY/WEEK/MONTH/MONTH_ROLLING) | `totalGB` на клиента **в каждом inbound**, `reset` | `TrafficLimit{Bytes, Reset}`; для 3x-UI режим `shared` — общий лимит контролирует бот |
| Устройства | `hwidDeviceLimit` (HWID) | `limitIp` (одновременные IP) | `DeviceLimit` + флаг возможности; семантика в `Info` |
| Ссылка подписки | `subscriptionUrl` в ответе API | `subId` + внешний адрес sub-сервиса (в конфиге провайдера) | `Account.SubscriptionURL` |
| Аутентификация | Bearer API-токен | Логин/пароль → cookie-сессия, web base path | Внутреннее дело провайдера |
| События | Вебхуки с HMAC-подписью | Нет | Push-провайдер шлёт события сам; для остальных `panels` опрашивает |
| Перевыпуск ссылки | Есть (revoke) | Эмуляция: новый client id + `subId` | `CredentialRevoker` |

### 7.2. Контракт

```go
package panels

// Provider — подключение к одному экземпляру панели (гостевой модуль panels.providers.*).
type Provider interface {
	Info(ctx context.Context) (Info, error)          // тип, версия панели, возможности
	ValidatePlacement(raw jsontext.Value) error     // проверка сквадов/inbounds тарифа на старте

	// EnsureAccount декларативно и идемпотентно приводит аккаунт в панели к spec:
	// ищет по spec.Key (детерминированный username/email), создаёт или обновляет.
	EnsureAccount(ctx context.Context, spec AccountSpec) (Account, error)
	GetAccount(ctx context.Context, ref AccountRef) (Account, error)
	DeleteAccount(ctx context.Context, ref AccountRef) error
}

type AccountSpec struct {
	Key         string          // стабильный ключ ("s42") → username/email в панели
	TelegramID  int64
	Enabled     bool
	ExpiresAt   time.Time
	Traffic     TrafficLimit    // Bytes=0 — безлимит; Reset: none|day|week|month|month_rolling
	DeviceLimit int             // 0 — без ограничения
	Placement   jsontext.Value // сквады / inbounds — понимает только провайдер
	Note        string
}

type Account struct {
	Ref             AccountRef // {Provider, ExternalID}
	Status          Status     // active | disabled | limited | expired
	ExpiresAt       time.Time
	UsedTraffic     int64
	SubscriptionURL string
	Raw             jsontext.Value // исходный ответ — для админки/отладки
}

// Опциональные возможности: проверяются type assertion, UI скрывает недоступное.
type TrafficResetter   interface{ ResetTraffic(ctx context.Context, ref AccountRef) error }
type CredentialRevoker interface{ RevokeCredentials(ctx context.Context, ref AccountRef) (Account, error) }
type DeviceManager     interface {
	ListDevices(ctx context.Context, ref AccountRef) ([]Device, error)
	RemoveDevice(ctx context.Context, ref AccountRef, id string) error
}
type TargetLister  interface{ ListTargets(ctx context.Context) ([]Target, error) }                     // сквады / inbounds
type AccountLister interface{ Accounts(ctx context.Context) iter.Seq2[Account, error] }          // сверка, опрос; пагинация внутри итератора
```

Почему `EnsureAccount`, а не `Create/Update`: вызов безопасно повторять (ретраи задач, сверка), нет ветвления
«создан ли уже» в домене. Если создание прошло, а мы упали до сохранения внешнего ID, повтор найдёт аккаунт
по детерминированному ключу и «усыновит» его.

### 7.3. Placement в тарифе

```yaml
placements:
  - provider: rw-main        # имя экземпляра из apps.panels.providers
    squads: ["9f1c…"]        # поле понимает только remnawave
  - provider: xui-de
    inbounds: [1, 3]         # поле понимает только 3xui
    traffic_mode: shared
```

`catalog` при старте вызывает `panels.ValidatePlacement` для каждого тарифа — ошибка конфигурации ловится до запуска.

### 7.4. Нормализованные события панелей

`panels.account.expired`, `panels.account.traffic_exceeded`, `panels.account.first_connected`,
`panels.account.disabled`, `panels.node.down` / `panels.node.up`.

- **Remnawave** (push): провайдер монтирует вебхук в `http`, проверяет HMAC-подпись, переводит события панели в нормализованные.
- **3x-UI** (нет push): `panels` запускает периодическую задачу в `jobs`, опрашивает `AccountLister`, по diff генерирует те же события.

Подписчики (уведомления, подписки) не знают, откуда событие.

### 7.5. `panels.providers.remnawave`
- Клиент: `github.com/Jolymmiles/remnawave-api-go/v3` — MIT, сгенерирован `ogen` из официальной спеки; v3.4.4 от 21.09.2026
  соответствует Remnawave 3.4. SDK **обёрнут** в провайдер (anti-corruption layer): его типы не выходят за пределы
  `modules/panels/remnawave/internal/rwapi`, поэтому изменения API панели и SDK не протекают в домен.
  - Версия SDK фиксируется в `go.mod`/`go.sum`. Обновление — осознанно: просмотр диффа и прогон контрактных тестов
    на Remnawave соответствующей версии в Docker.
  - Риск: автор SDK ведёт конкурирующий бот (`remnawave-telegram-shop`), и сроки поддержки новых версий панели зависят от него.
    Если SDK начнёт отставать или его изменения не устроят — форк (MIT) или собственная генерация `ogen` из спеки.
    Для своей генерации придётся повторить его шаг исправления спеки: `nullable` без типа, дубли схем, эндпоинты подписки
    без описания ответа. Переход затрагивает только `internal/rwapi` модуля.
- Вебхуки панели (`user.expired`, `user.limited`, `user.first_connected`, `user.not_connected`, `user.bandwidth_usage_threshold_reached`, …)
  переводятся в нормализованные события §7.4.
- При `Provision` — запрос версии панели, предупреждение/ошибка, если версия вне матрицы поддержки.
- Конфиг: `url`, `token`, `headers` (для панели за reverse-proxy), `webhook: {path, secret}`, `username_template`, `timeout`.
- Возможности: все (`TrafficResetter`, `CredentialRevoker`, `DeviceManager` (HWID), `TargetLister`, `AccountLister`), push-события.

### 7.6. `panels.providers.3xui` (фаза 8)
- Сессия: логин → cookie jar, автоматический релогин при 401/редиректе; поддержка web base path.
- `EnsureAccount`: для каждого inbound из placement — клиент с email `{key}_i{inboundID}`, общий `subId`,
  учётные данные по протоколу inbound (uuid для VLESS/VMess, пароль для Trojan/SS), `flow` из конфига.
- `SubscriptionURL` = `sub_base_url` из конфига + `subId`.
- `traffic_mode`: `per_inbound` (лимит на каждого клиента) или `shared` (бот суммирует трафик при опросе и отключает всех клиентов при превышении).
- Матрица поддерживаемых версий; контрактные тесты на docker-образе 3x-UI.

### 7.7. Несколько панелей в одном тарифе
- Remnawave-only: не нужно — одна ссылка покрывает все ноды.
- Тариф на нескольких экземплярах: у подписки несколько `bindings`. MVP-вариант — показать несколько ссылок;
  полноценный — модуль `subaggregator` (фаза 8) отдаёт единую ссылку `https://bot.example.com/sub/{token}`,
  собирая и объединяя конфиги провайдеров.

### 7.8. Сверка (reconciliation)
**Источник истины**: бот — для коммерческого состояния (тариф, срок, лимиты, включён ли); панель — для рантайма
(использованный трафик, устройства, онлайн). Периодическая задача `subscriptions.reconcile` вызывает
`EnsureAccount` для изменившихся/давно не сверенных подписок и чинит дрейф (например, админ поменял срок руками в панели);
«сироты» в панели только репортятся, не удаляются.

---

## 8. Домен

### 8.1. Модель данных (схема БД = модуль)

| Модуль | Таблицы |
|---|---|
| `users` | `users.users(id, created_at, lang, status, bot_blocked_at)`, `users.identities(user_id, kind, external_id, username, …)` |
| `catalog` | `catalog.plans` (только при `source: db`) |
| `billing` | `billing.orders(id, user_id, items jsonb, amount, currency, status, expires_at, idempotency_key)`, `billing.payments(id, order_id, gateway, external_id, amount, currency, status, raw, UNIQUE(gateway, external_id))` |
| `subscriptions` | `subscriptions.subscriptions(id, user_id, label, plan_id, plan_snapshot, status, starts_at, expires_at, traffic_limit, device_limit, version)`, `subscriptions.bindings(subscription_id, provider, external_id, synced_at, sync_error)`, `subscriptions.grants(subscription_id, source, idempotency_key UNIQUE, period, created_at)`, `subscriptions.credits(user_id, period, source, idempotency_key UNIQUE, applied_to, applied_at)` |
| `wallet` | `wallet.ledger` (проводки в `XTR`, баланс = сумма) |
| `referral` | `referral.links`, `referral.rewards` |
| `promo` | `promo.codes`, `promo.redemptions` |
| `jobs` | таблицы River в схеме `jobs` |

- Деньги — `int64` в минорных единицах + ISO-код. В MVP единственная валюта — `XTR` (Telegram Stars, целые),
  но тип `Money` всё равно хранит валюту: новый шлюз с рублями или крипто не потребует миграций.
- В заказе хранится **снимок тарифа** (цена, период, лимиты) — изменение тарифа не влияет на оплаченное.

### 8.2. Несколько подписок у пользователя

- Подписка — самостоятельная сущность: свой тариф, срок, лимиты, **свой аккаунт в панели** и своя ссылка.
  Ключ аккаунта в панели строится от ID подписки (`s{subscriptionID}`), а не от ID пользователя.
- У подписки есть имя (`label`): пользователь может назвать её «Телефон» или «Роутер»; по умолчанию — «Подписка #N».
- Покупка: «Новая подписка» или «Продлить …». Если подписок несколько — пользователь выбирает, если одна — продлевается она.
  Позиция заказа несёт `target` — ID продлеваемой подписки или пусто для новой.
- Лимит `max_per_user` в конфиге (`0` — без ограничения).
- Бонусные дни без явной цели (реферальная награда, промокод на дни) копятся как **неприменённые** (`subscriptions.credits`);
  пользователь применяет их к выбранной подписке, а при единственной подписке они применяются автоматически.
- Триал — один на пользователя, а не на подписку.
- Напоминания и уведомления — по каждой подписке отдельно, с её именем.

### 8.3. `subscriptions`: единая точка выдачи

```go
// Grant — единственный способ добавить время подписке: оплата, триал, реферальный бонус, промокод, подарок админа.
func (a *App) Grant(ctx context.Context, req GrantRequest) (Subscription, error)

type GrantRequest struct {
	UserID         int64
	SubscriptionID int64         // 0 — создать новую подписку
	PlanID         string        // тариф: лимиты и размещение на панелях
	Period         tors.Duration // явный период для бонусов/подарков; иначе — из тарифа
	Source         string        // "order" | "trial" | "referral" | "promo" | "admin"
	IdempotencyKey string        // "order:123", "trial:user:42" — повтор не продлевает дважды
}
```

- Активна → `expires_at += period`; истекла → `now + period`; сброс трафика при продлении — опция тарифа.
- Статусы: `pending` (ещё не выдана в панели) → `active` → `expired` → (продление) `active`; `suspended` (админ).
- Периодические задачи: `expiring` (напоминания за N часов из конфига), `expire` (перевод статуса), `reconcile`.

### 8.4. `billing`: деньги без знания о VPN

`billing` оперирует абстрактными позициями заказа `Item{Kind, Ref, Target, Qty, Price, Snapshot}`.
`Kind = "subscriptions.plan"` исполняет модуль `subscriptions`, `Kind = "wallet.topup"` — модуль `wallet`.
Биллинг не знает, что продаёт VPN.

```go
package billing

type Gateway interface {
	Currencies() []money.Currency
	CreatePayment(ctx context.Context, req PaymentRequest) (PaymentAction, error)
}

// PaymentAction — что показать пользователю; UI рендерит по типу.
type PaymentAction interface{ isPaymentAction() }
type TelegramInvoice struct{ Title, Description, Payload, Currency string; Prices []LabeledPrice } // Stars
type Completed       struct{}                                              // оплата с баланса (wallet)
type RedirectURL     struct{ URL string }                                  // задел для будущих внешних шлюзов

// Опциональные возможности:
type PreCheckValidator interface{ Validate(ctx context.Context, orderID int64, amount money.Money) error }
type Refunder          interface{ Refund(ctx context.Context, p Payment, amount money.Money) error }

// Шлюз сообщает о подтверждённой оплате (для Stars — из апдейта successful_payment):
//   billingApp.Confirm(ctx, Confirmation{Gateway, ExternalID, OrderID, Amount, Raw})
// Confirm сверяет сумму и валюту с заказом и идемпотентен по (gateway, external_id).
```

**Конвейер цены** — гостевые модули `billing.pricing.*` в порядке из конфига:

```go
type PriceModifier interface{ Apply(ctx context.Context, q *Quote) error } // промокод, реферальная скидка, акции
```

**Telegram Stars — единственный шлюз MVP** (плюс `fake` для разработки и `balance`, если включён `wallet`):

- Валюта `XTR`, цены тарифов в конфиге указываются в звёздах.
- `CreatePayment` возвращает `TelegramInvoice`, payload инвойса — ID заказа.
- В чате UI отправляет инвойс через `sendInvoice`; в Mini App — получает ссылку `createInvoiceLink` и открывает
  `Telegram.WebApp.openInvoice`. Шлюз один, способ показа выбирает UI.
- `pre_checkout_query` (ответить нужно за 10 секунд): billing проверяет, что заказ не истёк, не оплачен и сумма совпадает.
- `successful_payment` → `billing.Confirm`, `external_id = telegram_payment_charge_id`.
- Возврат — `refundStarPayment` (возможность `Refunder`), доступен из админки.
- Внешние вебхуки платёжек не нужны: оплата подтверждается обычным апдейтом бота.
- Шлюз зависит от приложения `telegram` — интеграционным модулям это разрешено; домен `billing` о Telegram не знает.
- Возможное развитие — подписки Stars с автосписанием (в Bot API только период 30 дней), см. §20.

### 8.5. Сквозной сценарий покупки (Stars)

```mermaid
sequenceDiagram
    autonumber
    actor U as Пользователь
    participant T as Telegram
    participant TG as telegram.handlers.shop
    participant B as billing
    participant ST as billing.gateways.telegram_stars
    participant J as jobs
    participant S as subscriptions
    participant P as panels → remnawave
    participant N as notifications

    U->>TG: «Купить» → новая или продлить «Телефон» → тариф
    TG->>B: CreateOrder(user, item{plan, target})
    B->>B: конвейер цены (промо, скидки)
    B->>ST: CreatePayment(order)
    ST-->>TG: TelegramInvoice
    TG-->>U: инвойс в чате (sendInvoice)
    U->>T: «Оплатить»
    T->>ST: pre_checkout_query
    ST->>B: Validate(order, amount)
    ST-->>T: answerPreCheckoutQuery(ok) — до 10 с
    T->>ST: successful_payment(charge_id)
    ST->>B: Confirm(stars, charge_id, amount)
    Note over B,J: одна транзакция: payment=succeeded, order=paid,<br/>durable-событие billing.order.paid
    J->>S: subscriptions.on_order_paid (ретраи до успеха)
    S->>S: Grant(target, key="order:ID") — идемпотентно
    S->>P: EnsureAccount(spec)
    P-->>S: Account{SubscriptionURL}
    S->>J: durable subscriptions.granted
    J->>N: notifications.on_granted
    N-->>U: «Подписка «Телефон» активна до …» + ссылка и QR
```

**Гарантии:** повторный `successful_payment` — no-op (уникальность `(gateway, external_id)`); недоступна панель — задача
ретраится, подписка в статусе `pending`, пользователь видит «активируем…»; падение процесса между шагами —
задача переживает рестарт; поддельное событие «заказ оплачен» ничего не выдаст — `subscriptions` перепроверяет
заказ у `billing`.

### 8.6. Каталог событий

| Событие | Публикует | Durable | Подписчики |
|---|---|---|---|
| `billing.order.creating` | billing | нет, синхронное «before» | антифрод, лимиты — могут отменить заказ через `ErrAbort` |
| `subscriptions.granting` | subscriptions | нет, синхронное «before» | ограничения выдачи — могут отменить через `ErrAbort` |
| `users.registered` | users | да | referral, notifications (админу) |
| `users.bot_blocked` | telegram | нет | users |
| `billing.order.paid` | billing | да | subscriptions, wallet, referral, notifications |
| `billing.payment.failed` | billing | нет | notifications |
| `subscriptions.granted` / `.expiring` / `.expired` | subscriptions | да | notifications, trial |
| `panels.account.traffic_exceeded` | panels | да | notifications |
| `panels.node.down` / `.up` | panels | нет | notifications (админу) |

---

## 9. Telegram-слой

### 9.1. Приложение `telegram`
- Библиотека — `telego` (полное покрытие Bot API, polling и webhook); изолирована в `telegram` и его гостях — см. ADR-007.
- Транспорт — гостевой модуль: `telegram.transports.polling` (dev, простые установки) или `telegram.transports.webhook` (монтируется в `http`).
- Middleware — упорядоченный список гостей: `recover`, `logging`, `ratelimit`, `users` (upsert пользователя, атрибуция `/start`),
  `i18n`, `ban`, `require_channel` (подписка на канал).
- Исходящие сообщения — через общий `Sender` с учётом лимитов Telegram (≈30 сообщений/с глобально, ≈1/с в чат),
  обработкой 429 (`retry_after`) и 403 (бот заблокирован → событие `users.bot_blocked`).

### 9.2. Контракт для UI-модулей

```go
package telegram

type Handler    interface{ Register(r Router) error }                              // telegram.handlers.*
type Middleware interface{ Wrap(next HandlerFunc) HandlerFunc }                    // telegram.middleware.*
type Transport  interface{ Run(ctx context.Context, sink UpdateSink) error }       // telegram.transports.*

type HandlerFunc func(c *Ctx) error

type Router interface {
	Command(name string, h HandlerFunc)
	Callback(prefix string, h HandlerFunc)     // префикс уникален, конфликт — ошибка старта
	Text(f TextFilter, h HandlerFunc)
	StartPayload(prefix string, h HandlerFunc) // /start ref_123 → "ref_" (referral), "promo_" (promo)
	On(f UpdateFilter, h HandlerFunc)          // pre_checkout_query, successful_payment, …
	Scene(name string, s Scene)                // многошаговые диалоги, состояние — в kv

	// UI как данные (§9.5): модуль даёт данные и действия, тема — экраны, кнопки и переходы
	Data(name string, p DataProvider)             // "subscriptions.list": параметры, поля для шаблонов, чтение
	Action(name string, a ActionSpec)             // "trial.start": параметры, исходы, экраны по умолчанию, обработчик
	Condition(name string, pred func(*Ctx) bool)  // "trial.available" — для visible_if / disabled_if
	DefaultTheme(fsys fs.FS)                      // экраны и фрагменты по умолчанию; тема пользователя их переопределяет
}
```

- **Callback data**: `<prefix>:<action>[:args]`, лимит 64 байта проверяет роутер; длинные данные — токен в `kv`.
- **Композиция экранов**: стандартная тема собирается из фрагментов модулей — `referral` добавляет «Пригласить друга»
  в главное меню, не трогая другие модули. Своя тема получает полный контроль над экранами и переходами (§9.5).
- **Сцены**: ввод промокода, поиск пользователя в админке — активная сцена получает текст раньше общих обработчиков.

### 9.3. Стандартные UI-модули
- `start`, `main_menu`, `language` (ru по умолчанию, en), `support`.
- `shop` — витрина: «Новая подписка» или «Продлить …» с выбором подписки → тариф → инвойс Stars.
- `my_subscriptions` — список подписок с именами; карточка подписки: ссылка, QR, остаток срока и трафика,
  продление, переименование, применение бонусных дней, перевыпуск ссылки и управление устройствами (если провайдер умеет).
- `trial`, `referral`, `promo`, `wallet` (баланс и пополнение, если модуль включён).
- `admin` (необязательный): статистика, карточка пользователя со всеми подписками, выдача дней, возврат Stars, рассылка, здоровье панелей.

### 9.4. Telegram Mini App (фаза 7)

Mini App — вторая «витрина» над тем же доменом, по устройству она зеркальна приложению `telegram`:

- Приложение `webapp` монтирует в `http` JSON API (`/app/api/…`) и отдаёт статику фронтенда (или фронтенд хостится отдельно).
- Авторизация — проверка `initData` Telegram WebApp (HMAC от токена бота) → пользователь из `users` по telegram-идентичности.
  Это тот же пользователь, что и в чате: подписки, баланс и рефералы общие.
- Эндпоинты — гостевые модули `webapp.endpoints.*`, фичи добавляют их так же, как кнопки в меню бота
  (`subscriptions/webui`, `referral/webui`, …). Вызывают те же сервисы `catalog`, `billing`, `subscriptions`, что и хендлеры бота.
- Оплата: шлюз Stars возвращает тот же `TelegramInvoice`, `webapp` превращает его в ссылку `createInvoiceLink`
  для `Telegram.WebApp.openInvoice`. Подтверждение приходит обычным `successful_payment` — поток после оплаты тот же.
- Фронтенд — отдельный проект (стек в §21), Go-части он не касается.

### 9.5. UI как данные: экраны, кнопки и переходы

Принцип — разделение ролей: **модули** дают данные (что показать) и действия (что сделать), а **тема** — набор YAML-файлов —
полностью задаёт представление и навигацию: какие экраны есть, что на них, где стоит каждая кнопка и куда она ведёт.
Это касается и экранов, которые вы создаёте сами, без единой строки кода.

| Что | Кто задаёт | Пример |
|---|---|---|
| Данные (только чтение) | Модуль | `subscriptions.list`, `subscriptions.get`, `catalog.plans`, `referral.stats` |
| Действия с именованными исходами | Модуль | `trial.start` → исходы `ok`, `already_used`, `need_channel` |
| Экраны, тексты, фото, кнопки, их расположение и переходы | Тема | `main_menu`, `sub_card`, `guide_ios` |

> Формат реализован в `theme/` (типы, проверки, шаблоны) — это источник истины; большой пример —
> `theme/testdata/theme.json`. В JSON экраны лежат в `screens` под своими ID, а `commands`, `events`,
> `routes` и `fragments` — на верхнем уровне той же темы. YAML ниже — то же самое, записанное короче.

Модули поставляют тему по умолчанию, поэтому бот работает сразу после установки. Стандартная тема собирается
из фрагментов модулей: например, `referral` добавляет свою кнопку в главное меню. `tors ui export` выгружает её целиком
в файлы. Экран с тем же ID в вашей теме полностью заменяет стандартный — вместе с раскладкой и переходами.

```
ui/
├── screens/        # по файлу на экран (или несколько экранов в одном файле)
├── fragments.yaml  # повторяющиеся куски: ряд «Назад», шапка, подвал
├── routes.yaml     # куда ведут исходы действий, события и команды
└── assets/         # картинки
```

#### Экран

```yaml
# ui/screens/main_menu.yaml
id: main_menu
data: { Subs: subscriptions.summary }       # данные модулей для шаблонов
blocks:                                     # тело сообщения сверху вниз
  - photo: assets/banner.jpg
  - text:
      ru: "## Привет, {{ .User.FirstName }}! 👋"
      en: "## Hi, {{ .User.FirstName }}! 👋"
  - buttons:                                # кнопки ВНУТРИ сообщения, между блоками (rich)
      align: center                         # left | center | right
      items: [ { text: { ru: "Как подключиться?", en: "How to connect?" }, goto: guide, style: link } ]
  - text: { ru: "Активных подписок: **{{ .Subs.ActiveCount }}**" }
keyboard:                                   # кнопки ПОД сообщением: ряды сверху вниз, в ряду слева направо
  - [ { text: { ru: "🛒 Купить подписку" }, goto: plans, style: success } ]
  - [ { text: { ru: "🔑 Мои подписки" }, goto: my_subs },
      { text: { ru: "🎁 Пригласить" }, goto: referral } ]
  - [ { text: { ru: "🎉 Пробный период" }, action: trial.start, style: primary, visible_if: trial.available } ]
  - [ { text: { ru: "💬 Поддержка" }, url: "https://t.me/my_support" } ]
```

Текст — Markdown с шаблонами Go (`{{ .Subs.ActiveCount }}`, `{{ .ExpiresAt | date }}`); перевод — прямо в экране (`ru`/`en`)
или ключом i18n. Фото загружается в Telegram один раз и дальше используется по `file_id` (кэш в `kv`).

#### Куда ведёт кнопка

| Поле | Что происходит |
|---|---|
| `goto: <экран>` (+ `params`) | Переход на экран — любой, в том числе созданный вами |
| `action: <модуль.действие>` (+ `params`, `on`) | Модуль выполняет действие и возвращает исход; `on` задаёт экран для каждого исхода |
| `back: true` / `home: true` | Назад по истории переходов / на стартовый экран |
| `url` / `web_app` / `copy_text` | Ссылка / Mini App / копирование текста (например, ссылки подписки) |

#### Как выглядит кнопка (Bot API 9.4+ — цвет и иконки, 10.3 — неактивные кнопки)

| Поле | Что задаёт |
|---|---|
| `text` | Надпись по языкам или ключ i18n; поддерживает шаблоны: `Продлить до {{ .ExpiresAt \| date }}` |
| `emoji` | Обычный эмодзи перед надписью — работает всегда (можно писать и прямо в `text`) |
| `icon` | ID кастомного эмодзи Telegram. Работает, только если у владельца бота Telegram Premium (в личных чатах) или бот купил дополнительный username на Fragment; иначе рендерер подставляет `emoji` |
| `style` | Цвет: `primary` (синий), `success` (зелёный), `danger` (красный); внутри тела сообщения — ещё `link` (без рамки). Произвольных цветов Telegram не поддерживает |
| `visible_if` / `disabled_if` | Скрыть кнопку или показать неактивной (серой, без действия) по именованному условию модуля (`trial.available`), а не по произвольному коду |

#### Действия, исходы и маршруты

Каждое действие объявляет свои параметры и исходы, а модуль даёт экраны по умолчанию для каждого исхода.
Переопределить их можно у конкретной кнопки (`on`) или глобально:

```yaml
# ui/routes.yaml
actions:                                    # исход действия → экран
  trial.start:      { ok: trial_started, already_used: trial_used, need_channel: join_channel }
  billing.checkout: { invoice: invoice, limit_reached: limit_screen }
events:                                     # экраны для событий вне нажатий
  billing.order.paid:     payment_success
  subscriptions.expiring: expiring_notice   # напоминание — тоже экран темы, со своими кнопками
commands:                                   # команды бота → экраны
  start: { new_user: welcome, default: main_menu }
  menu:  main_menu
  help:  faq
```

#### Списки из данных

```yaml
# ui/screens/my_subs.yaml
id: my_subs
data: { Subs: subscriptions.list }
empty: no_subs                              # подписок нет — показать экран no_subs
blocks:
  - text: { ru: "Ваши подписки ({{ len .Subs }}):" }
keyboard:
  - repeat: .Subs                           # по кнопке на каждую подписку
    columns: 1
    page_size: 6                            # больше — постранично «◀️ 1/3 ▶️»
    button: { text: "{{ .Label }} · до {{ .ExpiresAt | date }}", goto: sub_card, params: { id: "{{ .ID }}" } }
  - [ { text: { ru: "➕ Новая подписка" }, goto: plans, style: success } ]
  - use: back_row                           # фрагмент из fragments.yaml
```

Так же устроены списки тарифов, способов оплаты и устройств. Карточка подписки получает `id` из кнопки:

```yaml
# ui/screens/sub_card.yaml
id: sub_card
data: { Sub: subscriptions.get }            # параметр id берётся из кнопки
blocks:
  - text: { ru: "**{{ .Sub.Label }}**\nДо: {{ .Sub.ExpiresAt | date }}\nТрафик: {{ .Sub.Used | bytes }} из {{ .Sub.Limit | bytes }}" }
keyboard:
  - [ { text: { ru: "📋 Скопировать ссылку" }, copy_text: "{{ .Sub.URL }}" } ]
  - [ { text: { ru: "🔄 Продлить" }, action: billing.checkout, params: { renew: "{{ .Sub.ID }}" }, style: success } ]
  - [ { text: { ru: "✏️ Переименовать" }, goto: sub_rename, params: { id: "{{ .Sub.ID }}" } } ]
  - [ { text: { ru: "♻️ Перевыпустить ссылку" }, goto: revoke_confirm, params: { id: "{{ .Sub.ID }}" },
        style: danger, visible_if: sub.can_revoke } ]
  - use: back_row
```

Подтверждение опасного действия — обычный экран (`revoke_confirm`), на котором стоит кнопка с самим действием.

#### Свои страницы без кода

Инструкции по подключению, FAQ, «О сервисе», правила — обычные экраны темы, связанные через `goto`:

```yaml
id: guide
blocks: [ { text: { ru: "Выберите устройство:" } } ]
keyboard:
  - [ { text: "📱 iPhone", goto: guide_ios },     { text: "🤖 Android", goto: guide_android } ]
  - [ { text: "💻 Windows", goto: guide_windows }, { text: "🍏 macOS", goto: guide_macos } ]
  - use: back_row
```

#### Ввод текста

Экран может ждать ответ пользователя, и следующее сообщение уходит в действие:
`input: { action: subscriptions.rename, param: label, on: { ok: sub_card, invalid: rename_error } }`.

#### Схема навигации

`tors ui graph` строит схему всей темы — какие экраны есть и какие кнопки куда ведут. Фрагмент для стандартной темы:

```mermaid
flowchart LR
    main_menu -->|Купить| plans
    main_menu -->|Мои подписки| my_subs
    main_menu -->|Как подключиться| guide
    plans -->|тариф| plan_card
    plan_card -->|оплатить| invoice
    invoice -.->|оплата прошла| payment_success
    my_subs -->|подписка| sub_card
    my_subs -.->|пусто| no_subs
    sub_card -->|Продлить| invoice
    guide --> guide_ios
    guide --> guide_android
```

#### Формат сообщений и рендереры

**Rich messages** (Bot API 10.1–10.3, июнь–август 2026): метод `sendRichMessage` принимает текст в **Markdown**
(или HTML, или готовыми блоками) — заголовки, списки, таблицы, цитаты, раскрывающиеся блоки, фото и коллажи
в теле сообщения, а с 10.3 — блок кнопок внутри сообщения с выравниванием. Точный синтаксис Markdown — раздел
«Rich message formatting options» документации Bot API; сверить при реализации. Для тем это не важно: Markdown
для Telegram собирает рендерер.

Два рендерера — гостевые модули `telegram.renderers.*`, выбор в конфиге (`telegram.renderer`):

| | `rich` (по умолчанию) | `classic` (запасной) |
|---|---|---|
| Отправка | `sendRichMessage` | `sendPhoto` с подписью или `sendMessage` |
| Текст | Markdown как есть | Markdown → Telegram HTML (`goldmark`); таблицы и заголовки упрощаются |
| Фото | Блок в любом месте тела | Только над текстом или под ним (`show_caption_above_media`) |
| Кнопки внутри тела | Да, между любыми блоками | Переносятся под сообщение (`tors validate` предупреждает) |
| Переход между экранами | Всегда правка одного сообщения — фото лишь блок | Экран с фото ↔ без фото: удалить и отправить заново |

Rich messages появились в июне 2026. Как их показывают старые версии приложений и сторонние клиенты — проверить в фазе 3.
Если что-то не так, включается `classic`; темы при этом не меняются.

#### Точность расположения: что можно и чего нельзя

- Можно: точный ряд и место в ряду для каждой кнопки; кнопки внутри тела — между любыми блоками текста и фото,
  с выравниванием; порядок блоков; фото в любом месте тела (rich).
- Нельзя (ограничения Telegram): ширину отдельной кнопки — кнопки в ряду делят ширину поровну; размер и шрифт текста —
  только разметка (заголовки, жирный и т.п.); произвольный цвет кнопки.
- У Telegram есть лимиты на число кнопок в ряду и в сообщении и на длину данных кнопки — `tors validate` проверяет их
  (точные значения сверить при реализации).

#### Безопасность параметров

Параметры кнопок (например, `id` подписки) лежат в данных кнопки или в токене в `kv`, и пользователь может их подделать.
Поэтому каждое действие и каждый провайдер данных проверяет права: подписка принадлежит этому пользователю,
тариф доступен и т.д.

#### Инструменты

- `tors validate` — все `goto`, `on`, `events` и `commands` ведут на существующие экраны; у каждого действия сопоставлены
  все исходы; данные и параметры совпадают с объявленными модулями; ошибки шаблонов, переводы, лимиты Telegram;
  недостижимые экраны — предупреждение.
- `tors ui graph` — схема навигации (Mermaid).
- `tors ui docs` — для каждого провайдера данных и действия: параметры, поля для шаблонов, исходы.
- `tors ui export` — выгрузить стандартную тему в файлы.
- `tors ui preview <экран> --lang ru` — прислать экран в админ-чат с тестовыми данными.
- `/reload_ui` — перезагрузка темы без рестарта; при ошибке валидации остаётся прежняя тема.
- Визуальный редактор экранов и переходов на холсте с экспортом JSON — §9.6.

### 9.6. Редактор экранов (холст)

> **Статус: v0.1 сделан** — `web/ui-editor/` (как запустить и выложить на хостинг — в его README). Работают холст,
> симулятор, правка экранов и кнопок, проверки и экспорт JSON. Проверки и шаблоны считает Go-пакет
> `theme/`, собранный в WebAssembly. Пока нет `tors ui manifest`, поэтому редактор открывается
> со стандартным манифестом и принимает любой другой через «Файл → Открыть».

Веб-редактор, в котором тема собирается мышкой и сразу видно, как экраны будут выглядеть в Telegram.
На выходе — JSON темы, который вставляется в бота. Тема и так хранится в JSON — родном формате конфигурации ядра,
поэтому редактор не вводит новый формат, а собирает тот же файл, что описан в §9.5.

**Холст**

- Бесконечный холст, на нём карточки экранов, каждая нарисована как сообщение Telegram: фото, отформатированный текст,
  кнопки нужного цвета в своих рядах, кнопки внутри тела сообщения.
- **Точки входа** — отдельные узлы слева: `/start` (новый и вернувшийся пользователь), `/menu`, `/help`, события
  («оплата прошла», «подписка истекает»). От них стрелки ведут к экранам.
- **Переходы** — стрелки от конкретной кнопки к экрану. Протянул стрелку от кнопки «🛒 Купить» к экрану «Тарифы» — готово.
- **Действия** модулей — узлы с выходами по исходам: от `trial.start` выходят стрелки `ok`, `already_used`, `need_channel`,
  каждую можно направить на любой экран.
- **Режим «прокликать»** — симулятор: вводишь `/start`, видишь экран, нажимаешь кнопку — открывается следующий экран,
  как в настоящем боте, с тестовыми данными. Можно проверить, что новый пользователь увидит одно, а вернувшийся — другое.

**Правка экрана** (панель справа при выборе карточки)

- Блоки тела: добавить, удалить и перетащить фото, текст и ряд кнопок внутри сообщения.
- Клавиатура под сообщением — сетка: кнопки перетаскиваются между рядами и местами в ряду.
- Кнопка: надпись по языкам, эмодзи, иконка, цвет (только цвета, которые есть в Telegram), условие показа,
  куда ведёт (`goto`, действие с исходами, ссылка, Mini App, копирование, назад, домой), параметры.
- Текст: Markdown с панелью форматирования и **вставкой переменных**. Список переменных берётся из модулей:
  выбрал `subscriptions.summary` → `ActiveCount`, и в текст вставилось `{{ .Subs.ActiveCount }}`; в превью сразу
  подставляется пример значения.
- Переключатели превью: язык (ru/en), светлая/тёмная тема, рендерер `rich` или `classic` (видно, как упростится
  экран в запасном режиме).

**Откуда редактор знает о модулях — манифест.** `tors ui manifest` выгружает из собранного бота `manifest.json`:
провайдеры данных (параметры, поля с типами и примерами значений), действия (параметры, исходы, экраны по умолчанию),
условия, события, команды, языки и лимиты Telegram. Модули из чужих репозиториев попадают в манифест автоматически,
потому что он строится из того, что реально вкомпилировано в бота.

**Проверки — тот же код, что в боте.** Валидатор темы и движок шаблонов — отдельный Go-пакет без зависимостей
от Telegram, который собирается ещё и в WebAssembly. Редактор подсвечивает ровно те ошибки, что выдаст `tors validate`:
кнопка ведёт в никуда, у действия не расписан исход, переменная не существует, превышен лимит кнопок. Превью текста
строится тем же `text/template`, что и в боте, поэтому подстановка переменных в редакторе и в боте совпадает.
Внешний вид сообщения рисует сам редактор — это приближение к интерфейсу Telegram; точное отображение проверяется
через `tors ui preview`.

**Экспорт и применение**

```json
{
  "version": 1,
  "commands": { "start": { "new_user": "welcome", "default": "main_menu" } },
  "screens": {
    "main_menu": {
      "data": { "Subs": "subscriptions.summary" },
      "blocks": [
        { "photo": "https://example.com/banner.jpg" },
        { "text": { "ru": "## Привет, {{ .User.FirstName }}! 👋", "en": "## Hi, {{ .User.FirstName }}! 👋" } }
      ],
      "keyboard": [
        [ { "text": { "ru": "🛒 Купить подписку" }, "goto": "plans", "style": "success" } ],
        [ { "text": { "ru": "🔑 Мои подписки" }, "goto": "my_subs" },
          { "text": { "ru": "🎁 Пригласить" }, "goto": "referral" } ]
      ]
    }
  },
  "editor": { "positions": { "main_menu": [120, 80] } }
}
```

- Кнопка «Экспорт» отдаёт один JSON. Если в теме есть загруженные с компьютера картинки — ZIP с `theme.json` и `assets/`;
  картинки по ссылке или Telegram `file_id` остаются внутри JSON.
- Применить: положить файл в `ui/` бота и выполнить `/reload_ui` или `tors ui import theme.json`. Бот сначала проверяет тему,
  при ошибке остаётся прежняя.
- Раздел `editor` хранит положение карточек на холсте; бот его игнорирует. Поэтому экспортированный JSON можно снова открыть
  в редакторе и продолжить с того же места.
- Начать можно не с пустого холста: `tors ui export --format json` выгружает стандартную тему, её и открываем.
- `"version"` — версия формата темы; бот понимает старые версии и обновляет их.

**Где работает**

- **Этап 1 (сделан раньше фазы 7, v0.1)** — статическая веб-страница без сервера: открывается в браузере, загружает `manifest.json` и тему
  из файлов, выдаёт JSON. Ничего не хранит и ни к чему не подключается, поэтому её безопасно выложить хоть на GitHub Pages.
  От бота нужны только движок тем из фазы 3 и манифест, поэтому редактор можно сделать и раньше.
- **Этап 2 (фаза 9)** — тот же редактор внутри админки (Mini App или страница в модуле `http` с входом по Telegram для админов):
  манифест и тема загружаются из бота, превью — на реальных данных, кнопка «Отправить мне в Telegram» вместо ручного
  `tors ui preview`, «Сохранить» вместо копирования JSON (тема в БД, `telegram.ui.sources.db`), история версий и откат.

**Стек редактора** — тот же, что у Mini App (§21): TypeScript + React + Vite; холст — React Flow (`@xyflow/react`),
состояние — Zustand + Immer; проверки и шаблоны — Go → WebAssembly. В v0.1 перетаскивание кнопок сделано на HTML5 DnD,
а текст редактируется обычным полем с панелью разметки — dnd-kit и CodeMirror подключатся, когда понадобятся.
Код лежит в `web/ui-editor/`.

---

## 10. Фича-модули

| Модуль | Логика | Связи |
|---|---|---|
| `trial` | Один раз на пользователя (не на подписку), тариф из конфига, опционально — подписка на канал | `subscriptions.Grant(source=trial, key=trial:user:ID)` |
| `referral` | Ссылка `/start ref_…`, награда рефереру при первой оплате реферала: бонусные дни (неприменённые) или звёзды на баланс, если включён `wallet`; скидка рефералу | `users.registered`, `billing.order.paid`, `billing.pricing.referral_discount` |
| `promo` | Коды на скидку или на дни, лимиты использований, сроки | `billing.pricing.promo`, `subscriptions.Grant(source=promo)` |
| `wallet` (опц.) | Внутренний баланс в `XTR`: пополнение Stars как товар `wallet.topup`, оплата тарифов с баланса, зачисление реферальных наград и компенсаций | `billing.gateways.balance`, `billing.order.paid` |
| `notifications` | Шаблоны уведомлений по событиям, уведомления админу | durable-подписки, `telegram.Sender` |
| `broadcast` | Рассылки по сегментам через `jobs` с учётом лимитов | `users`, `telegram.Sender` |
| `admin` (опц.) | Админ-меню, роли по списку Telegram ID, аудит действий | вклад других модулей через `Menu("admin")` |

---

## 11. Пример конфигурации (YAML → адаптер → JSON)

```yaml
logging: { level: info, format: json }

apps:
  database: { dsn: "{env.DATABASE_URL}" }
  jobs:     { queues: { default: { workers: 10 }, notify: { workers: 4 } } }
  kv:       { store: { store: postgres } }
  http:     { listen: ":8080", public_url: "https://bot.example.com" }
  i18n:     { default: ru, languages: [ru, en], overrides_dir: /etc/tors/locales }
  users:    {}

  panels:
    providers:
      rw-main:
        provider: remnawave
        url: https://panel.example.com
        token: "{env.REMNAWAVE_TOKEN}"
        webhook: { path: /hooks/remnawave, secret: "{env.REMNAWAVE_WEBHOOK_SECRET}" }
      # xui-de:
      #   provider: 3xui
      #   url: https://de.example.com:2053/secret-path
      #   username: admin
      #   password: "{file./run/secrets/xui_de}"
      #   sub_base_url: https://de.example.com:2096/sub/

  catalog:
    source:
      source: config
      plans:
        - id: month
          title: { ru: "1 месяц", en: "1 month" }
          period: 30d
          traffic: 200GiB
          traffic_reset: month
          devices: 3
          prices: { XTR: 150 }          # цены в Telegram Stars
          placements: [ { provider: rw-main, squads: ["<squad-uuid>"] } ]
        - id: trial
          hidden: true
          period: 3d
          traffic: 10GiB
          devices: 1
          placements: [ { provider: rw-main, squads: ["<squad-uuid>"] } ]

  billing:
    order_ttl: 30m
    gateways:
      stars:    { gateway: telegram_stars }
      # balance: { gateway: balance }  # оплата с внутреннего баланса, если включён wallet
    pricing:
      - { modifier: promo }
      - { modifier: referral_discount, percent: 10 }

  subscriptions:
    max_per_user: 5              # 0 — без ограничения
    reminders: [72h, 24h]
    reconcile_every: 1h

  trial:         { plan: trial, require_channel: "@my_channel" }
  referral:      { reward: { days: 7 }, when: first_payment }
  promo:         {}
  # wallet:      {}               # внутренний баланс — необязательный модуль
  notifications: { admin_chat: -1001234567890 }

  telegram:
    token: "{env.BOT_TOKEN}"
    admins: [111111111]
    transport: { transport: webhook }   # для локальной разработки — polling
    middleware:
      - { middleware: recover }
      - { middleware: logging }
      - { middleware: ratelimit, rate: "2/s" }
      - { middleware: users }
      - { middleware: i18n }
    handlers:
      - { handler: start }
      - { handler: main_menu }
      - { handler: shop }
      - { handler: my_subscriptions }
      - { handler: trial }
      - { handler: referral }
      - { handler: promo }
      - { handler: support, contact: "@support" }
      - { handler: language }
      # - { handler: wallet }
      - { handler: admin }               # необязательный модуль
    renderer: { renderer: rich }         # запасной вариант — classic (§9.5)
    ui_dir: /etc/tors/ui                 # тема: переопределения экранов
```

Удалить `trial` из `apps` и `handlers` — триала нет. Раскомментировать `wallet` — появился баланс. Раскомментировать `xui-de` —
появилась вторая панель. Ни строчки кода.

---

## 12. Как написать модуль

```go
package support

import (
	"embed"
	"errors"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/modules/telegram"
)

//go:embed ui
var defaultTheme embed.FS // ui/support.yaml: экран support и кнопка в главном меню стандартной темы

func init() { tors.RegisterModule(Support{}) }

// Support — кнопка и команда «Поддержка».
type Support struct {
	Contact string `json:"contact"`
}

func (Support) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{
		ID:  "telegram.handlers.support",
		New: func() tors.Module { return new(Support) },
	}
}

func (s *Support) Validate() error {
	if s.Contact == "" {
		return errors.New("contact is required")
	}
	return nil
}

func (s *Support) Register(r telegram.Router) error {
	r.Data("support.info", telegram.DataFunc(s.info)) // в теме: data: { Support: support.info }
	r.DefaultTheme(defaultTheme)
	return nil
}

func (s *Support) info(c *telegram.Ctx, _ telegram.Params) (any, error) {
	return map[string]any{"Contact": s.Contact}, nil
}

var (
	_ tors.Validator   = (*Support)(nil)
	_ telegram.Handler = (*Support)(nil)
)
```

**Чек-лист автора модуля**

1. `init()` + `tors.RegisterModule`; ID в namespace хоста, имя в `snake_case`.
2. Конфиг — экспортируемые поля с `json`-тегами; значения по умолчанию выставляются в `Provision`.
3. `New()` без побочных эффектов; никаких глобальных переменных состояния.
4. `Provision`: зависимости через `ctx.App`/`AppAs`, загрузка гостей, ресурсы, маршруты, подписки на события. После него модуль готов к вызовам.
5. `Validate` — только проверки. `Start`/`Stop` — только фоновая активность, `Stop` идемпотентен. `Cleanup` освобождает всё из `Provision`.
6. Своя схема БД (создаётся автоматически) и встроенные миграции; свои тексты в `locales/` с префиксом ключей.
7. Interface guards (`var _ tors.Provisioner = (*X)(nil)`).
8. Провайдеры и шлюзы проходят общий контрактный тест-сьют (`panelstest.Run(t, p)`, `billingtest.Run(t, g)`).
9. README модуля с примером конфига; модуль появляется в `tors list-modules`.

---

## 13. Сборка и дистрибуция

```go
// cmd/tors/main.go — стандартный дистрибутив
package main

import (
	torscmd "github.com/tori43-hash/tors/cmd"

	_ "github.com/tori43-hash/tors/modules/standard"
	// _ "github.com/acme/tors-gateway-foo" // сторонний модуль — просто импорт
)

func main() { torscmd.Main() }
```

**Модули из любого репозитория**

- Модуль — обычный Go-модуль в любом месте: GitHub, GitLab, свой Git-сервер, приватный репозиторий или папка на диске.
  Регистрировать его у нас не нужно, наш репозиторий о нём не знает. Единственное требование — модуль импортирует
  наши пакеты-контракты (`github.com/tori43-hash/tors`, `…/modules/panels` и т.п.) совместимой версии.
- Подключение сейчас: свой `main.go` (пример выше) + `go get github.com/acme/tors-foo@v1.2.0` + `go build`.
  Можно завести отдельный репозиторий-«дистрибутив», где лежит только такой `main.go` со списком модулей.
- Фаза 9: утилита `xtors` (аналог xcaddy) —
  `xtors build --with github.com/acme/tors-foo@v1.2.0 --with github.com/me/my-mod=../my-mod` (локальная папка — для разработки).
- Приватные репозитории: `GOPRIVATE=github.com/acme/*` + git-доступ к репозиторию на машине сборки.
- Docker: `docker build --build-arg TORS_MODULES="github.com/acme/tors-foo@v1.2.0" .` собирает образ с нужными модулями.
- После сборки модуль виден в `tors list-modules` и `tors version` (с путём репозитория и версией) и включается
  в конфиге так же, как стандартный. Схема БД и роль для него создаются автоматически.
- Фаза 9: шаблон репозитория модуля (пример модуля, контрактные тесты, CI).
- Поставка: один статический бинарь, Docker-образ (distroless), `docker-compose.yml` с Postgres. Redis не нужен.

---

## 14. Безопасность

- Секреты только через `{env.*}`/`{file.*}`; логгер маскирует поля с тегом `secret`.
- Вебхуки: проверка подписи каждым модулем (HMAC Remnawave, `secret_token` Telegram).
- Mini App: каждый запрос API авторизуется по `initData` (HMAC от токена бота) с проверкой срока `auth_date`.
- Сумма и валюта платежа сверяются с заказом на сервере (в `pre_checkout_query` и в `Confirm`); цена считается только на сервере.
- Идемпотентность везде, где есть деньги и выдача: уникальные ключи в БД, а не проверки в коде.
- Админ-действия — только для ID из конфига, аудит через события.
- Rate limit на пользователя; защита от злоупотребления триалом (одноразовость по пользователю, опционально — канал).
- События нельзя подделать от имени чужого модуля, а критичные обработчики перепроверяют факт у источника (§3.6).

### 14.1. Данные и изоляция модулей

Уровни складываются: то, от чего не защищает уровень 1, закрывает уровень 2; то, от чего не защищает уровень 2, —
меры А–Б ниже, а для непроверенного кода — уровень 3.

| Уровень | Для кого | Что даёт | Что остаётся открытым → чем закрывается |
|---|---|---|---|
| **1. Схема на модуль** | Все модули, всегда (фаза 2) | Нет конфликтов имён; модуль по умолчанию видит только свою схему; его миграции не трогают чужое | Намеренный запрос к чужой схеме → уровень 2 |
| **2. Роль Postgres на модуль** | Все модули в продакшене (фаза 6, до появления сторонних модулей) | Postgres сам запрещает модулю читать и менять чужие данные; ошибка или SQL-инъекция в модуле не выходит за пределы его схемы | Вредоносный код внутри процесса → меры А и Б; непроверенный код → уровень 3 |
| **3. Отдельный процесс / WASM** | Непроверенный сторонний код (фаза 9+) | Модуль не видит память, окружение, токен бота и чужие доступы — только то, что мост явно разрешил | — |

**Как работает уровень 2**

- Схемы, роли и права создаёт отдельный шаг `tors db provision` с админскими правами при деплое
  (в docker compose — одноразовый сервис, который отрабатывает до старта бота). **Сам бот админского доступа к БД не получает.**
- Права роли `tors_<module>`: всё — на свою схему; `INSERT` в очередь `jobs` (для outbox); `REFERENCES` на `users.users`;
  `SELECT` — только на представления, которые другой модуль явно экспортировал (`db.Export(…)`) и которые этот модуль явно запросил.
- `tors db provision` генерирует пароли ролей и кладёт их в файл секретов, доступный только пользователю бота;
  при каждом запуске provision пароли меняются.
- У каждого модуля свой небольшой пул (до 4 соединений, простаивающие закрываются) — около 20 модулей укладываются
  в стандартные 100 соединений Postgres.
- Миграции модуля выполняются под его ролью: модуль не может изменить чужие таблицы даже миграцией.
- `tors db plan` показывает новые миграции каждого модуля до применения — миграции сторонних модулей стоит прочитать.

**Почему внутри процесса нельзя полностью защититься от вредоносного кода.** Все модули работают в одном процессе.
Код модуля может читать переменные окружения, файлы и — через `unsafe` и `reflect` — любую память процесса,
включая токен бота и доступы других модулей. Go не изолирует пакеты одного процесса друг от друга
(так же устроены Java, Python, Node и плагины Caddy). Поэтому защита строится в три слоя.

**А. Не пустить вредоносный код** (в CI нашего репозитория — сразу; для сторонних модулей — в `xtors`, фаза 9)

- Модули — только из белого списка источников. Версии закреплены в `go.sum` и сверяются с публичной базой
  контрольных сумм Go (`sum.golang.org`): собирается ровно тот код, который проверяли.
- Анализ возможностей `capslock` (Google): по всем транзитивным вызовам показывает, к чему модуль имеет доступ —
  файлы, сеть, переменные окружения, запуск процессов, `unsafe`, `reflect`, cgo. Политика для стороннего модуля в процессе:
  запрещены запуск процессов, cgo, `unsafe`, произвольное исполнение, чтение окружения и файлов; сеть — только если
  модуль её объявил (шлюз, панель). Нарушение — сборка падает.
- При обновлении модуля `capslock` сравнивает возможности старой и новой версии. Если новая версия внезапно
  получила, например, сеть или запуск процессов, сборка останавливается до ручной проверки.
- Ручное ревью стороннего модуля и его зависимостей перед первым включением; `govulncheck`.
- Оговорка: `capslock` — статический анализ. Это сильный фильтр, но не формальная гарантия, поэтому непроверенный код
  всё равно идёт через уровень 3.

**Б. Уменьшить, что можно украсть и куда унести** (работает, даже если плохой код прошёл проверку)

- В процессе бота нет админского доступа к БД (см. `tors db provision`).
- Секретов нет в переменных окружения: ядро читает их при старте и отдаёт каждому модулю только его собственные значения,
  после загрузки конфига окружение процесса очищается. Модуль, которому по правилу А запрещено читать окружение и файлы,
  до чужих секретов не дотянется.
- Исходящий трафик контейнера — только по белому списку (Telegram API, панели, БД) через egress-прокси
  (например, `smokescreen` от Stripe) или файрвол: украденное некуда отправить. Оговорка: белый список общий на процесс,
  поэтому канал через разрешённые адреса (например, Telegram API) остаётся — его закрывает только уровень 3.
- Postgres журналирует отказы в доступе для ролей модулей, а бот пересылает их в админ-чат: так видна и ошибка,
  и попытка залезть в чужие данные.

**В. Изолировать непроверенный код — уровень 3**

- `plugins.grpc` (hashicorp/go-plugin): модуль — отдельный процесс под своим пользователем ОС или в своём контейнере.
  У него нет токена бота и чужих доступов: только свой логин к своей схеме, свой белый список исходящих адресов и узкий
  API к боту — разрешённые в конфиге события и сервисы. Границы держат операционная система и Postgres, а не договорённости.
- `plugins.wasm` (wazero): песочница без сети, файлов и окружения; модуль может только вызывать функции, которые дал хост,
  включая запросы к своей схеме через хост. Строже, но писать такие модули сложнее.
- Цена: вызовы медленнее, модули сложнее писать и отлаживать. Поэтому уровень 3 — только для кода, которому не доверяем.

**Кто как запускается**

| Автор модуля | Как запускать |
|---|---|
| Мы (этот репозиторий) | В процессе; уровни 1–2, меры Б; отчёт `capslock` по зависимостям в CI |
| Известный автор, код проверен, версия закреплена | В процессе; уровни 1–2, меры А и Б; ревью каждого обновления |
| Неизвестный автор или непроверенный код | Только уровень 3 |

**Общие меры**

- Postgres не доступен снаружи — только во внутренней сети docker compose; при БД на другом сервере — TLS (`sslmode=verify-full`).
- Минимум персональных данных: Telegram ID, username, язык. Данных банковских карт нет вообще — оплата через Stars.
- Бэкапы: ежедневный `pg_dump` (при росте — WAL-G) и регулярная проверка восстановления.
- Выключенный модуль не удаляет свои данные; удаление — только явной командой `tors db drop-module <name>`.

## 15. Наблюдаемость

- `slog` с полями `module`, `user_id`, `order_id`, `trace_id`.
- Prometheus: апдейты/ошибки хендлеров, заказы и платежи по шлюзам, задачи и ретраи, латентность и ошибки провайдеров панелей.
- `/readyz` агрегирует `HealthChecker` приложений (БД, панели, Telegram).
- Критичные события (`panels.node.down`, серия ошибок провайдера, неудачные платежи) — в админ-чат через `notifications`.

## 16. Тестирование

- **Ядро**: unit-тесты реестра, форм `LoadModule`, порядка жизненного цикла, отката, детекта циклов, плейсхолдеров, шины событий. Цель — ≥85% покрытия.
- **Контрактные сьюты**: `panelstest` (fake, Remnawave, 3x-UI), `billingtest` (fake, Stars — в тестовом окружении Telegram).
- **Интеграционные**: testcontainers — Postgres (миграции, River), Remnawave и 3x-UI в docker для провайдеров (отдельный job CI).
- **Telegram**: хендлеры тестируются с фейковым `Sender` и сгенерированными апдейтами.
- **Время**: напоминания, истечение заказов и подписок, TTL в `kv` — тестируются через `testing/synctest` (виртуальные часы, без реального ожидания).
- **E2E-сценарий**: конфиг с `fake` провайдером и `fake` шлюзом — «покупка → выдача → продление → истечение» без внешних сервисов.
- CI: `golangci-lint` v2 (включая `depguard` с правилами §4.4), `go test -race`, `govulncheck`, проверка «ядро = только stdlib»,
  `apidiff` для пакетов-контрактов (ломающее изменение API без мажорной версии — красный CI).

---

## 17. Дорожная карта

| Фаза | Содержание | Критерий готовности | Размер |
|---|---|---|---|
| **0. Фундамент** | `go.mod`, линтеры и правила зависимостей, CI, Taskfile, шаблон ADR | CI зелёный на пустом каркасе | S |
| **1. Ядро** | Реестр, `Context`, все формы `LoadModule`, жизненный цикл с откатом, config + плейсхолдеры + `Duration`/`Size`, адаптеры, события, логи, CLI (`run/validate/adapt/list-modules/version`), YAML-адаптер | Демо-приложение `hello` запускается из YAML; ≥85% покрытия; ядро без внешних зависимостей | M |
| **2. Инфраструктура** | `database` (pgx, схема на модуль, tx, миграции по модулям, без доступа модулей к общему пулу), `jobs` (River, cron, durable-события), `kv` (memory, postgres), `http`, `i18n` | `docker compose up` поднимает бинарь + Postgres; миграции модулей применяются независимо | M |
| **3. Telegram** | Приложение, polling/webhook, роутер, middleware, UI как данные (экраны, переходы, действия с исходами, провайдеры данных, темы, `tors ui`, манифест; валидатор и шаблоны — отдельный пакет, собираемый в WebAssembly), рендереры `rich` и `classic`, сцены, `Sender` с лимитами; `start`, `main_menu`, `language`, `support` | Бот отвечает; экраны собираются из включённых модулей; тема задаёт экраны, тексты, фото, раскладку и цвета кнопок и все переходы, включая свои страницы, без пересборки | M/L |
| **4. Домен + Remnawave** | `users`, `catalog` (config), контракт `panels` + `fake` + `panelstest`, провайдер Remnawave (+вебхуки), `subscriptions` (несколько на пользователя, `Grant`, reconcile, напоминания), `my_subscriptions` (список, ссылка, QR, имя) | Команда выдаёт пользователю две подписки → в Remnawave два аккаунта, обе ссылки работают | L |
| **5. Биллинг = MVP** | `billing` (заказы, платежи, `Confirm`, pricing), шлюзы `telegram_stars` + `fake`, `shop` (новая / продлить выбранную), выдача через durable-события, `notifications` | E2E: покупка новой и продление выбранной подписки за Stars; повторный `successful_payment` не продлевает дважды; при недоступной панели выдача ретраится | L |
| **6. Рост и админка** | `trial`, `promo`, `referral` (бонусные дни), `wallet` + шлюз `balance`, `admin` (статистика, карточка, выдача дней, возврат Stars, рассылка), `broadcast`, `metrics`, роли Postgres на модуль и `tors db provision` (§14.1) | Фичи включаются/выключаются только конфигом; модуль под своей ролью не может прочитать чужую схему | M |
| **7. Веб: редактор экранов и Mini App** | Редактор экранов — статическая страница (§9.6): холст, симулятор, переменные из манифеста, проверки на WebAssembly, экспорт и импорт JSON. Приложение `webapp`: авторизация по `initData`, API витрины и подписок, оплата через `createInvoiceLink`, `webui/` у фич | Экран, собранный в редакторе, после вставки JSON выглядит и ведёт себя в боте так же, как в симуляторе; покупка и управление подписками из Mini App без изменений домена | L |
| **8. 3x-UI** | Провайдер `3xui`, контрактные тесты на docker-образе, опрос → нормализованные события, режим `shared` трафика, `subaggregator` (опц.) | Тариф на 3x-UI работает через тот же `shop`/`my_subscriptions` без изменений домена и UI | M/L |
| **9. Эксплуатация** | Hot reload (`SIGHUP`, `tors reload`), `UsagePool`, `xtors` с проверкой `capslock`, `plugins.grpc` (по необходимости), `catalog.sources.db` + редактирование тарифов в админке, редактор экранов внутри админки (сохранение в БД, живые данные, история версий), документация для авторов модулей | Перезагрузка конфига без рестарта; сторонний модуль собирается `xtors` | M |

Вертикальный срез появляется уже в фазе 4 — это главный способ не переусложнить ядро: каждое его API проверяется реальными модулями.
Фазы 7 и 8 независимы друг от друга, их можно поменять местами.

---

## 18. Решения (ADR, оформить в `docs/adr/`)

1. **ADR-001** Модули компилируются в бинарь (регистрация в `init()`), а не грузятся через `plugin`/go-plugin: переносимость, типобезопасность, один бинарь — как Caddy.
2. **ADR-002** Нативный конфиг — JSON; YAML — модуль-адаптер.
3. **ADR-003** PostgreSQL обязателен; очередь задач — River (транзакционная постановка решает outbox).
4. **ADR-004** Контракт панели — декларативный идемпотентный `EnsureAccount` + опциональные capability-интерфейсы.
5. **ADR-005** Источник истины: бот — коммерческое состояние, панель — рантайм; периодическая сверка.
6. **ADR-006** Шина событий в ядре (in-memory, синхронная); гарантированная доставка — модуль `jobs`.
7. **ADR-007** Telegram-библиотека `telego`, изолирована в приложении `telegram` и его гостях.
8. **ADR-008** Деньги — `int64` в минорных единицах + ISO-код; Stars — `XTR`.
9. **ADR-009** В MVP единственный шлюз — Telegram Stars; другие добавляются модулями.
10. **ADR-010** У пользователя может быть несколько подписок; одна подписка = один аккаунт в панели и своя ссылка.
11. **ADR-011** Одна копия процесса; `kv` и очередь задач — в той же PostgreSQL, Redis не используется.
12. **ADR-012** Схема PostgreSQL на модуль; в продакшене — роль на модуль, созданная отдельным шагом без админ-доступа у бота;
    для кода в процессе — проверка возможностей при сборке и минимизация секретов; изоляция недоверенного кода — только вне процесса.
13. **ADR-013** Событие `X.*` публикует только модуль `X`; обработчики, выдающие ценное, перепроверяют факт у источника.
14. **ADR-014** Клиент Remnawave — SDK `remnawave-api-go/v3` с зафиксированной версией внутри провайдера; запасной путь — форк или своя генерация.
15. **ADR-015** UI как данные: модули дают данные и действия с исходами, тема (YAML) задаёт экраны, кнопки и все переходы;
    рендерер по умолчанию — rich messages, запасной — classic.
16. **ADR-016** Редактор экранов собирает тот же JSON темы, что читает бот; проверки и шаблоны — общий Go-код, собранный в WebAssembly.

## 19. Риски

| Риск | Смягчение |
|---|---|
| Переусложнение ради модульности | Вертикальный срез в фазе 4; в ядро — только то, что нужно двум и более модулям |
| Ломающие изменения API Remnawave | Anti-corruption layer в провайдере, матрица версий, проверка версии при старте, контрактные тесты |
| Зоопарк версий и форков 3x-UI | Capability-флаги, матрица совместимости, тесты на конкретных образах |
| Единственный способ оплаты — Stars (комиссия, вывод через Fragment, не всем пользователям удобно) | Новый шлюз — отдельный модуль; домен и UI не меняются (`RedirectURL` уже в контракте) |
| Двойная выдача / потерянная оплата | Уникальные ключи в БД, транзакционный outbox, идемпотентные обработчики, E2E-тесты на повторы |
| River ещё в версии 0.x (API может меняться) | Скрыт за модулем `jobs`: обновление или замена затрагивает один модуль |
| Ломающие изменения контрактов для сторонних модулей | Semver для пакетов-контрактов, `apidiff` в CI, deprecation перед удалением |
| Сложность hot reload | Правила жизненного цикла соблюдаются с фазы 1, сама перезагрузка — в фазе 9 |
| Детали внешних API в плане не сверены с первоисточником: API 3x-UI, заголовок подписи вебхуков Remnawave, ограничения подписок Stars | Проверить по документации и живым инстансам в начале соответствующей фазы; контрактные тесты фиксируют поведение |
| Rich messages — новинка июня 2026: поддержка в старых и сторонних клиентах не проверена | Рендерер `classic` как запасной; формат тем от рендерера не зависит |
| Архитектура — гипотеза, пока нет кода | Вертикальный срез в фазе 4 проверяет API ядра на реальных модулях; при необходимости ядро правится до того, как на него опирается много модулей |

## 20. Решения и открытые вопросы

**Принято**

| Вопрос | Решение |
|---|---|
| Платёжные шлюзы | Только Telegram Stars (+ `fake` для разработки) |
| Подписок на пользователя | Несколько, лимит `max_per_user` в конфиге (§8.2) |
| Внутренний баланс | Необязательный модуль `wallet` (фаза 6) |
| Админка | Необязательный модуль `admin` |
| Тарифы | В конфиге (`catalog.sources.config`); редактирование из админки — фаза 9 |
| Языки | ru (по умолчанию) и en |
| Импорт пользователей из панели | Не нужен |
| Mini App | Планируется — фаза 7 |
| Масштаб | Одна копия бота; `kv` и очередь в PostgreSQL, Redis не нужен. Webhook в продакшене, polling для разработки |

**Открыто**

1. 3x-UI: одна подписка на несколько серверов (агрегатор `subaggregator`) или сервер = отдельный тариф? Можно решить к фазе 8.
2. Нужно ли автопродление? Bot API умеет подписки Stars с автосписанием, но только с периодом 30 дней.
   Решение не блокирует MVP: это возможность шлюза `telegram_stars`, её можно добавить позже.
3. Реферальная награда по умолчанию: бонусные дни или звёзды на баланс (если включён `wallet`)? Оба варианта поддерживаются конфигом.

---

## 21. Стек

Версии сверены по реестрам (Go proxy, npm, Docker Hub) на 28.09.2026, включая следующие мажорные пути Go-модулей
(`/v2`, `/v3`, …): в Go они публикуются как отдельные модули. При старте разработки версии фиксируются в `go.mod`.

| Область | Выбор | Версия | Почему |
|---|---|---|---|
| Язык | Go | 1.27.1 | Последний стабильный релиз; `encoding/json/v2` и сборщик мусора Green Tea включены по умолчанию |
| JSON / конфиг | `encoding/json/v2` + `jsontext` (stdlib) | Go 1.27 | Строгий разбор, быстрее v1; ядро остаётся только на stdlib |
| YAML-адаптер | `go.yaml.in/yaml/v3` | v3.0.5 | Официальное продолжение `gopkg.in/yaml.v3`, который больше не поддерживается; v4 пока в RC |
| Логи | `log/slog` (stdlib) | — | Структурные логи без зависимостей |
| HTTP | `net/http` + `ServeMux` (stdlib) | — | Маршруты с методами и параметрами, фреймворк не нужен |
| Telegram | `github.com/mymmrac/telego` | v1.12.1 | Поддерживает последний Bot API 10.3 (24.08.2026). Запасной вариант — `go-telegram/bot` v1.27.0 (тоже 10.3, без зависимостей) |
| СУБД | PostgreSQL | 18 (18.6) | Последняя мажорная версия |
| Драйвер БД | `github.com/jackc/pgx/v5` | v5.11.0 | Стандарт де-факто для Postgres в Go |
| Запросы | `sqlc` | v1.31.1 | Типобезопасный Go-код из SQL, без ORM; подключается директивой `tool` в `go.mod` |
| Миграции | `github.com/pressly/goose/v3` | v3.28.0 | `embed`, отдельная таблица версий на модуль |
| Очередь задач | `github.com/riverqueue/river` | v0.47.0 | Задачи в той же транзакции, что и данные; cron; ретраи |
| Клиент Remnawave | `github.com/Jolymmiles/remnawave-api-go/v3` (сгенерирован `ogen`) | v3.4.4 | Под Remnawave 3.4; спрятан внутри провайдера, запасной путь — форк или своя генерация (§7.5) |
| i18n | `github.com/nicksnyder/go-i18n/v2` | v2.6.1 | ru + en, переопределение текстов |
| Markdown → Telegram HTML | `github.com/yuin/goldmark` | v1.8.6 | Для запасного рендерера `classic` |
| QR-коды | `github.com/yeqown/go-qrcode/v2` | v2.3.0 | Поддерживается; популярный `skip2/go-qrcode` заброшен с 2020 |
| Метрики и трейсы | OpenTelemetry Go + Prometheus-экспортер | otel v1.46.0 | Отраслевой стандарт |
| Схема конфига | `github.com/invopop/jsonschema` | v0.14.0 | `tors schema` → подсказки и проверка конфига в редакторе |
| Тесты | `testing` + `testing/synctest`, `testcontainers-go` | v0.44.0 | Виртуальное время; настоящий Postgres и панели в Docker |
| Качество | `golangci-lint` v2 + `depguard`, `govulncheck`, `apidiff`, `capslock` | v2.14.0, x/vuln v1.8.0, capslock v0.3.3 | Стиль, архитектурные правила, уязвимости, совместимость API, возможности зависимостей |
| Редактор экранов (v0.1 сделан) | Тот же фронтенд-стек + `@xyflow/react` (холст), Zustand + Immer; Go → WebAssembly для проверок и шаблонов | React 19.3, Vite 8.3, TS 7.0, React Flow 12.12, Zustand 5.0, Immer 11 | Холст узлов — React Flow, стандарт для таких редакторов |
| Mini App (фаза 7) | TypeScript + React + Vite + `@tma.js/sdk-react` + TanStack Query | TS 7.0, React 19.3, Vite 8.3, tma.js 3.0, Query 5 | Актуальный SDK Mini App — `@tma.js/*`: пакеты `@telegram-apps/*` не обновлялись с октября 2025 |
| Поставка | Статический бинарь, Docker (distroless), docker compose | — | Один бинарь + Postgres |

Идиомы Go, которые используются в плане: generics (`AppAs[T]`, `On[E]`), итераторы `iter.Seq2` для постраничных списков,
`context` во всех вызовах, `errors.Join`, `sync.WaitGroup.Go`.

**Сознательно не используем**, хотя это популярно:

- ORM (GORM, ent) — вместо них `sqlc`: SQL остаётся явным, а типы генерируются.
- Веб-фреймворки (gin, echo, fiber) — возможностей stdlib `net/http` хватает.
- DI-контейнеры (wire, fx) — связывание через `Context`, как в Caddy.
- Микросервисы, Kafka/NATS, Redis — для одной копии бота это лишняя инфраструктура. Если понадобятся, подключаются модулями.

---

## 22. Границы расширяемости: какой модуль можно написать

«Абсолютно любой модуль» не примет ни одна система, включая Caddy. Здесь цель другая: любой модуль, который
написан на Go, является доверенным кодом и встраивается в существующую точку расширения или объявляет свою,
подключается **без изменения ядра**. То, что выходит за эти рамки, тоже решается модулями, а не правкой ядра.

**Что можно сделать без изменения ядра**

| Задача | Как |
|---|---|
| Новая панель (Marzban, Hiddify, …) | Гость `panels.providers.*` |
| Новый способ оплаты | Гость `billing.gateways.*` |
| Скидка, акция, особая цена | Гость `billing.pricing.*` |
| Запрет заказа или выдачи (антифрод, лимиты) | Подписка на «before»-события `billing.order.creating`, `subscriptions.granting` |
| Экран, кнопка, команда, диалог в боте | Гость `telegram.handlers.*` + `Menu(…)`, `Scene(…)` |
| Проверка всех входящих апдейтов | Гость `telegram.middleware.*` |
| Реакция на событие (аналитика, CRM, уведомления в Discord) | Модуль-приложение с подпиской на события |
| Другой товар (не VPN, например выделенный IP) | Приложение, которое исполняет свой `Item.Kind` из `billing.order.paid` |
| Свои данные о пользователе или подписке | Свои таблицы со ссылкой на `user_id`/`subscription_id`; чужие таблицы не меняются |
| Новая витрина (Mini App, сайт, другой мессенджер) | Приложение поверх доменных сервисов |
| Своя точка расширения | Новое приложение со своим namespace — сторонние модули расширяют уже его. Расширяемость рекурсивна, как в Caddy |
| Замена стандартного модуля | Своя сборка без стандартного модуля + своя реализация с тем же ID и интерфейсом-контрактом (правило 8, §4.4) |
| CLI-команда, HTTP-маршрут, миграции, тексты | `cmd.RegisterCommand`, `http.Mount`, `db.Migrate`, `locales/` |

**Осознанные ограничения**

1. **Модуль — это Go-код, вкомпилированный при сборке.** Установить модуль из админки без пересборки нельзя (в Caddy так же).
   Если это понадобится, пишется модуль-мост: `plugins.grpc` (hashicorp/go-plugin — отдельный процесс, любой язык)
   или `plugins.wasm` (wazero — песочница). Ядро при этом не меняется.
2. **Модули в процессе — доверенный код.** Роли Postgres ограничивают последствия ошибок, проверка возможностей
   (`capslock`) и минимизация секретов резко сужают, что может сделать вредоносный код, но полной гарантии внутри
   одного процесса не бывает. Непроверенный код подключается только через мосты из п. 1 — подробно в §14.1.
3. **Модуль расширяет систему только там, где хост предусмотрел точку расширения.** Если её нет, меняется хост-модуль
   (добавляется событие, конвейер или capability-интерфейс), но не ядро. Поэтому у ключевых решений домена
   (цена, заказ, выдача, продление, аккаунт в панели) точки расширения заложены с самого начала.
4. **Совместимость.** Сторонние модули зависят от Go-API пакетов-контрактов. Контракты версионируются по semver,
   `apidiff` в CI не пропускает незаметные ломающие изменения, а удаление идёт через deprecation. Новые возможности
   добавляются необязательными интерфейсами — они не ломают существующие модули.
5. **Среда выполнения.** Одна копия процесса и PostgreSQL. Модуль, которому нужно своё хранилище (например, ClickHouse
   для аналитики), приносит и настраивает его сам.
