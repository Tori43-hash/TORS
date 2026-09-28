# TORS

Модульный Telegram-бот для продажи VPN-подписок на Go. Первая панель — Remnawave, следующая — 3x-UI.
Архитектура «микроядро + модули» по образцу Caddy v2.

## Что уже есть

- [`docs/PLAN.md`](docs/PLAN.md) — архитектура и дорожная карта.
- [`web/ui-editor`](web/ui-editor) — редактор экранов бота на холсте с симулятором Telegram и экспортом `theme.json`.
- [`modules/telegram/theme`](modules/telegram/theme) — формат темы, проверки и шаблоны на Go (общие для бота и редактора).
