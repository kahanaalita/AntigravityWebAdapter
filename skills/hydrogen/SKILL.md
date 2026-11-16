---
name: hydrogen
description: Вызов модели Hydrogen 4 (Google AI Pro Web) с выбором уровня рассуждений (low, middle, high) (таймаут 20м стриминга, 5м idle). Слэш-команда /hydrogen.
metadata:
  icon: science
---

# Hydrogen 4 Skill (/hydrogen)

Этот скилл активируется при вводе команды `/hydrogen` или обращении к веб-модели **Hydrogen 4** (Google AI Pro). Единый серверный таймаут стриминга составляет 20 минут (1200с) с лимитом простоя потока 5 минут (300с) для всех уровней рассуждений.

## Поддержка режимов рассуждений (effort)

Пользователь может явно указать уровень рассуждений первым токеном:
- `/hydrogen low <запрос>` — быстрый базовый режим (`effort="low"`).
- `/hydrogen middle <запрос>` (или просто `/hydrogen <запрос>`) — сбалансированный режим (`effort="middle"`).
- `/hydrogen high <запрос>` — максимальная глубина аналитического поиска (`effort="high"`).

## Инструкция по выполнению

1. Извлеки параметр `effort`, если указан в начале запроса (`low`, `middle`, `high`, `medium`). По умолчанию `effort="middle"`.
2. Извлеки пути к изображениям (`@path/to/img`, `@clipboard`, base64) в массив `images`.
3. Очисти текст запроса пользователя от названия уровня рассуждений.
4. Вызови инструмент `gemini_pro_web_ask` сервера `google-ai-pro-web`:
   - `model`: `"hydrogen-4"`
   - `effort`: уровень рассуждений (`"low"`, `"middle"`, `"high"`)
   - `prompt`: текст запроса
   - `images`: список изображений
   
   *Fallback через бинарник, если MCP-клиент недоступен:*
   ```bash
   echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"antigravity","version":"1.0"}}}
   {"jsonrpc":"2.0","method":"notifications/initialized"}
   {"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"gemini_pro_web_ask","arguments":{"model":"hydrogen-4","effort":"middle","prompt":"<запрос>"}}}' | /Users/kahanaalita/.gemini/antigravity/scratch/agy-gemini-web-mcp/bin/agy-gemini-web-mcp
   ```

5. Верни ответ модели, сохраняя блок рассуждений (`<details><summary>Thinking Process</summary>...</details>`).
