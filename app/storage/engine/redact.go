package engine

import (
	"errors"
	"net/url"
	"strings"
)

// redactedValue replaces any secret in a connection string prepared for display
const redactedValue = "xxxxx"

// sensitiveQueryParams are connection-string query parameters known to carry secrets
var sensitiveQueryParams = map[string]struct{}{
	"password":    {},
	"passwd":      {},
	"pwd":         {},
	"sslpassword": {},
}

// RedactConnURL masks credentials in a database connection URL so it can be logged or put in an
// error message. The result is for display only and must never be handed to a driver. Connection
// strings carrying no credentials, sqlite file paths among them, are returned unchanged.
func RedactConnURL(connURL string) string {
	if connURL == "" {
		return ""
	}

	if !strings.Contains(connURL, "://") {
		return redactDSN(connURL)
	}

	u, err := url.Parse(connURL)
	if err != nil {
		// the location of the secret is unknown, so keep nothing but the scheme
		scheme, _, _ := strings.Cut(connURL, "://")
		return scheme + "://" + redactedValue
	}

	userRedacted := false
	if u.User != nil {
		if _, ok := u.User.Password(); ok {
			u.User = url.UserPassword(u.User.Username(), redactedValue)
			userRedacted = true
		}
	}

	query := u.Query()
	queryRedacted := false
	for key, values := range query {
		if _, ok := sensitiveQueryParams[strings.ToLower(key)]; !ok {
			continue
		}
		for i := range values {
			values[i] = redactedValue
		}
		queryRedacted = true
	}

	if !userRedacted && !queryRedacted {
		return connURL // nothing to hide, keep the original spelling instead of a re-encoded one
	}
	if queryRedacted {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

// redactDSN masks the password in a driver dsn shaped like user:password@tcp(host:port)/db. The
// "@tcp(" marker is the same one prepareStoreURL keys on, so anything else, sqlite paths included,
// passes through untouched.
func redactDSN(dsn string) string {
	at := strings.Index(dsn, "@tcp(")
	if at < 0 {
		return dsn
	}
	colon := strings.Index(dsn[:at], ":")
	if colon < 0 {
		return dsn
	}
	return dsn[:colon+1] + redactedValue + dsn[at:]
}

// unwrapURLError strips the *url.Error wrapper, which embeds the raw URL and would put the
// credentials it carries straight back into the message the caller formats.
func unwrapURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}
