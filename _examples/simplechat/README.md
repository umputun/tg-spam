# simplechat - toy chat server with spam protection

This is a toy chat server with spam protection. 
It is a simple example of how to use tg-spam library.


All the preparation steps done in main.go file and the actual spam check and spam report implemented in the `web.go` file, `postMessageHandler` function.

The application uses a simple in-memory session to authenticate users and stores messages to sqlite database. The list of users and passwords set in `main.go` file. The app supports dynamic updates anc can run with multiple clients will be synchronized. 