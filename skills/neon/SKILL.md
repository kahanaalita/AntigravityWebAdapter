---
name: neon
description: Вызов модели Neon 4 (Google AI Pro Web) с выбором уровня рассуждений (low, medium, high) (таймаут 20м стриминга, 5м idle). Слэш-команда /neon.
metadata:
  icon: science
---

# Neon 4 Skill (/neon)

Этот скилл активируется при вводе команды `/neon` или обращении к веб-модели **Neon 4** (Google AI Pro). Единый серверный таймаут стриминга составляет 20 минут (1200с) с лимитом простоя потока 5 минут (300с) для всех уровней рассуждений.

## Поддержка режимов рассуждений (effort)

Пользователь может явно указать уровень рассуждений первым токеном:
- `/neon low <запрос>` — быстрый базовый режим (`effort="low"`).
- `/neon medium <запрос>` (или просто `/neon <запрос>`) — сбалансированный режим (`effort="medium"`).
- `/neon high <запрос>` — максимальная глубина логического анализа и синтеза (`effort="high"`).

## Инструкция по выполнению

1. Извлеки параметр `effort`, если указан в начале запроса (`low`, `medium`, `high`, `middle`). По умолчанию `effort="medium"`.
2. Извлеки пути к изображениям (токены `@path/to/img`, `@clipboard`, base64) в массив `images`.
3. Очисти текст запроса пользователя от уровня рассуждений.
4. Вызови инструмент `gemini_pro_web_ask` сервера `google-ai-pro-web`:
   - `model`: `"neon-4"`
   - `effort`: уровень рассуждений (`"low"`, `"medium"`, `"high"`)
   - `prompt`: текст запроса
   - `images`: список изображений
   
   *Fallback через бинарник, если MCP-клиент недоступен:*
   ```bash
   echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"antigravity","version":"1.0"}}}
   {"jsonrpc":"2.0","method":"notifications/initialized"}
   {"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"gemini_pro_web_ask","arguments":{"model":"neon-4","effort":"medium","prompt":"<запрос>"}}}' | /Users/kahanaalita/.gemini/antigravity/scratch/agy-gemini-web-mcp/bin/agy-gemini-web-mcp
   ```

5. Верни ответ модели, сохраняя блок рассуждений (`<details><summary>Thinking Process</summary>...</details>`).
