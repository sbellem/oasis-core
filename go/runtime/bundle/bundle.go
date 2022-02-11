// Package bundle implements support for unified runtime bundles.
package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/oasisprotocol/oasis-core/go/common/crypto/hash"
)

// Bundle is a runtime bundle instance.
type Bundle struct {
	Manifest *Manifest
	Data     map[string][]byte
}

// Validate validates the runtime bundle for well-formedness.
func (bnd *Bundle) Validate() error {
	// Ensure all the files in the manifest are present.
	type bundleFile struct {
		descr, fn string
	}
	needFiles := []bundleFile{
		{
			descr: "ELF executable",
			fn:    bnd.Manifest.Executable,
		},
	}
	if sgx := bnd.Manifest.SGX; sgx != nil {
		needFiles = append(needFiles,
			[]bundleFile{
				{
					descr: "SGX executable",
					fn:    sgx.Executable,
				},
				{
					descr: "SGX signature",
					fn:    sgx.Signature,
				},
			}...,
		)
	}
	for _, v := range needFiles {
		if v.fn == "" {
			return fmt.Errorf("runtime/bundle: missing %s in manifest", v.descr)
		}
		if len(bnd.Data[v.fn]) == 0 {
			return fmt.Errorf("runtime/bundle: missing %s in bundle", v.descr)
		}
	}

	// Ensure all files in the bundle have a digest entry, and that the
	// extracted file's digest matches the one in the manifest.
	for fn, b := range bnd.Data {
		h := hash.NewFromBytes(b)

		mh, ok := bnd.Manifest.Digests[fn]
		if !ok {
			// Ignore the manifest not having a digest entry, though
			// it having one and being valid (while quite a feat) is
			// also ok.
			if fn == manifestName {
				continue
			}
			return fmt.Errorf("runtime/bundle: missing digest: '%s'", fn)
		}
		if !h.Equal(&mh) {
			return fmt.Errorf("runtime/bundle: invalid digest: '%s'", fn)
		}
	}

	return nil
}

// Add adds/overwrites a file to/in the bundle.
func (bnd *Bundle) Add(fn string, b []byte) error {
	if filepath.Dir(fn) != "." {
		return fmt.Errorf("runtime/bundle: invalid filename for add: '%s'", fn)
	}

	h := hash.NewFromBytes(b)
	bnd.Manifest.Digests[fn] = h
	bnd.Data[fn] = append([]byte{}, b...) // Copy
	return nil
}

// Write serializes a runtime bundle to the on-disk representation.
func (bnd *Bundle) Write(fn string) error {
	// Ensure the bundle is well-formed.
	if err := bnd.Validate(); err != nil {
		return fmt.Errorf("runtime/bundle: refusing to write malformed bundle: %w", err)
	}

	// Serialize the manifest.
	rawManifest, err := json.Marshal(bnd.Manifest)
	if err != nil {
		return fmt.Errorf("runtime/bundle: failed to serialize manifest: %w", err)
	}
	if bnd.Data[manifestName] != nil {
		// While this is "ok", instead of trying to figure out if the
		// deserialized manifest matches the serialied one, just bail.
		return fmt.Errorf("runtime/bundle: data contains manifest entry")
	}

	// Write out the archive to a in-memory buffer, taking care to ensure
	// that the manifest is the 0th entry.
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	type writeFile struct {
		fn string
		b  []byte
	}
	writeFiles := []writeFile{
		{
			fn: manifestName,
			b:  rawManifest,
		},
	}
	for f := range bnd.Data {
		writeFiles = append(writeFiles, writeFile{
			fn: f,
			b:  bnd.Data[f],
		})
	}
	for _, f := range writeFiles {
		fw, wErr := w.Create(f.fn)
		if wErr != nil {
			return fmt.Errorf("runtime/bundle: failed to create file '%s': %w", f.fn, wErr)
		}
		if _, wErr = fw.Write(f.b); err != nil {
			return fmt.Errorf("runtime/bundle: failed to write file '%s': %w", f.fn, wErr)
		}
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("runtime/bundle: failed to finalize bundle: %w", err)
	}

	if err = os.WriteFile(fn, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("runtime/bundle: failed to write bundle: %w", err)
	}

	return nil
}

// Close closes the bundle, releasing resources.
func (bnd *Bundle) Close() error {
	bnd.Manifest = nil
	bnd.Data = nil
	return nil
}

// Open opens and validates a runtime bundle instance.
func Open(fn string) (*Bundle, error) {
	r, err := zip.OpenReader(fn)
	if err != nil {
		return nil, fmt.Errorf("runtime/bundle: failed to open bundle: %w", err)
	}
	defer r.Close()

	// Read the contents.
	//
	// Note: This extracts everything into memory, which is somewhat
	// expensive if it turns out the contents aren't needed.
	data := make(map[string][]byte)
	for i, v := range r.File {
		// Sanitize the file name by ensuring that all names are rooted
		// at the correct location.
		switch i {
		case 0:
			// Much like the JAR files, the manifest MUST come first.
			if v.Name != manifestName {
				return nil, fmt.Errorf("runtime/bundle: invalid manifest file name: '%s'", v.Name)
			}
		default:
			if filepath.Dir(v.Name) != "." {
				return nil, fmt.Errorf("runtime/bundle: failed to sanitize path '%s'", v.Name)
			}
		}

		// Extract every file into memory.
		rd, rdErr := v.Open()
		if rdErr != nil {
			return nil, fmt.Errorf("runtime/bundle: failed to open '%s': %w", v.Name, rdErr)
		}
		defer rd.Close()

		b, rdErr := io.ReadAll(rd)
		if err != nil {
			return nil, fmt.Errorf("runtime/bundle: failed to read '%s': %w", v.Name, rdErr)
		}

		data[v.Name] = b
	}

	// Decode the manifest.
	var manifest Manifest
	b, ok := data[manifestName]
	if !ok {
		return nil, fmt.Errorf("runtime/bundle: missing manifest")
	}
	if err = json.Unmarshal(b, &manifest); err != nil {
		return nil, fmt.Errorf("runtime/bundle: failed to parse manifest: %w", err)
	}

	// Ensure the bundle is well-formed.
	bnd := &Bundle{
		Manifest: &manifest,
		Data:     data,
	}
	if err = bnd.Validate(); err != nil {
		return nil, err
	}

	return bnd, nil
}
