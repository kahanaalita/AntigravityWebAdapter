---
name: hydrogen-middle
description: Hydrogen 4 со сбалансированным уровнем рассуждений (middle effort) (таймаут 20м стриминга, 5м idle). Команды: /hydrogen-middle, /hydrogen middle.
metadata:
  icon: science
---

# Hydrogen 4 — Middle Effort (/hydrogen-middle)

Этот скилл активируется при выборе команды `/hydrogen-middle` или вводе `/hydrogen middle`. Использует модель **Hydrogen 4** со сбалансированным бюджетом рассуждений (`effort="middle"`), серверным таймаутом стриминга 20 минут (1200с) и лимитом ожидания первого токена 5 минут (300с).

## Инструкция по выполнению

1. Извлеки текст запроса пользователя (удали префиксы `/hydrogen-middle` или `/hydrogen middle`).
2. Извлеки изображения (`@path`, `@clipboard`).
3. Вызови `gemini_pro_web_ask` (или `hydrogen_middle`) сервера `google-ai-pro-web`:
   - `model`: `"hydrogen-4"`
   - `effort`: `"middle"`
   - `prompt`: текст запроса
   - `images`: список изображений
4. Выведи результат со свернутым блоком процесса рассуждений.
