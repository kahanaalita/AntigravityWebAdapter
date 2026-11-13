---
name: argon-high
description: Argon 4 с максимальным уровнем рассуждений (high effort) для глубокого анализа. Команды: /argon-high, /argon high.
metadata:
  icon: psychology
---

# Argon 4 — High Effort (/argon-high)

Этот скилл активируется при выборе команды `/argon-high` или вводе `/argon high`. Использует модель **Argon 4** с максимальным бюджетом рассуждений (`effort="high"`).

## Инструкция по выполнению

1. Извлеки текст запроса пользователя (удали префиксы `/argon-high` или `/argon high`).
2. Извлеки изображения (`@path`, `@clipboard`).
3. Вызови `gemini_pro_web_ask` (или `argon_high`) сервера `google-ai-pro-web`:
   - `model`: `"argon-4"`
   - `effort`: `"high"`
   - `prompt`: текст запроса
   - `images`: список изображений
4. Выведи результат со свернутым блоком процесса рассуждений.
