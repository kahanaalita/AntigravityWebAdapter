---
name: argon
description: Вызов модели Argon 4 (Google AI Pro Web) с выбором уровня рассуждений (low, medium, high). Слэш-команда /argon.
metadata:
  icon: science
---

# Argon 4 Skill (/argon)

Этот скилл активируется при вводе команды `/argon` или обращении к веб-модели **Argon 4** (Google AI Pro).

## Поддержка режимов рассуждений (effort)

Пользователь может явно указать уровень рассуждений первым токеном:
- `/argon low <запрос>` — быстрый базовый режим (`effort="low"`).
- `/argon medium <запрос>` (или просто `/argon <запрос>`) — сбалансированный режим (`effort="medium"`).
- `/argon high <запрос>` — максимальная глубина логического анализа и синтеза (`effort="high"`).

## Инструкция по выполнению

1. Извлеки параметр `effort`, если указан в начале запроса (`low`, `medium`, `high`, `middle`). По умолчанию `effort="medium"`.
2. Извлеки пути к изображениям (токены `@path/to/img`, `@clipboard`, base64) в массив `images`.
3. Очисти текст запроса пользователя от уровня рассуждений.
4. Вызови инструмент `gemini_pro_web_ask` сервера `google-ai-pro-web`:
   - `model`: `"argon-4"`
   - `effort`: уровень рассуждений (`"low"`, `"medium"`, `"high"`)
   - `prompt`: текст запроса
   - `images`: список изображений
   
   *Fallback через бинарник, если MCP-клиент недоступен:*
   ```bash
   echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"antigravity","version":"1.0"}}}
   {"jsonrpc":"2.0","method":"notifications/initialized"}
   {"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"gemini_pro_web_ask","arguments":{"model":"argon-4","effort":"medium","prompt":"<запрос>"}}}' | /Users/kahanaalita/.gemini/antigravity/scratch/agy-gemini-web-mcp/bin/agy-gemini-web-mcp
   ```

5. Верни ответ модели, сохраняя блок рассуждений (`<details><summary>Thinking Process</summary>...</details>`).
