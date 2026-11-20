---
name: a-neon-high
description: Neon 4 (High Effort) — максимальный синтез и глубокий анализ (таймаут 20м стриминга, 5м idle). Команды: /a-neon-high, /neon high.
---

# Neon 4 — High Effort (/a-neon-high)

Активируется при выборе `/a-neon-high`, `/neon-high` или вводе `/neon high`. Вызывает модель **Neon 4** с максимальным бюджетом рассуждений (`effort="high"`), серверным таймаутом стриминга 20 минут (1200с) и лимитом ожидания первого токена 5 минут (300с).

## Инструкция по выполнению

1. Извлеки текст запроса пользователя (удали префиксы команды).
2. Извлеки изображения (`@path`, `@clipboard`, base64).
3. Вызови инструмент `gemini_pro_web_ask` (или `neon_high`) сервера `google-ai-pro-web`:
   - `model`: `"neon-4"`
   - `effort`: `"high"`
   - `prompt`: текст запроса
   - `images`: список изображений
4. Выведи ответ модели со свернутым блоком Thinking Process (`<details><summary>Thinking Process</summary>...</details>`).
