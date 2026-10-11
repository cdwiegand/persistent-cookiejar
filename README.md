# cookiejar

```
import "github.com/cdwiegand/persistent-cookiejar"
```

[Package cookiejar](https://pkg.go.dev/github.com/cdwiegand/persistent-cookiejar) implements an in-memory RFC 6265-compliant http.CookieJar.

This implementation is a fork of `orirawlings/persistent-cookiejar`, which was archived, and `Marmelatze/persistent-cookiejar` which added functionality around deleting old entries by domain, and originally from `net/http/cookiejar` which implements methods for dumping the cookies to persistent storage and retrieving them.

One difference is that deleting a cookie will NOT return it from the jar - previous implementations would return it without the value - this fork (and its parent from Marmelatze) do NOT return deleted cookies.

Cookies that have not been sent or set within 24 hours are pruned from the jar, even if they have not yet expired. Deleted or expired cookies are kept internally (never returned) for 24 hours after they last changed, so that saving alongside another process that still holds the old cookie does not bring it back. Entries loaded from files written by older versions, which lack a last-access time, are treated as accessed at load time.
