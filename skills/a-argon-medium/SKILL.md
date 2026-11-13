---
name: a-argon-medium
description: Argon 4 (Medium Effort) — сбалансированный анализ. Команды: /a-argon-medium, /argon medium.
---

# Argon 4 — Medium Effort (/a-argon-medium)

Активируется при выборе `/a-argon-medium`, `/argon-medium` или вводе `/argon medium`. Вызывает модель **Argon 4** со сбалансированным бюджетом рассуждений (`effort="medium"`).

## Инструкция по выполнению

1. Извлеки текст запроса пользователя (удали префиксы команды).
2. Извлеки изображения (`@path`, `@clipboard`, base64).
3. Вызови инструмент `gemini_pro_web_ask` (или `argon_medium`) сервера `google-ai-pro-web`:
   - `model`: `"argon-4"`
   - `effort`: `"medium"`
   - `prompt`: текст запроса
   - `images`: список изображений
4. Выведи ответ модели со свернутым блоком Thinking Process (`<details><summary>Thinking Process</summary>...</details>`).
