# cookiejar

```
import "github.com/cdwiegand/persistent-cookiejar"
```

[Package cookiejar](https://pkg.go.dev/github.com/cdwiegand/persistent-cookiejar) implements an in-memory RFC 6265-compliant http.CookieJar.

This implementation is a fork of `orirawlings/persistent-cookiejar`, which was archived, and `Marmelatze/persistent-cookiejar` which added functionality around deleting old entries by domain, and originally from `net/http/cookiejar` which implements methods for dumping the cookies to persistent storage and retrieving them.

One difference is that deleting a cookie will NOT return it from the jar - previous implementations would return it without the value - this fork (and its parent from Marmelatze) do NOT return deleted cookies.
