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
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// containerIndex is the artifact a run that assembled the three architectures wrote: the digest of
// the image index inside its layout file, and one manifest for each architecture the run built.
type containerIndex struct {
	digest    string
	manifests []containerIndexManifest
}

// containerIndexManifest is one architecture the index names: the platform the entry spells, the
// variant it names with it, and the digest of the bytes the file holds for it. The platform and the
// variant stay apart because that is how a layout names a 32-bit ARM image: the platform is linux/arm
// and the variant v7 is what tells it from linux/arm64.
type containerIndexManifest struct {
	platform string
	variant  string
	digest   string
}

// word spells the entry the way a container target is spelled, which is how a run names it to the
// operator and how it is matched to the architecture it was built for.
func (one containerIndexManifest) word() string {
	if one.variant != "" {
		return one.platform + "/" + one.variant
	}

	return one.platform
}

// readContainerIndex reads the index out of the OCI layout file a run wrote, and refuses a file that
// is not one image index naming one manifest for each architecture the run built an image for. An
// OCI layout is a tar archive of content-addressed files, so every digest it names is checked
// against the bytes the archive holds under it: a file that disagrees with itself describes an
// artifact the run did not write, and reporting it would hand the operator numbers no image has.
func readContainerIndex(path string) (containerIndex, error) {
	files, err := readOCILayout(path)
	if err != nil {
		return containerIndex{}, err
	}

	digest, indexBody, err := readOCIIndex(path, files)
	if err != nil {
		return containerIndex{}, err
	}

	var index struct {
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				OS           string `json:"os"`
				Architecture string `json:"architecture"`
				Variant      string `json:"variant"`
			} `json:"platform"`
		} `json:"manifests"`
	}

	if err := json.Unmarshal(indexBody, &index); err != nil {
		return containerIndex{}, fmt.Errorf("reading the image index %s of %s: %w", digest, path, err)
	}

	// The manifests land in the order the release matrix names the architectures, because what a
	// run reports about the index it wrote reads in that order whatever order the container program
	// happened to write the entries in.
	manifests := make([]containerIndexManifest, len(containerTargets))

	for _, one := range index.Manifests {
		manifest := containerIndexManifest{
			platform: one.Platform.OS + "/" + one.Platform.Architecture,
			variant:  one.Platform.Variant,
			digest:   one.Digest,
		}

		rank := containerPlatformRank(manifest.word())
		if rank < 0 {
			return containerIndex{}, fmt.Errorf("the image index %s of %s names an entry for %s, which is no architecture a container image is built for", digest, path, manifest.word())
		}

		body, err := readOCIBlob(path, files, manifest.digest)
		if err != nil {
			return containerIndex{}, err
		}

		if manifest.digest != digestOf(body) {
			return containerIndex{}, fmt.Errorf("the image index %s of %s names %s as the %s manifest, which is not the digest of the bytes the file holds for it", digest, path, manifest.digest, manifest.word())
		}

		manifests[rank] = manifest
	}

	missing := []string{}

	for rank, target := range containerTargets {
		if manifests[rank].digest == "" {
			missing = append(missing, containerPlatformFor(target))
		}
	}

	if len(missing) > 0 {
		return containerIndex{}, fmt.Errorf("the image index %s of %s holds no manifest for %v, and a run asked for all three architectures it builds images for", digest, path, missing)
	}

	return containerIndex{digest: digest, manifests: manifests}, nil
}

// containerIndexReading spells the line a run reports about the index it wrote: the layout file, the
// digest of the image index inside it, and the digest of each architecture's manifest, in the order the
// release names the architectures. These are the numbers a release records, because they are what the
// published artifact names and nothing a container program was asked to recall.
func containerIndexReading(file string, index containerIndex) string {
	parts := []string{file + " index " + index.digest}

	for _, one := range index.manifests {
		parts = append(parts, one.word()+" "+one.digest)
	}

	return strings.Join(parts, ", ")
}

// readOCILayout reads every file the layout archive holds, by the name the archive gives it.
func readOCILayout(path string) (map[string][]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening the index file %s: %w", path, err)
	}

	defer file.Close()

	reader := tar.NewReader(file)

	files := map[string][]byte{}

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("reading the index file %s: %w", path, err)
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		body, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("reading %s from the index file %s: %w", header.Name, path, err)
		}

		files[header.Name] = body
	}

	return files, nil
}

// readOCIIndex returns the image index a layout's index.json names: its digest and its bytes. That
// index is the one artifact whose digest a run reports as the digest of the index it assembled.
func readOCIIndex(path string, files map[string][]byte) (string, []byte, error) {
	top, ok := files["index.json"]
	if !ok {
		return "", nil, fmt.Errorf("the index file %s holds no index.json, so it is not an OCI layout", path)
	}

	var layout struct {
		Manifests []struct {
			Digest string `json:"digest"`
		} `json:"manifests"`
	}

	if err := json.Unmarshal(top, &layout); err != nil {
		return "", nil, fmt.Errorf("reading index.json of %s: %w", path, err)
	}

	if len(layout.Manifests) != 1 {
		return "", nil, fmt.Errorf("the index file %s names %d artifacts in its index.json, want the one image index a run writes", path, len(layout.Manifests))
	}

	body, err := readOCIBlob(path, files, layout.Manifests[0].Digest)
	if err != nil {
		return "", nil, err
	}

	// The digest index.json names is what a run reports as the digest of the index it assembled, so
	// it is checked against the bytes the archive holds under it the same way every manifest is.
	if layout.Manifests[0].Digest != digestOf(body) {
		return "", nil, fmt.Errorf("the index file %s names %s as its image index, which is not the digest of the bytes the file holds for it", path, layout.Manifests[0].Digest)
	}

	return layout.Manifests[0].Digest, body, nil
}

// readOCIBlob returns the bytes an OCI layout holds for a digest, and refuses a digest it holds no
// bytes for. The bytes a digest names are stored under that digest, so an archive is a set of
// content-addressed files rather than a list of names.
func readOCIBlob(path string, files map[string][]byte, digest string) ([]byte, error) {
	body, ok := files[ociBlobPath(digest)]
	if !ok {
		return nil, fmt.Errorf("the index file %s holds no bytes for %s", path, digest)
	}

	return body, nil
}

// ociBlobPath spells where a layout stores the bytes a digest names: the algorithm and the digest
// of the bytes themselves, each its own directory.
func ociBlobPath(digest string) string {
	algorithm, value, _ := strings.Cut(digest, ":")

	return "blobs/" + algorithm + "/" + value
}

// digestOf spells the digest an OCI layout names a blob by: the hash of its own bytes.
func digestOf(body []byte) string {
	sum := sha256.Sum256(body)

	return "sha256:" + hex.EncodeToString(sum[:])
}
