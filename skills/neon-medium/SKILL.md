---
name: neon-medium
description: Neon 4 со сбалансированным уровнем рассуждений (medium effort) (таймаут 20м стриминга, 5м idle). Команды: /neon-medium, /neon medium.
metadata:
  icon: science
---

# Neon 4 — Medium Effort (/neon-medium)

Этот скилл активируется при выборе команды `/neon-medium` или вводе `/neon medium`. Использует модель **Neon 4** со сбалансированным бюджетом рассуждений (`effort="medium"`), серверным таймаутом стриминга 20 минут (1200с) и лимитом ожидания первого токена 5 минут (300с).

## Инструкция по выполнению

1. Извлеки текст запроса пользователя (удали префиксы `/neon-medium` или `/neon medium`).
2. Извлеки изображения (`@path`, `@clipboard`).
3. Вызови `gemini_pro_web_ask` (или `neon_medium`) сервера `google-ai-pro-web`:
   - `model`: `"neon-4"`
   - `effort`: `"medium"`
   - `prompt`: текст запроса
   - `images`: список изображений
4. Выведи результат модели со свернутым блоком процесса рассуждений.
