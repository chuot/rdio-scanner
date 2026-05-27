// Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const audioStorageSubdir = "audio"

// audioStorageRoot returns the directory under which all audio files live,
// resolved against the server's base directory.
func audioStorageRoot(config *Config) string {
	return config.GetPath(audioStorageSubdir)
}

// buildAudioFilePath composes the absolute path for a new call's audio file.
// Layout: {root}/YYYY/MM/DD/{ms}_{systemRef}_{talkgroupRef}_{rand}.{ext}
// The timestamp comes from the call (not wall clock), so files cluster by
// when the radio transmission actually occurred.
func buildAudioFilePath(config *Config, call *Call) (string, error) {
	if call == nil {
		return "", fmt.Errorf("audio storage: nil call")
	}
	if call.System == nil || call.Talkgroup == nil {
		return "", fmt.Errorf("audio storage: call missing system or talkgroup")
	}

	t := call.Timestamp.UTC()
	if t.IsZero() {
		return "", fmt.Errorf("audio storage: call has zero timestamp")
	}

	suffix, err := randomHex(4)
	if err != nil {
		return "", fmt.Errorf("audio storage: rand: %w", err)
	}

	ext := audioExtension(call.AudioFilename, call.AudioMime)

	name := fmt.Sprintf("%d_%d_%d_%s%s",
		t.UnixMilli(),
		call.System.SystemRef,
		call.Talkgroup.TalkgroupRef,
		suffix,
		ext,
	)

	dir := filepath.Join(
		audioStorageRoot(config),
		t.Format("2006"),
		t.Format("01"),
		t.Format("02"),
	)

	return filepath.Join(dir, name), nil
}

// writeAudioFile writes the audio bytes to the given absolute path, creating
// the parent directory if needed. Returns the path written (same as input).
func writeAudioFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("audio storage: mkdir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("audio storage: write: %w", err)
	}
	return nil
}

// readAudioFile loads the audio bytes from disk.
func readAudioFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("audio storage: read: %w", err)
	}
	return b, nil
}

// deleteAudioFile removes an audio file. Missing files are not an error.
func deleteAudioFile(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("audio storage: delete: %w", err)
	}
	return nil
}

// audioExtension returns a sensible file extension (including leading dot)
// for the given filename or mime type. Falls back to ".bin" if neither helps.
func audioExtension(filename, mime string) string {
	if ext := filepath.Ext(filename); ext != "" {
		return strings.ToLower(ext)
	}
	switch strings.ToLower(mime) {
	case "audio/aac", "audio/mp4", "audio/m4a", "audio/x-m4a":
		return ".m4a"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	case "audio/flac":
		return ".flac"
	}
	return ".bin"
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
