---
name: a-neon-medium
description: Neon 4 (Medium Effort) — сбалансированный анализ (таймаут 20м стриминга, 5м idle). Команды: /a-neon-medium, /neon medium.
---

# Neon 4 — Medium Effort (/a-neon-medium)

Активируется при выборе `/a-neon-medium`, `/neon-medium` или вводе `/neon medium`. Вызывает модель **Neon 4** со сбалансированным бюджетом рассуждений (`effort="medium"`), серверным таймаутом стриминга 20 минут (1200с) и лимитом ожидания первого токена 5 минут (300с).

## Инструкция по выполнению

1. Извлеки текст запроса пользователя (удали префиксы команды).
2. Извлеки изображения (`@path`, `@clipboard`, base64).
3. Вызови инструмент `gemini_pro_web_ask` (или `neon_medium`) сервера `google-ai-pro-web`:
   - `model`: `"neon-4"`
   - `effort`: `"medium"`
   - `prompt`: текст запроса
   - `images`: список изображений
4. Выведи ответ модели со свернутым блоком Thinking Process (`<details><summary>Thinking Process</summary>...</details>`).
