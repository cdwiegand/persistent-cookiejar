// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookiejar

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/rogpeppe/go-internal/lockedfile"
	"github.com/rogpeppe/go-internal/robustio"
)

// Save saves the cookies to the persistent cookie file.
// Before the file is written, it reads any cookies that
// have been stored from it and merges them into j.
func (j *Jar) Save() error {
	if j.filename == "" {
		return nil
	}
	return j.save(time.Now())
}

// MarshalJSON implements json.Marshaler by encoding all persistent cookies
// currently in the jar.
func (j *Jar) MarshalJSON() ([]byte, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return json.Marshal(j.allEntriesToPersist())
}

// save is like Save but takes the current time as a parameter.
func (j *Jar) save(now time.Time) error {
	// Lock a stable sidecar: replacing the data file also replaces its inode,
	// so locking the data file itself would allow writers to bypass each other.
	lock, err := lockedfile.OpenFile(j.filename+".lock", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("cannot lock cookie file: %w", err)
	}
	defer lock.Close()
	// TODO optimization: if the file hasn't changed since we
	// loaded it, don't bother with the merge step.

	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.load(now); err != nil {
		return err
	}
	j.deleteExpiredAtTime(now)
	return j.replaceFile()
}

// replaceFile writes and syncs a complete replacement before changing the
// destination. The caller holds both j.mu and the sidecar file lock.
func (j *Jar) replaceFile() error {
	f, err := os.CreateTemp(filepath.Dir(j.filename), "."+filepath.Base(j.filename)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create cookie file: %w", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := j.writeTo(f); err != nil {
		return fmt.Errorf("cannot encode cookie file: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("cannot sync cookie file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("cannot close cookie file: %w", err)
	}
	if err := robustio.Rename(f.Name(), j.filename); err != nil {
		return fmt.Errorf("cannot replace cookie file: %w", err)
	}
	return nil
}

// load loads the cookies from j.filename. If the file does not exist,
// no error will be returned and no cookies will be loaded.
func (j *Jar) load(now time.Time) error {
	if _, err := os.Stat(filepath.Dir(j.filename)); os.IsNotExist(err) {
		// The directory that we'll store the cookie jar
		// in doesn't exist, so don't bother trying
		// to acquire the lock.
		return nil
	}
	f, err := lockedfile.Open(j.filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	if err := j.mergeFrom(f, now); err != nil {
		return fmt.Errorf("cannot merge cookie data: %w", err)
	}
	return nil
}

// mergeFrom reads all the cookies from r and stores them in the Jar.
func (j *Jar) mergeFrom(r io.Reader, now time.Time) error {
	decoder := json.NewDecoder(r)
	// Cope with old cookiejar format by just discarding
	// cookies, but still return an error if it's invalid JSON.
	var data json.RawMessage
	if err := decoder.Decode(&data); err != nil {
		if err == io.EOF {
			// Empty file.
			return nil
		}
		return err
	}
	var entries []entry
	if err := json.Unmarshal(data, &entries); err != nil {
		log.Printf("warning: discarding cookies in invalid format (error: %v)", err)
		return nil
	}
	j.merge(entries, now)
	return nil
}

// writeTo writes all the cookies in the jar to w
// as a JSON array.
func (j *Jar) writeTo(w io.Writer) error {
	encoder := json.NewEncoder(w)
	entries := j.allEntriesToPersist()
	if err := encoder.Encode(entries); err != nil {
		return err
	}
	return nil
}

// allEntriesToPersist returns all the entries in the jar, sorted by primarly by canonical host
// name and secondarily by path length.
func (j *Jar) allEntriesToPersist() []entry {
	var entries []entry
	for _, submap := range j.entries {
		for _, e := range submap {
			if !j.persistSessionCookies && !e.Persistent {
				// Do not persist session values, but record their replacement
				// of any older persistent version held by another process.
				e.Value = ""
				e.Persistent = true
				e.Expires = e.Updated
			}
			entries = append(entries, e)
		}
	}
	sort.Sort(byCanonicalHost{entries})
	return entries
}
