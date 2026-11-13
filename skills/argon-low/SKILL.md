---
name: argon-low
description: Argon 4 с низким уровнем рассуждений (low effort) для быстрых ответов. Команды: /argon-low, /argon low.
metadata:
  icon: flash_on
---

# Argon 4 — Low Effort (/argon-low)

Этот скилл активируется при выборе команды `/argon-low` или вводе `/argon low`. Использует модель **Argon 4** с быстрым базовым бюджетом рассуждений (`effort="low"`).

## Инструкция по выполнению

1. Извлеки текст запроса пользователя (удали префиксы `/argon-low` или `/argon low`).
2. Извлеки изображения (`@path`, `@clipboard`).
3. Вызови `gemini_pro_web_ask` (или `argon_low`) сервера `google-ai-pro-web`:
   - `model`: `"argon-4"`
   - `effort`: `"low"`
   - `prompt`: текст запроса
   - `images`: список изображений
4. Выведи результат модели.
