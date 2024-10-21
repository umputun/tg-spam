# simplechat - Toy Chat Server with Spam Protection

This is a toy chat server with spam protection.  
It is a simple example of how to use the tg-spam library.

All the preparation steps are done in the main.go file, and the actual spam check and spam report are implemented in the `web.go` file, specifically in the `postMessageHandler` function.

The application uses a simple in-memory session to authenticate users and stores messages in an SQLite database. The list of users and passwords is set in the main.go file. The app supports dynamic updates and can run with multiple clients that will be synchronized.
