package cookiejar

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func jarAt(t *testing.T, file string, persistSessions bool, now time.Time) *Jar {
	t.Helper()
	j, err := newAtTime(&Options{Filename: file, PersistSessionCookies: persistSessions}, now)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func saveAt(t *testing.T, j *Jar, now time.Time) {
	t.Helper()
	if err := j.save(now); err != nil {
		t.Fatal(err)
	}
}

func TestRetainInactiveCookiesAndMergeAccess(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cookies")
	a := jarAt(t, file, false, tNow)
	setCookies(a, "https://example.com/", []string{"a=value; max-age=604800"}, tNow)
	saveAt(t, a, tNow)
	b := jarAt(t, file, false, tNow)
	access := tNow.Add(23 * time.Hour)
	a.cookiesAtTime(mustParseURL("https://example.com/"), access)
	saveAt(t, a, access)
	saveAt(t, b, tNow.Add(25*time.Hour))
	// Cleanup, saving, and reloading must all preserve an inactive live cookie.
	now := tNow.Add(6 * 24 * time.Hour)
	b.deleteExpiredAtTime(now)
	saveAt(t, b, now)
	c := jarAt(t, file, false, now)
	if got := allCookies(c, now); got != "a=value" {
		t.Fatalf("inactive cookie lost: %q", got)
	}
	for _, submap := range c.entries {
		for _, e := range submap {
			if !e.LastAccess.Equal(access) {
				t.Fatalf("last access = %v, want %v", e.LastAccess, access)
			}
		}
	}
}

func TestSessionReplacementDoesNotResurrect(t *testing.T) {
	for _, persistOther := range []bool{false, true} {
		for _, oldSavesFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("persistOther=%t/oldSavesFirst=%t", persistOther, oldSavesFirst), func(t *testing.T) {
				file := filepath.Join(t.TempDir(), "cookies")
				a := jarAt(t, file, false, tNow)
				setCookies(a, "https://example.com/", []string{"a=old; max-age=604800"}, tNow)
				saveAt(t, a, tNow)
				b := jarAt(t, file, persistOther, tNow)
				now := tNow.Add(time.Second)
				setCookies(a, "https://example.com/", []string{"a=session-secret"}, now)
				if oldSavesFirst {
					saveAt(t, b, now)
				}
				saveAt(t, a, now)
				saveAt(t, b, now)
				saveAt(t, a, now)
				if got := queryJar(a, "https://example.com/", now); got != "a=session-secret" {
					t.Fatalf("local session cookie changed: %q", got)
				}
				if got := queryJar(b, "https://example.com/", now); got != "" {
					t.Fatalf("old cookie resurrected: %q", got)
				}
				c := jarAt(t, file, true, now)
				if got := allCookies(c, now); got != "" {
					t.Fatalf("session value or old cookie loaded: %q", got)
				}
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(data, []byte("session-secret")) {
					t.Fatal("session value persisted")
				}
			})
		}
	}
}

func TestMaxAgeLimits(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, age := range []int{1, maxInt, -1, -maxInt - 1} {
		t.Run(strconv.Itoa(age), func(t *testing.T) {
			j := newTestJar("", false)
			e, err := j.newEntryAtTime(&http.Cookie{Name: "a", Value: "b", MaxAge: age}, tNow, "/", "example.com")
			if err != nil {
				t.Fatal(err)
			}
			if age < 0 {
				if e.Expires.After(tNow) {
					t.Fatal("negative MaxAge did not expire cookie")
				}
				return
			}
			wantAge := time.Duration(math.MaxInt64)
			if int64(age) <= math.MaxInt64/int64(time.Second) {
				wantAge = time.Duration(age) * time.Second
			}
			if !e.Expires.Equal(tNow.Add(wantAge)) || e.Value != "b" {
				t.Fatalf("unexpected capped cookie: %+v", e)
			}
		})
	}
}

func TestSaveEncodingFailurePreservesFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cookies")
	j := jarAt(t, file, false, tNow)
	setCookies(j, "https://example.com/", []string{"a=original; max-age=3600"}, tNow)
	saveAt(t, j, tNow)
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	j.setCookiesAtTime(mustParseURL("https://example.com/"), []*http.Cookie{{
		Name: "bad", Value: "bad", Expires: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
	}}, tNow)
	if err := j.save(tNow); err == nil {
		t.Fatal("expected unrepresentable JSON date to fail")
	}
	if _, err := j.MarshalJSON(); err == nil {
		t.Fatal("MarshalJSON hid encoding failure")
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed save damaged existing file")
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(file), ".cookies-*.tmp"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %v, %v", leftovers, err)
	}
}

func TestSaveAcrossProcesses(t *testing.T) {
	const count = 8
	if file := os.Getenv("COOKIEJAR_TEST_FILE"); file != "" {
		j := newTestJar(file, false)
		for i := 0; i < count; i++ {
			j.SetCookies(mustParseURL("https://example.com/"), []*http.Cookie{{
				Name: fmt.Sprintf("p%s_%d", os.Getenv("COOKIEJAR_TEST_PROCESS"), i), Value: "v", MaxAge: 3600,
			}})
			if err := j.Save(); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	file := filepath.Join(t.TempDir(), "cookies")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var commands []*exec.Cmd
	var outputs []*bytes.Buffer
	for i := 0; i < 3; i++ {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSaveAcrossProcesses$")
		cmd.Env = append(os.Environ(), "COOKIEJAR_TEST_FILE="+file, fmt.Sprintf("COOKIEJAR_TEST_PROCESS=%d", i))
		output := new(bytes.Buffer)
		cmd.Stdout, cmd.Stderr = output, output
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
		outputs = append(outputs, output)
	}
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Errorf("process %d: %v\n%s", i, err, outputs[i])
		}
	}
	j := newTestJar(file, false)
	if got := len(j.AllCookies()); got != 3*count {
		t.Fatalf("concurrent saves lost cookies: got %d, want %d", got, 3*count)
	}
}

func TestSaveCorruptFilePreservesFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cookies")
	j := jarAt(t, file, false, tNow)
	setCookies(j, "https://example.com/", []string{"a=value; max-age=3600"}, tNow)
	corrupt := []byte(`[{"Name":`)
	if err := os.WriteFile(file, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if err := j.save(tNow); err == nil {
		t.Fatal("expected corrupt cookie file to fail the merge")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, corrupt) {
		t.Fatal("failed merge overwrote corrupt file")
	}
}
