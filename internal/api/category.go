package api

import (
	"net/url"
	"path"
	"strings"
)

// 카테고리: 파일 확장자 기반 자동 분류. GUI 사이드바 필터가 쓴다.
const (
	CategoryVideo   = "video"
	CategoryAudio   = "audio"
	CategoryDoc     = "doc"
	CategoryArchive = "archive"
	CategoryApp     = "app"
	CategoryImage   = "image"
	CategoryEtc     = "etc"
)

var extCategory = map[string]string{
	".mp4": CategoryVideo, ".mkv": CategoryVideo, ".avi": CategoryVideo, ".mov": CategoryVideo,
	".webm": CategoryVideo, ".m4v": CategoryVideo, ".ts": CategoryVideo, ".flv": CategoryVideo,
	".wmv": CategoryVideo,

	".mp3": CategoryAudio, ".m4a": CategoryAudio, ".flac": CategoryAudio, ".wav": CategoryAudio,
	".aac": CategoryAudio, ".ogg": CategoryAudio, ".opus": CategoryAudio,

	".pdf": CategoryDoc, ".doc": CategoryDoc, ".docx": CategoryDoc, ".xls": CategoryDoc,
	".xlsx": CategoryDoc, ".ppt": CategoryDoc, ".pptx": CategoryDoc, ".txt": CategoryDoc,
	".hwp": CategoryDoc, ".hwpx": CategoryDoc, ".epub": CategoryDoc, ".csv": CategoryDoc,

	".zip": CategoryArchive, ".rar": CategoryArchive, ".7z": CategoryArchive, ".tar": CategoryArchive,
	".gz": CategoryArchive, ".bz2": CategoryArchive, ".xz": CategoryArchive,

	".dmg": CategoryApp, ".pkg": CategoryApp, ".app": CategoryApp, ".exe": CategoryApp,
	".msi": CategoryApp, ".apk": CategoryApp, ".deb": CategoryApp, ".rpm": CategoryApp,

	".jpg": CategoryImage, ".jpeg": CategoryImage, ".png": CategoryImage, ".gif": CategoryImage,
	".webp": CategoryImage, ".heic": CategoryImage, ".svg": CategoryImage, ".bmp": CategoryImage,
	".tiff": CategoryImage, ".raw": CategoryImage, ".psd": CategoryImage,
}

// Categorize는 파일 이름 또는 URL에서 카테고리를 정한다.
func Categorize(nameOrURL string) string {
	name := nameOrURL
	if u, err := url.Parse(nameOrURL); err == nil && u.Path != "" {
		name = u.Path
	}
	ext := strings.ToLower(path.Ext(name))
	if ext == ".m3u8" || ext == ".m3u" {
		return CategoryVideo // HLS 스트림
	}
	if c, ok := extCategory[ext]; ok {
		return c
	}
	return CategoryEtc
}
