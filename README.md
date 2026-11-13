# `agy-gemini-web-mcp`

**Адаптер между Google Antigravity и веб-версией Google AI Pro (модели Hydrogen 4 и Argon 4)**

`agy-gemini-web-mcp` — высокопроизводительный локальный MCP-сервер (Go) со `stdio`-транспортом, обеспечивающий интеграцию сред Antigravity с веб-протоколом Google AI Pro. Поддерживает управление глубиной рассуждений (Reasoning Effort), двухфазную загрузку мультимодальных вложений через Scotty Resumable Upload, фоновую ротацию сессионных кук и автоматический CDP-захват авторизации из локального Chrome.

---

## Возможности

1. **Модели и уровни рассуждения:**
   - **Hydrogen 4**: `/hydrogen low` (120s), `/hydrogen middle` (300s, по умолчанию), `/hydrogen high` (600s).
   - **Argon 4**: `/argon low` (120s), `/argon medium` (300s, по умолчанию), `/argon high` (600s).
   - Мягкий фоллбэк и кросс-алиасинг (`middle` ↔ `medium`).
2. **Прозрачная мультимодальность:**
   - Чтение путей из текста промпта (`@./path/to/image.png`).
   - Автозахват изображения из системного буфера обмена (`@clipboard` / `clipboard://`).
   - Base64 / Data URI (`data:image/png;base64,...`).
   - Параллельная загрузка изображений через протокол Google Scotty Resumable Upload (`content-push.googleapis.com/upload/`).
3. **Обход ограничений и безопасность сессии:**
   - Использование `bogdanfinn/tls-client` с реализацией Chrome TLS/JA3/JA4 отпечатка и HTTP/2 фреймов.
   - Извлечение CSRF (`SNlM0e`), `cfb2h` и `FdrFJe` из `WIZ_global_data`.
   - Фоновая ротация токена `__Secure-1PSIDTS` каждые 8 минут.
   - CDP Harvester: автоматический забор актуальных сессионных токенов из запущенного Chrome (`--remote-debugging-port=9222`).
4. **Контекст диалогов:**
   - Кэширование `conversation_id`, `response_id`, `choice_id` для продолжения диалогов без потери контекста.
   - Вывод рассуждений (*Thinking Trace*) в компактном сворачиваемом блоке `<details><summary>Thinking Process</summary>...</details>`.

---

## Быстрый старт

### 1. Сборка бинарника

```bash
cd /Users/kahanaalita/.gemini/antigravity/scratch/agy-gemini-web-mcp
go build -o bin/agy-gemini-web-mcp ./cmd/server
```

### 2. Установка слэш-команд в Antigravity

Команда автоматически копирует `hydrogen.toml` и `argon.toml` в `.antigravity/commands/` и `~/.antigravity/commands/`:

```bash
./bin/agy-gemini-web-mcp -install-commands
```

### 3. Подключение в Antigravity (`settings.json` или `mcp_config.json`)

```json
{
  "mcpServers": {
    "google-ai-pro-web": {
      "command": "/Users/kahanaalita/.gemini/antigravity/scratch/agy-gemini-web-mcp/bin/agy-gemini-web-mcp",
      "args": ["--cookie-store", "/Users/kahanaalita/.config/agy-gemini-web/cookies.json"],
      "env": {
        "CHROME_CDP_URL": "http://127.0.0.1:9222",
        "DEFAULT_HYDROGEN_EFFORT": "middle",
        "DEFAULT_ARGON_EFFORT": "medium"
      },
      "timeoutSeconds": 600
    }
  }
}
```

---

## Настройка сессионных кук

Файл кук `~/.config/agy-gemini-web/cookies.json` поддерживает 3 формата:
1. **JSON-объект:**
   ```json
   {
     "__Secure-1PSID": "...",
     "__Secure-1PSIDTS": "..."
   }
   ```
2. **Экспорт расширений браузера (Cookie-Editor):**
   ```json
   [
     {"name": "__Secure-1PSID", "value": "..."},
     {"name": "__Secure-1PSIDTS", "value": "..."}
   ]
   ```
3. **Автозахват через Chrome CDP:**
   Запустите Chrome с флагом отладки:
   ```bash
   /Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome --remote-debugging-port=9222
   ```
   Сервер сам заберет авторизационные токены при старте или при вызове:
   ```bash
   ./bin/agy-gemini-web-mcp -harvest-cookies
   ```

---

## Использование в чате Antigravity

* **/hydrogen:**
  ```text
  /hydrogen middle Проанализируй этот код архитектуры
  /hydrogen high @screens/mockup.png Что улучшить в UX?
  /hydrogen low Быстро сформулируй 3 тезиса
  ```

* **/argon:**
  ```text
  /argon high @clipboard Сделай детальный ревью дизайна из буфера обмена
  /argon medium Объясни разницу между алгоритмами
  ```

* **Прямой вызов инструментов субагентами:**
  Доступны как единый инструмент `gemini_pro_web_ask`, так и 6 атомарных алиасов:
  `hydrogen_low`, `hydrogen_middle`, `hydrogen_high`, `argon_low`, `argon_medium`, `argon_high`.
