package main

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/storage"
)

// docStatus is the result of comparing a local file against its S3 object.
type docStatus int

const (
	statusClean      docStatus = iota // local and remote are identical
	statusRemoteOnly                  // exists on S3, not downloaded locally
	statusStale                       // remote changed, local untouched
	statusDirty                       // local changed, remote untouched
	statusDiverged                    // both changed
)

// compareDoc compares the local copy of a document with its remote object.
// It returns the status and the sha256 of the local file ("" if absent).
func compareDoc(docsDir string, e index.Entry, rem storage.Object) (docStatus, string) {
	path := filepath.Join(docsDir, filepath.FromSlash(rem.Key))
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() || (fi.Size() == 0 && rem.Size > 0) {
		return statusRemoteOnly, ""
	}

	sha, md5sum := hashLocal(path)

	// Single-part S3 ETags are the MD5 of the content: a direct identity check.
	if !strings.Contains(rem.ETag, "-") && md5sum == rem.ETag {
		return statusClean, sha
	}

	var localChanged bool
	if e.LocalSHA256 != "" {
		localChanged = sha != e.LocalSHA256
	} else {
		// No recorded hash: the content differs from remote, so treat it as a local change.
		localChanged = true
	}
	remoteChanged := e.BaseETag != rem.ETag

	switch {
	case localChanged && remoteChanged:
		return statusDiverged, sha
	case localChanged:
		return statusDirty, sha
	case remoteChanged:
		return statusStale, sha
	default:
		return statusClean, sha
	}
}

func hashLocal(path string) (shaHex, md5Hex string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	s, m := sha256.New(), md5.New()
	if _, err := io.Copy(io.MultiWriter(s, m), f); err != nil {
		return "", ""
	}
	return hex.EncodeToString(s.Sum(nil)), hex.EncodeToString(m.Sum(nil))
}

// remoteDocs lists S3 and returns the non-system objects keyed by document ID.
func remoteDocs(objects []storage.Object) map[string]storage.Object {
	m := make(map[string]storage.Object, len(objects))
	for _, obj := range objects {
		key := strings.TrimPrefix(obj.Key, "/")
		if isIgnoredRemoteKey(key) {
			continue
		}
		obj.Key = key
		m[index.TrimDocExt(key)] = obj
	}
	return m
}
