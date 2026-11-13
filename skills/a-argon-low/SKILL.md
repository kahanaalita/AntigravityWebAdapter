---
name: a-argon-low
description: Argon 4 (Low Effort) — быстрый ответ. Команды: /a-argon-low, /argon low.
---

# Argon 4 — Low Effort (/a-argon-low)

Активируется при выборе `/a-argon-low`, `/argon-low` или вводе `/argon low`. Вызывает модель **Argon 4** с быстрым базовым бюджетом рассуждений (`effort="low"`).

## Инструкция по выполнению

1. Извлеки текст запроса пользователя (удали префиксы команды).
2. Извлеки изображения (`@path`, `@clipboard`, base64).
3. Вызови инструмент `gemini_pro_web_ask` (или `argon_low`) сервера `google-ai-pro-web`:
   - `model`: `"argon-4"`
   - `effort`: `"low"`
   - `prompt`: текст запроса
   - `images`: список изображений
4. Выведи ответ модели.
