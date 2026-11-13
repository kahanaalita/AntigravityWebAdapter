---
name: a-hydrogen-middle
description: Hydrogen 4 (Middle Effort) — сбалансированный анализ. Команды: /a-hydrogen-middle, /hydrogen middle.
---

# Hydrogen 4 — Middle Effort (/a-hydrogen-middle)

Активируется при выборе `/a-hydrogen-middle`, `/hydrogen-middle` или вводе `/hydrogen middle`. Вызывает модель **Hydrogen 4** со сбалансированным бюджетом рассуждений (`effort="middle"`).

## Инструкция по выполнению

1. Извлеки текст запроса пользователя (удали префиксы команды).
2. Извлеки изображения (`@path`, `@clipboard`, base64).
3. Вызови инструмент `gemini_pro_web_ask` (или `hydrogen_middle`) сервера `google-ai-pro-web`:
   - `model`: `"hydrogen-4"`
   - `effort`: `"middle"`
   - `prompt`: текст запроса
   - `images`: список изображений
4. Выведи ответ модели со свернутым блоком Thinking Process (`<details><summary>Thinking Process</summary>...</details>`).
