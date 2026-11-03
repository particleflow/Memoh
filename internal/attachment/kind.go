package attachment

import "strings"

// Attachment kind vocabulary (TF-16 design §1): a mechanical, countable
// classification of one attachment by its MIME type, shared by the presence
// decision input and the acknowledgement slots. Kinds are stable identifiers;
// display surfaces use KindLabel.
const (
	KindImage       = "image"
	KindVideo       = "video"
	KindAudio       = "audio"
	KindSpreadsheet = "spreadsheet"
	KindDocument    = "document"
	KindFile        = "file"
)

// kindLabels are the Chinese display labels for the kind vocabulary
// (TF-16 design §1 table).
var kindLabels = map[string]string{
	KindImage:       "图片",
	KindVideo:       "视频",
	KindAudio:       "音频",
	KindSpreadsheet: "表格",
	KindDocument:    "文档",
	KindFile:        "文件",
}

const wordprocessingMLPrefix = "application/vnd.openxmlformats-officedocument.wordprocessingml."

// spreadsheetMimes are the spreadsheet-lane MIME types (design §1 table).
var spreadsheetMimes = map[string]bool{
	"text/csv":                 true,
	"application/vnd.ms-excel": true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": true,
	"text/tab-separated-values":                                         true,
}

// Kind classifies one attachment: the MIME mapping first, then the declared
// attachment type vocabulary (image/audio/video/file) when the MIME is absent,
// and KindFile for anything unrecognized. mime is normalized here; fallbackType
// is matched case-insensitively.
func Kind(mime, fallbackType string) string {
	normalized := NormalizeMime(mime)
	if normalized == "" {
		switch strings.ToLower(strings.TrimSpace(fallbackType)) {
		case KindImage:
			return KindImage
		case KindAudio:
			return KindAudio
		case KindVideo:
			return KindVideo
		}
		return KindFile
	}
	switch {
	case strings.HasPrefix(normalized, "image/"):
		return KindImage
	case strings.HasPrefix(normalized, "video/"):
		return KindVideo
	case strings.HasPrefix(normalized, "audio/"):
		return KindAudio
	case spreadsheetMimes[normalized]:
		return KindSpreadsheet
	case normalized == "application/pdf" ||
		normalized == "application/msword" ||
		strings.HasPrefix(normalized, wordprocessingMLPrefix) ||
		strings.HasPrefix(normalized, "text/"):
		return KindDocument
	default:
		return KindFile
	}
}

// KindLabel returns the Chinese display label for a kind; unknown kinds label
// as a plain file.
func KindLabel(kind string) string {
	if label, ok := kindLabels[kind]; ok {
		return label
	}
	return kindLabels[KindFile]
}
