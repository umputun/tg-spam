# simplechat - Toy Chat Server with Spam Protection

This is a toy chat server with spam protection.  
It is a simple example of how to use the tg-spam library.

All the preparation steps are done in the main.go file, and the actual spam check and spam report are implemented in the `web.go` file, specifically in the `postMessageHandler` function.

The application uses a simple in-memory session to authenticate users and stores messages in an SQLite database. The list of users and passwords is set in the main.go file. The app supports dynamic updates and can run with multiple clients that will be synchronized.

## Not suitable for production

The authentication here exists only so the example has a user name to attach to a message; it is not a model to copy into a real service. Sessions are held in memory with no expiry, which means they stay valid until the process restarts and are lost when it does. User names and passwords are hard-coded in `main.go` and kept as plain text. There is no CSRF protection, no rate limiting and no account lockout, and the server is expected to run over plain HTTP, so the session cookie is marked `Secure` only when the process terminates TLS itself.
