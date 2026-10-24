# cookiejar

```
import "github.com/cdwiegand/persistent-cookiejar"
```

[Package cookiejar](https://pkg.go.dev/github.com/cdwiegand/persistent-cookiejar) implements an in-memory RFC 6265-compliant http.CookieJar.

This implementation is a fork of `orirawlings/persistent-cookiejar`, which was archived, and `Marmelatze/persistent-cookiejar` which added functionality around deleting old entries by domain, and originally from `net/http/cookiejar` which implements methods for dumping the cookies to persistent storage and retrieving them.

One difference is that deleting a cookie will NOT return it from the jar - previous implementations would return it without the value - this fork (and its parent from Marmelatze) do NOT return deleted cookies.

Unexpired cookies are retained regardless of inactivity. Applications can implement their own inactivity policy using the removal methods. Deleted or expired cookies are kept internally (never returned) for 24 hours after they last changed, so that saving alongside another process that still holds the old cookie does not bring it back within that window.

Positive `Max-Age` values that would overflow a Go duration are capped at `math.MaxInt64` nanoseconds (approximately 292 years). Negative values always delete the cookie. Cookies with a matching IP address in their Domain attribute are accepted as host-only cookies.

Call `Save()` explicitly when cookies should be written to disk; changes are not saved automatically. Session cookie values stay in memory unless `PersistSessionCookies` is enabled. With it disabled, saving a session cookie writes only an expired, empty-valued record to prevent an older persistent version from returning during a merge.

```go
jar, err := cookiejar.New(&cookiejar.Options{
    Filename: "cookies.json",
    PersistSessionCookies: true, // Restore login sessions across runs, if desired.
})
if err != nil {
    return err
}
client := &http.Client{Jar: jar}
resp, err := client.Get("https://example.com/")
if err != nil {
    return err
}
if err := resp.Body.Close(); err != nil {
    return err
}
if err := jar.Save(); err != nil {
    return err
}
```

Saves use a synced temporary file and replace the cookie file only after writing succeeds. Writers coordinate through a persistent `<filename>.lock` file; leave that file in place. All processes sharing a cookie file should use this version's locking protocol; older versions lock the data file itself and cannot safely write alongside it. Save errors, including corrupt JSON encountered during merging, are returned without replacing the existing file.
