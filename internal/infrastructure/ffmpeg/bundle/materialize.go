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
//
// WebSocket API Access Policy:
// This WebSocket API is reserved exclusively for Saubeo Solutions and its native applications.
// Unauthorized access is strictly prohibited.
// See API_ACCESS_POLICY.md for full terms.

package bundle

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// program is one of the two programs a bundle carries, already read out of the binary.
type program struct {
	// Name is the program, without the suffix its target puts on a file it runs: ffmpeg, not
	// ffmpeg.exe. The suffix belongs to the name the server writes it under, not to the program.
	Name string

	// Body is the program as it runs, decompressed from what the binary embeds.
	Body []byte
}

// Materialize writes the bundle's two programs into dir and returns the paths the server runs them
// from.
//
// The programs are named after the ffmpeg release they are, so an upgrade writes a new file instead
// of replacing one another server may be running. A program that is already the right one is left
// alone, so a server that restarts does not rewrite a binary a running one is using.
func (b Bundle) Materialize(dir string) (string, string, error) {
	if b.files == nil {
		return "", "", errors.New("this server carries no ffmpeg bundle to write out")
	}

	programs, err := b.read()
	if err != nil {
		return "", "", err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", fmt.Errorf("the server cannot prepare %s for its ffmpeg: %w", dir, err)
	}

	version := ReadManifest().FFmpeg
	suffix := executableSuffix(b.Dir)

	written := make([]string, 0, len(programs))

	for _, one := range programs {
		path := filepath.Join(dir, one.Name+"-"+version+suffix)

		same, err := isTheSameProgram(path, one.Body)
		if err != nil {
			return "", "", err
		}

		if same {
			written = append(written, path)
			continue
		}

		if err := writeProgram(path, one.Body); err != nil {
			return "", "", err
		}

		written = append(written, path)
	}

	return written[0], written[1], nil
}

// read decompresses both programs before anything is written. A bundle whose program cannot be read
// back is a build that produced something broken, and the server must not be left with one program
// on disk and the other missing.
func (b Bundle) read() ([]program, error) {
	suffix := executableSuffix(b.Dir)

	programs := make([]program, 0, 2)

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		body, err := readGzip(b.files, b.Dir+"/"+name+suffix+".gz")
		if err != nil {
			return nil, fmt.Errorf("the ffmpeg bundle this server was built with is not readable: %w", err)
		}

		programs = append(programs, program{Name: name, Body: body})
	}

	return programs, nil
}

func readGzip(files fs.FS, name string) ([]byte, error) {
	compressed, err := fs.ReadFile(files, name)
	if err != nil {
		return nil, err
	}

	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}

	defer reader.Close()

	return io.ReadAll(reader)
}

// isTheSameProgram reports whether the file on disk is already this program, decided by its
// sha256 (CA-018.2): a program's identity is its content, not its length, and a file planted at
// the program's path with the bundle program's length is not the bundle's program. The hash is
// streamed, so verifying a 40 MB program does not pull it into memory.
//
// The path is inspected with os.Lstat: a symlink at the program's path is not a program the
// server wrote, and the server does not write its program through a link it did not make — it
// says so, and stops.
func isTheSameProgram(path string, body []byte) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}

		return false, fmt.Errorf("the server cannot inspect %s: %w", path, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("%s is a symlink the server did not write; the server will not write its program through it", path)
	}

	if !info.Mode().IsRegular() {
		return false, nil
	}

	onDisk, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("the server cannot read %s: %w", path, err)
	}

	defer onDisk.Close()

	hash := sha256.New()

	if _, err := io.Copy(hash, onDisk); err != nil {
		return false, fmt.Errorf("the server cannot read %s: %w", path, err)
	}

	want := sha256.Sum256(body)

	return bytes.Equal(hash.Sum(nil), want[:]), nil
}

// writeProgram writes the program through a temporary file and moves it into place, so a server that
// stops halfway leaves either the old program or the new one, and never half of either.
func writeProgram(path string, body []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("the server cannot prepare %s: %w", path, err)
	}

	defer os.Remove(temporary.Name())

	if _, err := temporary.Write(body); err != nil {
		temporary.Close()

		return fmt.Errorf("the server cannot write %s: %w", path, err)
	}

	if err := temporary.Chmod(0755); err != nil {
		temporary.Close()

		return fmt.Errorf("the server cannot make %s runnable: %w", path, err)
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("the server cannot finish writing %s: %w", path, err)
	}

	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("the server cannot put %s in place: %w", path, err)
	}

	return nil
}
