---
name: hydrogen-high
description: Hydrogen 4 с максимальным уровнем рассуждений (high effort) для глубокого анализа (таймаут 20м стриминга, 5м idle). Команды: /hydrogen-high, /hydrogen high.
metadata:
  icon: psychology
---

# Hydrogen 4 — High Effort (/hydrogen-high)

Этот скилл активируется при выборе команды `/hydrogen-high` или вводе `/hydrogen high`. Использует модель **Hydrogen 4** с максимальным бюджетом рассуждений (`effort="high"`), серверным таймаутом стриминга 20 минут (1200с) и лимитом ожидания первого токена 5 минут (300с).

## Инструкция по выполнению

1. Извлеки текст запроса пользователя (удали префиксы `/hydrogen-high` или `/hydrogen high`).
2. Извлеки изображения (`@path`, `@clipboard`).
3. Вызови `gemini_pro_web_ask` (или `hydrogen_high`) сервера `google-ai-pro-web`:
   - `model`: `"hydrogen-4"`
   - `effort`: `"high"`
   - `prompt`: текст запроса
   - `images`: список изображений
4. Выведи результат со свернутым блоком процесса рассуждений.
