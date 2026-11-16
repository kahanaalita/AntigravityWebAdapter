# Полный технический аудит и план исправления проблемных мест репозитория agy-gemini-web-mcp

**Дата аудита:** 16 ноября 2026 г.  
**Целевой репозиторий:** `/Users/kahanaalita/.gemini/antigravity/scratch/agy-gemini-web-mcp`  
**Архитектура:** Go MCP-сервер (Model Context Protocol) для взаимодействия с веб-версией Google Gemini (Hydrogen 4 и Argon 4) через `boq_assistant-bard-web-server` RPC (`StreamGenerate`), CDP-мост (Chrome DevTools Protocol), Scotty resumable upload и ротацию сессий.

---

## Сводная таблица найденных проблем и уязвимостей

| № | Модуль | Проблема / Дефект | Критичность | Последствия |
|---|---|---|---|---|
| 1 | `internal/media/resolver.go` | Жесткий захват `@clipboard` регулярным выражением в тексте промпта без валидации наличия изображения в буфере ОС | 🔴 **Critical** | Аварийный сбой (`osascript exit status 1`) всего запроса к модели, если пользователь упомянул слово `@clipboard` в тексте, а в буфере нет картинки. |
| 2 | `internal/auth/cdp_bridge.go` | Небезопасный выбор WebSocket target и отсутствие проверки домена в CDP Bridge | 🔴 **Critical** | При подключении к не-Gemini вкладке (например, `chrome://newtab` или сторонний сайт) `fetch()` блокируется политиками CORS/CSP, а запрос падает без куки авторизации. |
| 3 | `internal/auth/cdp_bridge.go` & `cdp_eval.go` | Игнорирование HTTP-статуса ответа (`r.ok`) в браузере и потеря `exceptionDetails` CDP | 🔴 **Critical** | Ошибки HTTP 401/403/500 возвращаются как "успешный" HTML-текст в парсер потока; JS-исключения маскируются в неинформативное `empty response from browser fetch`. |
| 4 | `internal/geminiweb/parser.go` | Тихий сбой с возвратом `nil`-ошибки при пустом или поврежденном потоке ответов | 🔴 **Critical** | При получении HTML-ошибки или разрыве соединения `ParseStream` возвращает пустой результат без ошибки, и пользователь получает пустой ответ. |
| 5 | `internal/geminiweb/parser.go` | Перезапись буфера текста при обходе кандидатов и потеря составных частей ответа (`textB.Reset()`) | 🟠 **High** | При получении нескольких черновиков (drafts) выбирается последний черновик вместо основного (индекс 0); при наличии нескольких текстовых частей в кандидате предыдущие затираются. |
| 6 | `internal/auth/auto_harvest.go` & `rotator.go` | Спам пустыми окнами Chrome (`/tmp/agy-chrome-cdp-profile`) при фоновой ротации куки | 🟠 **High** | Запуск пустого временного профиля без авторизации Google приводит к циклическому открытию новых окон Chrome каждые 8 минут при истечении сессии. |
| 7 | `internal/auth/session.go` | Удержание Write Lock (`s.mu.Lock()`) во время долгих сетевых запросов | 🟠 **High** | Блокировка всех горутин, запрашивающих сессию или статус CDP, приводящая к каскадным таймаутам. |
| 8 | `internal/auth/session.go` | Ложное признание сессии невалидной при HTTP 302 локализации и отсутствие сохранения куки на диск | 🟠 **High** | Сброс сессии при безобидных редиректах на языковые страницы (`/app?hl=ru`); потеря обновленных в `client.Generate()` куки при перезапуске сервера. |
| 9 | `internal/mcp/handlers.go` & `slash_parser.go` | Ошибочное усечение начала промпта при совпадении первых слов с именем модели без слэша | 🟡 **Medium** | Промпты вроде *"argon is a noble gas..."* или *"hydrogen technology..."* лишаются первого слова, так как парсер принимает их за команду. |
| 10 | `internal/media/scotty.go` | Неполный парсинг `uploadRef` для новых форматов ответа Google Rupio (`customerSpecificInfo`) | 🟡 **Medium** | Ошибка загрузки изображений, если Google возвращает токен вложенности в новом JSON-формате без подстроки `/contrib_service/`. |
| 11 | `internal/media/resolver.go` | Дублирование загрузки одного и того же изображения из буфера обмена | 🟢 **Low** | Если пользователь указал `@clipboard` в промпте и передал его в массиве `images`, буфер считывается и загружается дважды. |

---

## 1. Модуль `internal/media/resolver.go`: Жесткий захват `@clipboard` и дублирование

### Причина проблемы
Регулярное выражение `reAtClip = regexp.MustCompile("@(clipboard)")` находит вхождение подстроки `@clipboard` в любом месте текста промпта. Метод `ResolveFromPromptAndList` сразу вызывает `ResolveClipboard()`. Если в системном буфере обмена macOS нет PNG-изображения, `osascript` возвращает ошибку `exit status 1`, из-за чего весь вызов `ResolveFromPromptAndList` аварийно завершается с ошибкой `resolve clipboard: osascript clipboard error: exit status 1`.
Даже если пользователь просто обсуждал код или синтаксис (например, писал про `@clipboard`), запрос полностью ломался.

### Реализация исправления
1. Заменять `@clipboard` только если это изолированный токен-директива `(?:^|\s)@(clipboard)(?:\s|$)`.
2. Если в буфере нет картинки, не ронять весь запрос, а пропускать директиву (или заменять информативной плашкой) и позволять текстовому запросу выполниться.
3. Внедрить дедупликацию по хешу данных (`sha256`), чтобы исключить двойную загрузку одной и той же картинки.

#### Полный код исправления (`internal/media/resolver.go`):
```go
package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reImgPath = regexp.MustCompile(`(?:^|\s)@([^\s"']+\.(png|jpg|jpeg|webp|gif|heic))(?:\s|$)`)
	reAtClip  = regexp.MustCompile(`(?:^|\s)@(clipboard)(?:\s|$)`)
)

type ResolvedImage struct {
	FileName string
	MimeType string
	Data     []byte
}

type ImageResolver struct {
	WorkDir string
}

func NewImageResolver(workDir string) *ImageResolver {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	return &ImageResolver{WorkDir: workDir}
}

func (r *ImageResolver) ResolveFromPromptAndList(prompt string, explicitPaths []string) (string, []ResolvedImage, error) {
	var images []ResolvedImage
	seenHashes := make(map[string]bool)

	addImage := func(img *ResolvedImage) {
		if img == nil || len(img.Data) == 0 {
			return
		}
		hash := sha256.Sum256(img.Data)
		hashStr := hex.EncodeToString(hash[:])
		if !seenHashes[hashStr] {
			seenHashes[hashStr] = true
			images = append(images, *img)
		}
	}

	cleanedPrompt := prompt

	// 1. Обработка @clipboard в тексте промпта
	if reAtClip.MatchString(cleanedPrompt) {
		clipImg, err := r.ResolveClipboard()
		if err == nil && clipImg != nil {
			addImage(clipImg)
			cleanedPrompt = reAtClip.ReplaceAllString(cleanedPrompt, " ")
		} else {
			// НЕ роняем запрос! Если буфер пуст или не содержит картинки, удаляем токен
			// и продолжаем обработку текстового запроса
			cleanedPrompt = reAtClip.ReplaceAllString(cleanedPrompt, " ")
		}
	}

	// 2. Обработка путей @file в тексте промпта
	cleanedPrompt = reImgPath.ReplaceAllStringFunc(cleanedPrompt, func(match string) string {
		sub := reImgPath.FindStringSubmatch(match)
		if len(sub) <= 1 {
			return match
		}
		p := strings.TrimSpace(sub[1])
		target := p
		if !filepath.IsAbs(target) && r.WorkDir != "" {
			target = filepath.Join(r.WorkDir, target)
		}

		info, err := os.Stat(target)
		if err != nil || info.IsDir() {
			// Файл не найден или является каталогом — оставляем как обычный текст
			return match
		}

		img, err := r.ResolveFile(p)
		if err == nil && img != nil {
			addImage(img)
			return " "
		}
		return match
	})

	// 3. Обработка явного списка paths/images
	for _, p := range explicitPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p == "@clipboard" || p == "clipboard://" {
			clipImg, err := r.ResolveClipboard()
			if err == nil && clipImg != nil {
				addImage(clipImg)
			}
			continue
		}
		if strings.HasPrefix(p, "data:image/") {
			img, err := r.ResolveDataURI(p)
			if err != nil {
				return "", nil, fmt.Errorf("resolve data uri: %w", err)
			}
			addImage(img)
			continue
		}

		cleanPath := strings.TrimPrefix(p, "@")
		img, err := r.ResolveFile(cleanPath)
		if err != nil {
			return "", nil, fmt.Errorf("resolve image %q: %w", cleanPath, err)
		}
		addImage(img)
	}

	cleanedPrompt = strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(cleanedPrompt, " "))
	return cleanedPrompt, images, nil
}

func (r *ImageResolver) ResolveFile(filePath string) (*ResolvedImage, error) {
	target := filePath
	if !filepath.IsAbs(target) && r.WorkDir != "" {
		target = filepath.Join(r.WorkDir, target)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", target, err)
	}

	mimeType := detectMimeType(data, target)
	return &ResolvedImage{
		FileName: filepath.Base(target),
		MimeType: mimeType,
		Data:     data,
	}, nil
}

func (r *ImageResolver) ResolveDataURI(uri string) (*ResolvedImage, error) {
	comma := strings.Index(uri, ",")
	if comma < 0 {
		return nil, errors.New("invalid data URI: missing comma separator")
	}

	header := uri[:comma]
	payload := uri[comma+1:]

	mime := "image/png"
	if strings.HasPrefix(header, "data:") {
		semi := strings.Index(header, ";")
		if semi > 5 {
			mime = header[5:semi]
		}
	}

	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("decode base64 data URI: %w", err)
	}

	return &ResolvedImage{
		FileName: "image.png",
		MimeType: mime,
		Data:     data,
	}, nil
}

func (r *ImageResolver) ResolveClipboard() (*ResolvedImage, error) {
	if path, err := exec.LookPath("pngpaste"); err == nil {
		cmd := exec.Command(path, "-")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil && out.Len() > 0 {
			return &ResolvedImage{
				FileName: "clipboard.png",
				MimeType: "image/png",
				Data:     out.Bytes(),
			}, nil
		}
	}

	script := `set clip to (the clipboard as «class PNGf»)`
	cmd := exec.Command("/usr/bin/osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("osascript clipboard: %w", err)
	}

	raw := strings.TrimSpace(string(out))
	if strings.HasPrefix(raw, "«data PNGf") && strings.HasSuffix(raw, "»") {
		hexStr := raw[len("«data PNGf") : len(raw)-len("»")]
		data, err := hex.DecodeString(hexStr)
		if err != nil {
			return nil, fmt.Errorf("decode clipboard hex: %w", err)
		}
		return &ResolvedImage{
			FileName: "clipboard.png",
			MimeType: "image/png",
			Data:     data,
		}, nil
	}

	return nil, errors.New("no image in clipboard")
}

func detectMimeType(data []byte, filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".heic":
		return "image/heic"
	}

	if len(data) > 512 {
		return http.DetectContentType(data[:512])
	}
	if len(data) > 0 {
		return http.DetectContentType(data)
	}
	return "image/png"
}
```

---

## 2. Модуль `internal/auth/cdp_bridge.go` & `cdp_eval.go`: Таргетирование вкладок, обработка ошибок и WebSocket

### Причина проблемы
1. `resolveWebSocketDebuggerURL` ищет вкладку с `gemini.google.com`, но при её отсутствии переключается на *любую первую попавшуюся страницу* (`t.Type == "page"`). Если открыта вкладка `chrome://newtab`, `youtube.com` или пустой таб, в ней запускается `fetch('https://gemini.google.com/...')`. Браузер мгновенно блокирует такой запрос по CORS/CSP, а в контексте чужой вкладки отсутствуют куки Gemini и объект `window.WIZ_global_data`.
2. Вызов `fetch()` внутри браузера не проверяет `r.ok`. Если сервер Google возвращает HTTP 401, 403 или 500, метод `r.text()` возвращает HTML-код страницы ошибки, который мост считает валидным ответом!
3. `cdpEvalResponse` не парсит поле `exceptionDetails`. Если JavaScript в промисе падает (например, `TypeError: Failed to fetch`), CDP возвращает объект ошибки, `Result.Value` равен пустой строке, а код выбрасывает бесполезное `empty response from browser fetch` вместо реальной причины сбоя.
4. На каждый вызов открывается и закрывается новое WebSocket-соединение.

### Реализация исправления
1. Строго проверять, что целевая страница — именно `gemini.google.com`. Если вкладка не найдена — создавать или навигировать страницу в контекст `https://gemini.google.com/app`.
2. Внедрить проверку `response.ok` и `response.status` непосредственно в JS-обертку: выбрасывать исключение с деталями статуса при `!r.ok`.
3. Парсить `exceptionDetails` и выводить реальный стек ошибки JS.
4. Безопасно кодировать тело запроса через `json.Marshal`, исключая ошибки интерполяции спецсимволов.

#### Полный код исправления (`internal/auth/cdp_bridge.go`):
```go
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type BridgeGenerateParams struct {
	Body            string
	Bl              string
	Sid             string
	ExtensionHeader string
	Timeout         time.Duration
}

func (s *SessionManager) BridgeGenerate(ctx context.Context, params BridgeGenerateParams) (string, error) {
	endpoint := s.cdpURL
	if endpoint == "" {
		endpoint = "http://127.0.0.1:9222"
	}

	// Строгий резолв: привязываемся ТОЛЬКО к вкладке gemini.google.com
	wsURL, err := resolveGeminiTargetURL(ctx, endpoint)
	if err != nil {
		return "", fmt.Errorf("resolve gemini cdp page target: %w", err)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return "", fmt.Errorf("dial cdp ws: %w", err)
	}
	defer conn.Close()

	stopWatcher := make(chan struct{})
	defer close(stopWatcher)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stopWatcher:
		}
	}()

	bodyJSON, err := json.Marshal(params.Body)
	if err != nil {
		return "", fmt.Errorf("marshal params body: %w", err)
	}

	jsExpr := fmt.Sprintf(`(async () => {
		const at = window.WIZ_global_data?.SNlM0e || '';
		const bl = %q || window.WIZ_global_data?.cfb2h || '';
		const sid = %q || window.WIZ_global_data?.FdrFJe || '';
		const extHdr = %q;
		const reqID = Math.floor(100000 + Math.random() * 900000);
		const url = 'https://gemini.google.com/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate?bl=' + bl + '&_reqid=' + reqID + '&rt=c&f.sid=' + sid;
		
		const headers = {
			'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8',
			'X-Same-Domain': '1'
		};
		if (extHdr) {
			headers['x-goog-ext-525001261-jspb'] = extHdr;
		}

		const r = await fetch(url, {
			method: 'POST',
			headers: headers,
			body: %s,
			credentials: 'include'
		});

		if (!r.ok) {
			const errText = await r.text();
			throw new Error('HTTP ' + r.status + ' (' + r.statusText + '): ' + errText.slice(0, 300));
		}

		return await r.text();
	})()`, params.Bl, params.Sid, params.ExtensionHeader, string(bodyJSON))

	req := cdpEvalRequest{
		ID:     201,
		Method: "Runtime.evaluate",
		Params: map[string]any{
			"expression":    jsExpr,
			"awaitPromise":  true,
			"returnByValue": true,
		},
	}

	if err := conn.WriteJSON(req); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("write Runtime.evaluate: %w", err)
	}

	timeout := params.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	_ = conn.SetReadDeadline(time.Now().Add(timeout + 15*time.Second))

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		var resp cdpEvalResponse
		if err := conn.ReadJSON(&resp); err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("read cdp eval response: %w", err)
		}

		if resp.ID == 201 {
			if resp.Error != nil {
				return "", fmt.Errorf("cdp protocol error: %s (code %d)", resp.Error.Message, resp.Error.Code)
			}
			if resp.Result.ExceptionDetails != nil {
				ex := resp.Result.ExceptionDetails
				desc := ex.Text
				if ex.Exception != nil && ex.Exception.Description != "" {
					desc = ex.Exception.Description
				}
				return "", fmt.Errorf("browser js exception at line %d: %s", ex.LineNumber, desc)
			}
			val := resp.Result.Result.Value
			if val == "" {
				return "", errors.New("empty response text from browser fetch")
			}
			return val, nil
		}
	}
}

// resolveGeminiTargetURL строго находит вкладку gemini.google.com
func resolveGeminiTargetURL(ctx context.Context, endpoint string) (string, error) {
	clean := strings.TrimRight(endpoint, "/")
	targetURL := clean + "/json"

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("connect to CDP %s: %w", targetURL, err)
	}
	defer resp.Body.Close()

	var targets []struct {
		Type                 string `json:"type"`
		URL                  string `json:"url"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return "", fmt.Errorf("decode cdp targets: %w", err)
	}

	for _, t := range targets {
		if t.Type == "page" && strings.Contains(t.URL, "gemini.google.com") && t.WebSocketDebuggerURL != "" {
			return t.WebSocketDebuggerURL, nil
		}
	}

	return "", errors.New("no active gemini.google.com tab found in Chrome via CDP. Please open https://gemini.google.com in Chrome")
}
```

#### Дополнение в `internal/auth/cdp_eval.go`:
```go
type cdpEvalResponse struct {
	ID     int `json:"id"`
	Result struct {
		Result struct {
			Type        string `json:"type"`
			Subtype     string `json:"subtype"`
			Value       string `json:"value"`
			Description string `json:"description"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text         string `json:"text"`
			LineNumber   int    `json:"lineNumber"`
			ColumnNumber int    `json:"columnNumber"`
			Exception    *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}
```

---

## 3. Модуль `internal/geminiweb/parser.go`: Тихий сбой, сброс текста и порядок кандидатов

### Причина проблемы
1. Если входной поток содержит ошибку (HTML страницы 401/403 или невалидный JSON), парсер доходит до EOF и возвращает `&ParsedResponse{}, nil`. В коде `client.go` проверка `if parseErr == nil && parsed != nil` считается успешной, и пользователю отдается пустая строка.
2. При наличии нескольких кандидатов (черновиков ответа Gemini) цикл `for _, candAny := range candidates` обходит их все. Вызов `textB.Reset()` стирает ответ первого (основного) кандидата и сохраняет текст последнего.
3. Если ответ разбит на несколько строковых фрагментов в массиве `parts`, `textB.Reset()` стирает предыдущие фрагменты внутри того же кандидата.

### Реализация исправления
1. Возвращать явную ошибку `ErrEmptyStream` или `errors.New("empty response or failed to parse Gemini stream")`, если и `Text`, и `Thought` остались пустыми.
2. Обрабатывать только основного кандидата (`candidates[0]`) либо явно аккумулировать текст первого кандидата без перезаписи последующими.
3. Аккумулировать строковые части `parts` без преждевременного сброса буфера.

#### Полный код исправления ключевых функций (`internal/geminiweb/parser.go`):
```go
package geminiweb

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

var (
	ErrEmptyParsedResponse = errors.New("empty response or failed to parse Gemini stream")
	reThoughtTag           = regexp.MustCompile(`(?is)<thought(?:\s+[^>]*)?>(.*?)</thought>`)
	reThoughtBlock         = regexp.MustCompile(`(?is):::thought\s*(.*?)(?:\n\s*:::|\s+:::)(?:[\r\n\s]+|$)`)
)

func ParseStream(reader io.Reader) (*ParsedResponse, error) {
	resp := &ParsedResponse{}
	bufReader := bufio.NewReader(reader)

	// Пропуск ведущего префикса )]}'
	for {
		b, err := bufReader.ReadByte()
		if err != nil {
			break
		}
		if b == '[' || b == '{' || (b >= '0' && b <= '9') {
			_ = bufReader.UnreadByte()
			break
		}
	}

	var thoughtBuilder strings.Builder
	var textBuilder strings.Builder

	dec := json.NewDecoder(bufReader)
	var chunksCount int

	for {
		var token any
		err := dec.Decode(&token)
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		switch v := token.(type) {
		case []any:
			chunksCount++
			if rawBytes, err := json.Marshal(v); err == nil {
				resp.RawChunks = append(resp.RawChunks, string(rawBytes))
			}
			processEnvelopeToken(v, resp, &thoughtBuilder, &textBuilder)
		}
	}

	fullText := textBuilder.String()

	// Извлечение :::thought ... :::
	if matches := reThoughtBlock.FindAllStringSubmatch(fullText, -1); len(matches) > 0 {
		for _, m := range matches {
			if len(m) > 1 {
				t := strings.TrimSpace(m[1])
				if t != "" {
					if thoughtBuilder.Len() > 0 {
						thoughtBuilder.WriteString("\n\n")
					}
					thoughtBuilder.WriteString(t)
				}
			}
		}
		fullText = reThoughtBlock.ReplaceAllString(fullText, "")
	}

	// Извлечение <thought>...</thought>
	if matches := reThoughtTag.FindAllStringSubmatch(fullText, -1); len(matches) > 0 {
		for _, m := range matches {
			if len(m) > 1 {
				t := strings.TrimSpace(m[1])
				if t != "" {
					if thoughtBuilder.Len() > 0 {
						thoughtBuilder.WriteString("\n\n")
					}
					thoughtBuilder.WriteString(t)
				}
			}
		}
		fullText = reThoughtTag.ReplaceAllString(fullText, "")
	}

	resp.Thought = strings.TrimSpace(thoughtBuilder.String())
	resp.Text = strings.TrimSpace(fullText)

	if resp.Text == "" && resp.Thought == "" {
		return nil, ErrEmptyParsedResponse
	}

	return resp, nil
}

func extractFromInnerData(inner []any, resp *ParsedResponse, thoughtB *strings.Builder, textB *strings.Builder) {
	// 1. Conversation ID & Response ID
	if len(inner) > 1 {
		if convInfo, ok := inner[1].([]any); ok {
			if len(convInfo) > 0 {
				if cid, ok := convInfo[0].(string); ok && cid != "" {
					resp.ConversationID = cid
				}
			}
			if len(convInfo) > 1 {
				if rid, ok := convInfo[1].(string); ok && rid != "" {
					resp.ResponseID = rid
				}
			}
		}
	}

	// 2. Candidates: обрабатываем ПЕРВОГО кандидата (основной черновик)
	if len(inner) > 4 {
		if candidates, ok := inner[4].([]any); ok && len(candidates) > 0 {
			// Берем только первого кандидата (index 0)
			if cand, ok := candidates[0].([]any); ok && len(cand) > 0 {
				if choiceID, ok := cand[0].(string); ok && choiceID != "" {
					resp.ChoiceID = choiceID
				}

				if len(cand) > 1 {
					if parts, ok := cand[1].([]any); ok {
						var candText strings.Builder
						for _, pAny := range parts {
							if pStr, ok := pAny.(string); ok && pStr != "" {
								candText.WriteString(pStr)
							} else if pArr, ok := pAny.([]any); ok {
								for _, sub := range pArr {
									if subStr, ok := sub.(string); ok && subStr != "" {
										candText.WriteString(subStr)
									}
								}
							}
						}
						// В потоке StreamGenerate каждый чанк содержит полное накопленное состояние кандидата
						if candText.Len() > 0 {
							textB.Reset()
							textB.WriteString(candText.String())
						}
					}
				}

				for i := 2; i < len(cand); i++ {
					inspectThinkingTrace(cand[i], thoughtB)
				}
			}
		}
	}

	if len(inner) > 5 {
		for i := 5; i < len(inner); i++ {
			inspectThinkingTrace(inner[i], thoughtB)
		}
	}
}
```

---

## 4. Модули `internal/auth/auto_harvest.go` и `rotator.go`: Устранение спама окнами Chrome

### Причина проблемы
Если Chrome работает без флага отладки, `AutoHarvestLocked` запускает:
`open -n -a "Google Chrome" --args --user-data-dir=/tmp/agy-chrome-cdp-profile ...`
Каталог `/tmp/agy-chrome-cdp-profile` пуст, в нем нет Google-аккаунта. За 3 секунды ожидания авторизация не появится. Фоновый `CookieRotator` каждые 8 минут при ошибке 401/403 снова запускает `AutoHarvest`, непрерывно открывая новые окна браузера на рабочем столе пользователя.

### Реализация исправления
1. Прекратить автоматический запуск Chrome с пустым профилем `/tmp`. Запускать браузер только при наличии явной команды от пользователя (`--harvest-cookies`).
2. В фоновом ротаторе при ошибке авторизации не спамить процессами, а включать экспоненциальный бэкофф и однократно логировать инструкцию для пользователя:
   `open -a "Google Chrome" --args --remote-debugging-port=9222`
3. Сохранять куки на диск сразу при их обновлении в сессии.

#### Патч для `internal/auth/auto_harvest.go`:
```go
package auth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"
)

func (s *SessionManager) AutoHarvest(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.AutoHarvestLocked(ctx)
}

func (s *SessionManager) AutoHarvestLocked(ctx context.Context) error {
	endpoint := s.cdpURL
	if endpoint == "" {
		endpoint = "http://127.0.0.1:9222"
	}

	// 1. Проверяем, доступен ли уже работающий CDP
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(endpoint + "/json/version")
	if err == nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		if harvestErr := s.harvestCookiesFromCDPLocked(ctx); harvestErr == nil {
			log.Printf("[auto_harvest] successfully harvested cookies from active Chrome CDP session")
			return nil
		}
	}

	// 2. Попытка Safari fallback
	if safariErr := s.harvestCookiesFromSafariLocked(ctx); safariErr == nil {
		log.Printf("[auto_harvest] successfully harvested cookies from Safari")
		return nil
	}

	// НЕ открываем пустые окна /tmp! Выводим инструкцию для пользователя.
	return fmt.Errorf("Chrome CDP is not active at %s and Safari cookies inaccessible. To enable automatic harvest, start Chrome with: open -a \"Google Chrome\" --args --remote-debugging-port=9222", endpoint)
}
```

---

## 5. Модуль `internal/auth/session.go`: Оптимизация мьютексов и HTTP 302

### Причина проблемы
1. `RefreshWizData` захватывает `s.mu.Lock()` и удерживает его на протяжении всех сетевых HTTP-запросов и CDP-вызовов (до 10-30 секунд). Все остальные вызовы в это время блокируются.
2. При получении HTTP 302 с `gemini.google.com/app` на `/app?hl=ru` сессия ошибочно признается недействительной, хотя это нормальный региональный редирект.

### Реализация исправления
1. Выполнять сетевые запросы `Do(req)` без удержания блокировки записи, обновляя `s.wizData` только в короткой критической секции.
2. Проверять заголовок `Location`: если 302 ведет на `accounts.google.com` или `ServiceLogin` — только тогда считать сессию истекшей. При локализации следовать редиректу.

#### Патч для `internal/auth/session.go` (`RefreshWizData`):
```go
func (s *SessionManager) RefreshWizData(ctx context.Context) (*WizData, error) {
	// 1. Быстрая попытка через CDP
	if s.CDPURL() != "" {
		if wiz, err := s.FetchWizDataViaCDP(ctx); err == nil && wiz != nil && wiz.At != "" {
			s.mu.Lock()
			s.wizData = wiz
			s.lastWizFetch = time.Now()
			s.mu.Unlock()
			return wiz, nil
		}
	}

	// 2. Сетевой запрос выполняется БЕЗ удержания s.mu
	req, err := fhttp.NewRequestWithContext(ctx, "GET", "https://gemini.google.com/app", nil)
	if err != nil {
		return nil, err
	}

	cookieHdr := s.store.CookieHeader()
	if cookieHdr != "" {
		req.Header.Set("Cookie", cookieHdr)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch gemini app: %w", err)
	}
	defer resp.Body.Close()

	// Проверяем 302: если редирект на логин — сессия истекла
	if resp.StatusCode == 302 {
		loc := resp.Header.Get("Location")
		if strings.Contains(loc, "accounts.google.com") || strings.Contains(loc, "ServiceLogin") {
			if harvestErr := s.AutoHarvest(ctx); harvestErr == nil {
				return s.refreshWizDataWithNewCookies(ctx)
			}
			return nil, errors.New("gemini session expired: redirected to accounts.google.com")
		}
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gemini response body: %w", err)
	}

	wiz, err := ParseWizGlobalData(string(bodyBytes))
	if err != nil {
		if harvestErr := s.AutoHarvest(ctx); harvestErr == nil {
			return s.refreshWizDataWithNewCookies(ctx)
		}
		return nil, fmt.Errorf("parse WIZ_global_data: %w", err)
	}

	s.mu.Lock()
	s.wizData = wiz
	s.lastWizFetch = time.Now()
	s.mu.Unlock()

	return wiz, nil
}
```

---

## 6. Модуль `internal/mcp/handlers.go` & `slash_parser.go`: Исключение порчи промптов

### Причина проблемы
В `slash_parser.go` проверка `tokens[0]` сопоставляла как слова со слэшем (`/argon`), так и обычные слова без слэша (`argon`). Если запрос пользователя начинался со слов *"Argon is an inert gas"*, парсер отрезал слово *"Argon"* и передавал в модель *"is an inert gas"*.

### Реализация исправления
Маршрутизировать команду только тогда, когда первый токен начинается со слэша `/`.

#### Патч для `internal/mcp/slash_parser.go`:
```go
func ParseSlashCommand(input string, fallbackModel config.ModelType, fallbackEffort string) (*SlashCommandResult, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return &SlashCommandResult{
			Model:  fallbackModel,
			Prompt: "",
		}, nil
	}

	model := fallbackModel
	rawEffort := fallbackEffort

	tokens := strings.Fields(trimmed)
	idx := 0

	// СТРОГО: проверяем ТОЛЬКО наличие префикса '/'
	if len(tokens) > 0 && strings.HasPrefix(tokens[0], "/") {
		cmd := strings.TrimPrefix(strings.ToLower(tokens[0]), "/")
		if m, err := config.ParseModel(cmd); err == nil {
			model = m
			idx++

			// Проверяем следующий токен на уровень effort: low, middle, medium, high
			if idx < len(tokens) {
				candidate := strings.ToLower(tokens[idx])
				switch candidate {
				case "low", "middle", "medium", "high", "med", "mid", "max", "1", "2", "3":
					rawEffort = candidate
					idx++
				}
			}
		}
	}

	prompt := strings.Join(tokens[idx:], " ")
	effortLevel, tier, _, err := config.CanonicalEffort(model, rawEffort)
	if err != nil {
		return nil, err
	}

	return &SlashCommandResult{
		Model:  model,
		Effort: effortLevel,
		Tier:   tier,
		Prompt: prompt,
	}, nil
}
```

---

## 7. Модуль `internal/media/scotty.go`: Поддержка расширенных Rupio JSON-ответов

### Реализация исправления
Добавить поддержку формата `customerSpecificInfo.photopicker_service_attachment_token` и `sessionStatus.additionalInfo`:

```go
func extractUploadRef(body string, hdr fhttp.Header) string {
	// 1. Заголовок
	if ref := hdr.Get("X-Goog-Upload-Status-Resource-Id"); ref != "" {
		return ref
	}

	// 2. Структурированный JSON
	var rupioResp map[string]any
	if err := json.Unmarshal([]byte(body), &rupioResp); err == nil {
		// Рекурсивный поиск attachment token
		if token := findAttachmentToken(rupioResp); token != "" {
			return token
		}
	}

	// 3. Fallback подстроки /contrib_service/
	if idx := strings.Index(body, "/contrib_service/"); idx >= 0 {
		sub := body[idx:]
		end := strings.IndexAny(sub, "\"' \r\n\t,")
		if end > 0 {
			return sub[:end]
		}
		return strings.TrimSpace(sub)
	}

	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "/") {
		return trimmed
	}

	return ""
}

func findAttachmentToken(node any) string {
	switch v := node.(type) {
	case map[string]any:
		for k, val := range v {
			if strings.Contains(k, "attachment_token") || k == "resourceId" || k == "upload_ref" {
				if s, ok := val.(string); ok && s != "" {
					return s
				}
			}
			if token := findAttachmentToken(val); token != "" {
				return token
			}
		}
	case []any:
		for _, item := range v {
			if token := findAttachmentToken(item); token != "" {
				return token
			}
		}
	}
	return ""
}
```

---

## План внедрения исправлений по шагам

1. **Шаг 1:** Применить исправления в `internal/media/resolver.go` (устраняет падение запросов при упоминании `@clipboard` и двойную загрузку).
2. **Шаг 2:** Применить исправления в `internal/geminiweb/parser.go` (устраняет тихие сбои с пустым ответом, гарантирует корректный выбор основного кандидата).
3. **Шаг 3:** Применить исправления в `internal/auth/cdp_bridge.go` и `cdp_eval.go` (гарантирует подключение исключительно к вкладке Gemini, валидацию статуса `fetch` и обработку исключений).
4. **Шаг 4:** Применить исправления в `internal/auth/auto_harvest.go` и `rotator.go` (ликвидирует бесконечный спам окнами Chrome).
5. **Шаг 5:** Применить исправления в `internal/auth/session.go` (оптимизация мьютексов и правильная обработка 302).
6. **Шаг 6:** Применить исправления в `internal/mcp/slash_parser.go` и `internal/media/scotty.go`.
7. **Шаг 7:** Запустить тестовый прогон `go test -v ./...` и контрольную сборку бинарника `go build -o bin/agy-gemini-web-mcp cmd/server/main.go`.
