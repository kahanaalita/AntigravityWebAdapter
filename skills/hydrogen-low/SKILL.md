---
name: hydrogen-low
description: Hydrogen 4 с базовым уровнем рассуждений (low effort) для быстрых ответов. Команды: /hydrogen-low, /hydrogen low.
metadata:
  icon: flash_on
---

# Hydrogen 4 — Low Effort (/hydrogen-low)

Этот скилл активируется при выборе команды `/hydrogen-low` или вводе `/hydrogen low`. Использует модель **Hydrogen 4** с быстрым базовым бюджетом рассуждений (`effort="low"`).

## Инструкция по выполнению

1. Извлеки текст запроса пользователя (удали префиксы `/hydrogen-low` или `/hydrogen low`).
2. Извлеки изображения (`@path`, `@clipboard`).
3. Вызови `gemini_pro_web_ask` (или `hydrogen_low`) сервера `google-ai-pro-web`:
   - `model`: `"hydrogen-4"`
   - `effort`: `"low"`
   - `prompt`: текст запроса
   - `images`: список изображений
4. Выведи результат.
