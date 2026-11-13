# Руководство по интеграции AntigravityWebAdapter с DeepSeek Harness (DSH)

В данном руководстве описано подключение **AntigravityWebAdapter** к среде **DeepSeek Harness (DSH)** для прямого вызова веб-моделей Google Gemini (Hydrogen 4 и Argon 4) с поддержкой глубоких рассуждений (Reasoning Effort) и мультимодального анализа изображений.

Благодаря архитектуре адаптера, **никаких изменений в исходный код вносить не нужно**. Подключение осуществляется исключительно через конфигурационные файлы DSH.

---

## Вариант 1. Интеграция через протокол MCP (Рекомендуемый)

DeepSeek Harness нативно поддерживает Model Context Protocol (MCP) через официальный модуль `@deepseek-ai/dsh-mcp-client`. При этом адаптер запускается в режиме `stdio` (`--mcp`).

### 1. Конфигурация в `settings.yaml`

Добавьте блок адаптера в секцию плагинов/MCP вашего профиля DeepSeek Harness (файл `settings.yaml` или `cordis.config.yml`):

```yaml
plugins:
  - id: mcp-gemini-web
    name: '@deepseek-ai/dsh-mcp-client'
    config:
      serverName: gemini_web
      transport: stdio
      command: /Users/kahanaalita/.gemini/antigravity/scratch/AntigravityWebAdapter/bin/gemini-gateway
      args:
        - '--mcp'
        - '--base'
        - '/Users/kahanaalita/.gemini/antigravity/scratch/AntigravityWebAdapter/runtime'
      env:
        CHROME_PATH: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
```

### 2. Доступные инструменты модели

После инициализации агенту DeepSeek Harness автоматически становятся доступны следующие функции с префиксом пространства имен:

1. **`ask_gemini_web`**:
   - `prompt` *(строка, обязательно)* — текст запроса к модели.
   - `model` *(строка)* — целевая модель: `"4 Hydrogen"`, `"4 Argon"`, `"3.5 Flash-Lite"`, `"3.1 Pro"`. По умолчанию: `"4 Hydrogen"`.
   - `effort` *(строка)* — глубина рассуждений: `"low"`, `"medium"`, `"high"`.
   - `image_paths` *(массив строк)* — пути к локальным файлам изображений для анализа.
2. **`gemini_web_status`**:
   - Проверка активности веб-сессии Google Gemini и авторизации пользователя.

---

## Вариант 2. Интеграция через OpenAI-совместимый HTTP/SSE шлюз

Если ваш профиль DeepSeek Harness использует стандартный REST/SSE клиент провайдеров моделей (OpenAI API spec), адаптер может работать как локальный фоновый шлюз.

### 1. Запуск шлюза

Запустите бинарник адаптера в режиме HTTP-сервера:

```bash
cd /Users/kahanaalita/.gemini/antigravity/scratch/AntigravityWebAdapter
./bin/gemini-gateway --port 5690 --base ./runtime
```

*(Для автоматического запуска в фоне можно использовать `systemd`, `launchd` на macOS или `pm2`)*.

### 2. Конфигурация провайдера в DeepSeek Harness

В конфигурации провайдеров моделей укажите локальный адрес шлюза:

```yaml
providers:
  gemini-web:
    type: openai
    baseURL: 'http://127.0.0.1:5690/v1'
    apiKey: 'sk-no-key-required'
    models:
      - id: gemini-4-hydrogen-high
        name: 'Gemini 4 Hydrogen (High Reasoning)'
      - id: gemini-4-hydrogen-medium
        name: 'Gemini 4 Hydrogen (Medium Reasoning)'
      - id: gemini-4-argon-high
        name: 'Gemini 4 Argon (Code & Analysis)'
      - id: gemini-3.5-flash-lite
        name: 'Gemini 3.5 Flash-Lite'
```

---

## Требования к окружению и авторизация

1. **Google Chrome**:
   - Адаптер автоматически обнаруживает установленный браузер Google Chrome и системный прокси (V2Ray, Clash, SOCKS5 на портах 10808, 7890 и др.).
2. **Первичный вход**:
   - При первом запуске откроется окно Chrome для авторизации в Google Gemini (`https://gemini.google.com/app`). 
   - Авторизационные куки и профиль кэшируются в директории `runtime/chrome_profile` и сохраняются между сессиями.
3. **Безопасность**:
   - Chrome запускается с флагом `--disable-blink-features=AutomationControlled` для предотвращения детекта автоматизации.
