---
name: argon-medium
description: Argon 4 со сбалансированным уровнем рассуждений (medium effort). Команды: /argon-medium, /argon medium.
metadata:
  icon: science
---

# Argon 4 — Medium Effort (/argon-medium)

Этот скилл активируется при выборе команды `/argon-medium` или вводе `/argon medium`. Использует модель **Argon 4** со сбалансированным бюджетом рассуждений (`effort="medium"`).

## Инструкция по выполнению

1. Извлеки текст запроса пользователя (удали префиксы `/argon-medium` или `/argon medium`).
2. Извлеки изображения (`@path`, `@clipboard`).
3. Вызови `gemini_pro_web_ask` (или `argon_medium`) сервера `google-ai-pro-web`:
   - `model`: `"argon-4"`
   - `effort`: `"medium"`
   - `prompt`: текст запроса
   - `images`: список изображений
4. Выведи результат модели со свернутым блоком процесса рассуждений.
