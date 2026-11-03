package tools

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path"
	"strings"

	"github.com/felinics/memoh/internal/attachment"
	"github.com/felinics/memoh/internal/workspace/bridge"
)

// The read tool does not decode video: no frames, no thumbnails, no metadata
// probing. It returns a reference the model can hand to a specialist bot via
// delegation, together with the content hash that delegation attachments
// require. Whether to delegate at all stays the model's decision.

// readVideoContentType reports the video container MIME type for the read
// sniff stage. Content sniffing decides for the containers DetectContentType
// knows; otherwise a binary probe plus a video extension must still show a
// recognizable container magic, or the branch fails instead of guessing.
func readVideoContentType(probe []byte, filePath string) (string, bool, error) {
	// Known audio extensions belong to the audio transcription branch, never
	// to the video reference, even when they ride an mp4 container brand.
	if isAudioExtension(filePath) {
		return "", false, nil
	}
	sniffed := strings.ToLower(strings.TrimSpace(http.DetectContentType(probe)))
	if strings.HasPrefix(sniffed, "video/") {
		return sniffed, true, nil
	}
	if bytes.IndexByte(probe, 0) < 0 {
		return "", false, nil
	}
	switch strings.ToLower(path.Ext(filePath)) {
	case ".mp4", ".m4v", ".mov":
		if hasMP4FtypBox(probe) {
			return "video/mp4", true, nil
		}
	case ".webm", ".mkv":
		if bytes.HasPrefix(probe, []byte{0x1a, 0x45, 0xdf, 0xa3}) {
			return "video/webm", true, nil
		}
	case ".avi":
		if hasAVIHeader(probe) {
			return "video/x-msvideo", true, nil
		}
	default:
		return "", false, nil
	}
	return "", true, errors.New("video container not recognized")
}

// hasMP4FtypBox checks for the ISO base media ftyp box at offset 4.
func hasMP4FtypBox(probe []byte) bool {
	return len(probe) >= 12 && bytes.Equal(probe[4:8], []byte("ftyp"))
}

// hasAVIHeader checks for the RIFF container with an AVI list type.
func hasAVIHeader(probe []byte) bool {
	return len(probe) >= 12 && bytes.Equal(probe[0:4], []byte("RIFF")) && bytes.Equal(probe[8:12], []byte("AVI "))
}

// isAudioExtension keeps known audio extensions out of the video branch; the
// audio transcription branch owns them when it is present.
func isAudioExtension(filePath string) bool {
	switch strings.ToLower(path.Ext(filePath)) {
	case ".mp3", ".m4a", ".wav", ".ogg", ".opus", ".flac", ".aac", ".wma":
		return true
	}
	return false
}

// readVideoReference stats the file and builds the public reference result.
// It never reads beyond the probe bytes the caller already consumed.
func readVideoReference(ctx context.Context, client *bridge.Client, rawPath, resolvedPath, mimeType string) (any, error) {
	result := map[string]any{
		"ok":   true,
		"path": rawPath,
		"mime": mimeType,
		"kind": "video",
	}
	if stat, err := client.Stat(ctx, resolvedPath); err == nil && stat != nil {
		result["size"] = stat.GetSize()
	}
	if hash := mediaContentHash(rawPath); hash != "" {
		result["content_hash"] = hash
		result["handoff"] = "delegate_to_bot"
	}
	return result, nil
}

// mediaContentHash derives the managed-media content hash from a consumer
// media access path: the storage key's basename without its extension. Any
// other path reports an empty hash.
func mediaContentHash(rawPath string) string {
	if !attachment.IsMediaAccessPath(rawPath) {
		return ""
	}
	storageKey := attachment.ExtractStorageKey(rawPath)
	if storageKey == "" {
		return ""
	}
	base := path.Base(storageKey)
	return strings.TrimSuffix(base, path.Ext(base))
}
