package attachment

import "testing"

func TestKindMimeMapping(t *testing.T) {
	cases := []struct {
		mime         string
		fallbackType string
		want         string
	}{
		{"image/png", "", KindImage},
		{"IMAGE/JPEG", "", KindImage},
		{"video/mp4", "", KindVideo},
		{"audio/ogg", "", KindAudio},
		{"text/csv", "", KindSpreadsheet},
		{"application/vnd.ms-excel", "", KindSpreadsheet},
		{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "", KindSpreadsheet},
		{"text/tab-separated-values", "", KindSpreadsheet},
		{"application/pdf", "", KindDocument},
		{"application/msword", "", KindDocument},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "", KindDocument},
		{"text/plain", "", KindDocument},
		{"text/markdown", "", KindDocument},
		{"application/octet-stream", "", KindFile},
		{"application/zip", "", KindFile},
		{"", "image", KindImage},
		{"", "AUDIO", KindAudio},
		{"", "video", KindVideo},
		{"", "file", KindFile},
		{"", "", KindFile},
		{"", "sticker", KindFile},
		{"data:image/png;base64,xxx", "", KindFile},
		{"image/png; charset=utf-8", "", KindImage},
	}
	for _, tc := range cases {
		if got := Kind(tc.mime, tc.fallbackType); got != tc.want {
			t.Errorf("Kind(%q, %q) = %q, want %q", tc.mime, tc.fallbackType, got, tc.want)
		}
	}
}

func TestKindLabel(t *testing.T) {
	cases := map[string]string{
		KindImage:       "图片",
		KindVideo:       "视频",
		KindAudio:       "音频",
		KindSpreadsheet: "表格",
		KindDocument:    "文档",
		KindFile:        "文件",
		"party":         "文件",
		"":              "文件",
	}
	for kind, want := range cases {
		if got := KindLabel(kind); got != want {
			t.Errorf("KindLabel(%q) = %q, want %q", kind, got, want)
		}
	}
}
