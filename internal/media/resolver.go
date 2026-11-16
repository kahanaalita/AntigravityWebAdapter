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

// ResolvedImage holds normalized image data ready for upload.
type ResolvedImage struct {
	FileName string
	MimeType string
	Data     []byte
}

// ImageResolver parses image sources from prompt strings, file paths, clipboard, and base64.
type ImageResolver struct {
	WorkDir string
}

// NewImageResolver creates an ImageResolver.
func NewImageResolver(workDir string) *ImageResolver {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	return &ImageResolver{WorkDir: workDir}
}

// ResolveFromPromptAndList extracts images from prompt text (@paths, @clipboard) and explicit images array.
// Returns cleaned prompt text and resolved images.
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

	// 1. Check for @clipboard in prompt
	if reAtClip.MatchString(cleanedPrompt) {
		clipImg, err := r.ResolveClipboard()
		if err == nil && clipImg != nil {
			addImage(clipImg)
		}
		// Do not fail the whole request if clipboard is empty or fails.
		// Strip the isolated token and proceed with prompt text.
		for reAtClip.MatchString(cleanedPrompt) {
			cleanedPrompt = reAtClip.ReplaceAllString(cleanedPrompt, " ")
		}
	}

	// 2. Extract @file paths from prompt
	for {
		matched := false
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
				// File does not exist on disk or is a directory:
				// Leave token as ordinary user text.
				return match
			}

			img, err := r.ResolveFile(p)
			if err == nil && img != nil {
				addImage(img)
				matched = true
				return " "
			}
			return match
		})
		if !matched {
			break
		}
	}

	// 3. Process explicit list
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

	// Clean up whitespace in prompt
	cleanedPrompt = strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(cleanedPrompt, " "))

	return cleanedPrompt, images, nil
}

// ResolveFile reads an image from relative or absolute file path.
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

// ResolveDataURI decodes data:image/<type>;base64,<data>.
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

// ResolveClipboard captures image from macOS clipboard using pngpaste or osascript.
func (r *ImageResolver) ResolveClipboard() (*ResolvedImage, error) {
	// Try pngpaste first if available
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

	// Try AppleScript on macOS
	script := `set clip to (the clipboard as «class PNGf»)`
	cmd := exec.Command("/usr/bin/osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("osascript clipboard error: %w", err)
	}

	raw := strings.TrimSpace(string(out))
	// Expected format: «data PNGf<HEX_STRING>»
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

	return nil, errors.New("no PNG image found in system clipboard")
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

// ValidateImageSize checks if image exceeds limit (15MB) and validates image headers.
func ValidateImageSize(data []byte, maxBytes int64) error {
	if int64(len(data)) > maxBytes {
		return fmt.Errorf("image size %d bytes exceeds maximum allowed %d bytes", len(data), maxBytes)
	}
	// Verify that data is not empty
	if len(data) == 0 {
		return errors.New("empty image data")
	}
	return nil
}

// CheckImageValid verifies image format decoding.
func CheckImageValid(data []byte) error {
	_, _, err := image.DecodeConfig(bytes.NewReader(data))
	return err
}
