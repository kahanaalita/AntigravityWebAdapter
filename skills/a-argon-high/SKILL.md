---
name: a-argon-high
description: Argon 4 (High Effort) — максимальный синтез и глубокий анализ. Команды: /a-argon-high, /argon high.
---

# Argon 4 — High Effort (/a-argon-high)

Активируется при выборе `/a-argon-high`, `/argon-high` или вводе `/argon high`. Вызывает модель **Argon 4** с максимальным бюджетом рассуждений (`effort="high"`).

## Инструкция по выполнению

1. Извлеки текст запроса пользователя (удали префиксы команды).
2. Извлеки изображения (`@path`, `@clipboard`, base64).
3. Вызови инструмент `gemini_pro_web_ask` (или `argon_high`) сервера `google-ai-pro-web`:
   - `model`: `"argon-4"`
   - `effort`: `"high"`
   - `prompt`: текст запроса
   - `images`: список изображений
4. Выведи ответ модели со свернутым блоком Thinking Process (`<details><summary>Thinking Process</summary>...</details>`).
