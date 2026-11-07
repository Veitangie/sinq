// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
)

type multipartEntry struct {
	data               string
	source             string
	contentDisposition string
	contentType        string
	headers            map[string][]string
}

type multipartData struct {
	boundary     string
	entries      []multipartEntry
	materialized []byte
}

func makeMultipartBody(data multipartData, workspace Workspace) ([]byte, string, error) {
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	var err error
	if data.boundary != "" {
		err = mw.SetBoundary(data.boundary)
		if err != nil {
			return nil, "", fmt.Errorf("Failed to set multipart boundary: %w", err)
		}
	}

	for _, entry := range data.entries {
		h := textproto.MIMEHeader(entry.headers)
		if h == nil {
			h = make(textproto.MIMEHeader)
		}
		h.Set(contentDispositionHeader, entry.contentDisposition)
		if entry.contentType != "" {
			h.Set(contentTypeHeader, entry.contentType)
		}

		part, err := mw.CreatePart(h)
		if err != nil {
			return nil, "", fmt.Errorf("Failed to create multipart entry: %w", err)
		}

		if entry.source != "" {
			f, err := workspace.Open(entry.source)
			if err != nil {
				return nil, "", fmt.Errorf("Failed to open file %s for multipart entry: %w", entry.source, err)
			}
			_, err = io.Copy(part, f)
			f.Close()
			if err != nil {
				return nil, "", fmt.Errorf("Failed to write multipart entry data: %w", err)
			}
			continue
		}

		_, err = part.Write([]byte(entry.data))
		if err != nil {
			return nil, "", fmt.Errorf("Failed to write multipart entry data: %w", err)
		}
	}

	err = mw.Close()
	return body.Bytes(), mw.FormDataContentType(), err
}

const (
	octetStream              = "application/octet-stream"
	contentDispositionHeader = "Content-Disposition"
	contentTypeHeader        = "Content-Type"
)

var typeLookup map[string]string = map[string]string{
	// Text & data
	".txt":    "text/plain",
	".csv":    "text/csv",
	".md":     "text/markdown",
	".html":   "text/html",
	".htm":    "text/html",
	".css":    "text/css",
	".js":     "text/javascript",
	".mjs":    "text/javascript",
	".json":   "application/json",
	".ndjson": "application/x-ndjson",
	".xml":    "application/xml",
	".yaml":   "application/yaml",
	".yml":    "application/yaml",
	".ics":    "text/calendar",
	".vtt":    "text/vtt",
	".wasm":   "application/wasm",

	// Images
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
	".tif":  "image/tiff",
	".tiff": "image/tiff",
	".avif": "image/avif",
	".heic": "image/heic",

	// Documents
	".pdf":  "application/pdf",
	".rtf":  "application/rtf",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ppt":  "application/vnd.ms-powerpoint",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".odt":  "application/vnd.oasis.opendocument.text",
	".ods":  "application/vnd.oasis.opendocument.spreadsheet",
	".odp":  "application/vnd.oasis.opendocument.presentation",

	// Archives & compression
	".zip": "application/zip",
	".tar": "application/x-tar",
	".gz":  "application/gzip",
	".tgz": "application/gzip",
	".bz2": "application/x-bzip2",
	".xz":  "application/x-xz",
	".7z":  "application/x-7z-compressed",
	".rar": "application/vnd.rar",

	// Audio
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".oga":  "audio/ogg",
	".opus": "audio/opus",
	".flac": "audio/flac",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",

	// Video
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".webm": "video/webm",
	".ogv":  "video/ogg",
	".mov":  "video/quicktime",
	".avi":  "video/x-msvideo",
	".mkv":  "video/x-matroska",

	// Fonts
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
}

func contentTypeFromFile(filename string) string {
	idx := strings.LastIndexByte(filename, '.')
	if idx == -1 {
		return ""
	}
	res := typeLookup[filename[idx:]]

	if res == "" {
		res = mime.TypeByExtension(filename[idx:])
	}

	return res
}
