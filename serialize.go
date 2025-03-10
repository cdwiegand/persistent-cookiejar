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
	// Marshaling entries can never fail.
	data, _ := json.Marshal(j.allEntriesToPersist())
	return data, nil
}

// save is like Save but takes the current time as a parameter.
func (j *Jar) save(now time.Time) error {
	f, err := lockedfile.OpenFile(j.filename, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("cannot open cookie file: %w", err)
	}
	defer f.Close()
	// TODO optimization: if the file hasn't changed since we
	// loaded it, don't bother with the merge step.

	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.mergeFrom(f); err != nil {
		// The cookie file is probably corrupt.
		log.Printf("cannot read cookie file to merge it; ignoring it: %v", err)
	}
	j.deleteExpiredAtTime(now)
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("cannot truncate file: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return fmt.Errorf("cannot seek to beginning of cookie file: %w", err)
	}
	return j.writeTo(f)
}

// load loads the cookies from j.filename. If the file does not exist,
// no error will be returned and no cookies will be loaded.
func (j *Jar) load() error {
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
	if err := j.mergeFrom(f); err != nil {
		return fmt.Errorf("cannot merge cookie data: %w", err)
	}
	return nil
}

// mergeFrom reads all the cookies from r and stores them in the Jar.
func (j *Jar) mergeFrom(r io.Reader) error {
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
	j.merge(entries)
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
			if j.persistSessionCookies || e.Persistent {
				entries = append(entries, e)
			}
		}
	}
	sort.Sort(byCanonicalHost{entries})
	return entries
}
