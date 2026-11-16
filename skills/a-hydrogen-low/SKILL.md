---
name: a-hydrogen-low
description: Hydrogen 4 (Low Effort) — быстрый базовый ответ (таймаут 20м стриминга, 5м idle). Команды: /a-hydrogen-low, /hydrogen low.
---

# Hydrogen 4 — Low Effort (/a-hydrogen-low)

Активируется при выборе `/a-hydrogen-low`, `/hydrogen-low` или вводе `/hydrogen low`. Вызывает модель **Hydrogen 4** с быстрым базовым бюджетом рассуждений (`effort="low"`), серверным таймаутом стриминга 20 минут (1200с) и лимитом ожидания первого токена 5 минут (300с).

## Инструкция по выполнению

1. Извлеки текст запроса пользователя (удали префиксы команды).
2. Извлеки изображения (`@path`, `@clipboard`, base64).
3. Вызови инструмент `gemini_pro_web_ask` (или `hydrogen_low`) сервера `google-ai-pro-web`:
   - `model`: `"hydrogen-4"`
   - `effort`: `"low"`
   - `prompt`: текст запроса
   - `images`: список изображений
4. Выведи ответ модели.
