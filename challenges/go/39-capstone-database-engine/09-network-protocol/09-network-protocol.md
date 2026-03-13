<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Network Protocol

A database is only useful if clients can connect to it over the network. Your task is to implement a PostgreSQL-compatible wire protocol in Go, enabling your database engine to accept connections from standard PostgreSQL clients like `psql`, `pgcli`, or any PostgreSQL driver library. You will implement the startup handshake, simple query protocol, extended query protocol (prepared statements), result set serialization, error reporting, and connection multiplexing. By the end, you will be able to connect to your database with a real PostgreSQL client and execute SQL queries.

## Requirements

1. Implement the PostgreSQL wire protocol message framing: each message consists of a 1-byte message type identifier followed by a 4-byte big-endian length (including the length itself but not the type byte) followed by the payload. Implement `ReadMessage(conn net.Conn) (msgType byte, payload []byte, err error)` and `WriteMessage(conn net.Conn, msgType byte, payload []byte) error`. Handle the special case of the startup message, which has no type byte (just length + payload).

2. Implement the startup handshake: read the startup message containing the protocol version (3.0 = `196608`) and key-value parameters (user, database, etc.). Respond with `AuthenticationOk` (type 'R', int32 0), followed by `ParameterStatus` messages for `server_version`, `server_encoding` (UTF8), `client_encoding`, `DateStyle`, and `integer_datetimes`. Finally, send `ReadyForQuery` (type 'Z', byte 'I' for idle) to indicate the server is ready.

3. Implement the Simple Query protocol: receive a `Query` message (type 'Q') containing a SQL string, execute it against your database engine, and respond with: `RowDescription` (type 'T') listing column names and types, zero or more `DataRow` messages (type 'D') each containing the column values as text-encoded strings, `CommandComplete` (type 'C') with a tag like "SELECT 3" or "INSERT 0 1", and `ReadyForQuery` (type 'Z'). Handle multiple statements in one query string, separated by semicolons.

4. Implement the Extended Query protocol: `Parse` (type 'P') to prepare a named or unnamed statement with parameter placeholders ($1, $2, ...), `Bind` (type 'B') to bind parameter values to a prepared statement creating a portal, `Describe` (type 'D') to request parameter types or result column descriptions, `Execute` (type 'E') to execute a portal with an optional row limit, and `Sync` (type 'S') to complete the extended query cycle. Maintain a per-connection map of prepared statements and portals.

5. Implement error reporting using the `ErrorResponse` message (type 'E') with field-based encoding: severity ('S'), SQLSTATE code ('C'), message ('M'), detail ('D'), hint ('H'), position ('P'). Map your parser and executor errors to appropriate SQLSTATE codes (e.g., '42601' for syntax error, '42P01' for undefined table, '23505' for unique violation). After an error in a transaction, set the transaction status to 'E' (error) in subsequent `ReadyForQuery` messages.

6. Implement a TCP server that listens on a configurable port (default 5432), accepts multiple concurrent client connections, and handles each connection in a separate goroutine. Implement graceful shutdown: on SIGINT/SIGTERM, stop accepting new connections, wait for active queries to complete (with a timeout), then close all connections and shut down. Track active connections for monitoring.

7. Implement connection-level transaction state tracking: the `ReadyForQuery` message includes a byte indicating transaction status ('I' = idle, 'T' = in transaction, 'E' = failed transaction). Track this state as clients issue BEGIN, COMMIT, ROLLBACK, and as errors occur. Implement the `DISCARD ALL` and `RESET ALL` commands for connection cleanup.

8. Write tests covering: startup handshake using a raw TCP connection, simple query protocol for SELECT/INSERT/UPDATE/DELETE, extended query protocol with prepared statements and parameter binding, error response formatting and SQLSTATE codes, concurrent client connections (10 clients issuing queries simultaneously), graceful shutdown (connect, start a query, signal shutdown, verify query completes), and integration tests using the `pgx` Go PostgreSQL driver as a real client connecting to your server.

## Hints

- The PostgreSQL wire protocol documentation is authoritative and detailed. The message format section is your primary reference.
- For the startup message, there is no type byte. Read 4 bytes for the length, then `length - 4` bytes for the payload. The first 4 bytes of the payload are the protocol version.
- Text-format encoding for result values: integers as decimal strings, floats as decimal strings, strings as-is, NULLs as a special -1 length indicator in the DataRow message, booleans as "t"/"f".
- The `pgx` driver (github.com/jackc/pgx) is an excellent test client. Use `pgx.Connect()` with your server's address and verify that basic queries work end-to-end.
- For the extended query protocol, `Parse` + `Bind` + `Execute` + `Sync` is the typical message sequence. The unnamed prepared statement (empty string name) is used for one-shot queries.
- Use `bufio.Reader` and `bufio.Writer` wrapping the TCP connection for efficient I/O. Flush the writer after each complete response sequence.

## Success Criteria

1. The `psql` command-line tool or `pgx` driver can connect to your server and complete the startup handshake.
2. Simple SELECT queries return correctly formatted result sets with accurate row counts in the CommandComplete tag.
3. INSERT, UPDATE, and DELETE queries return correct affected row counts.
4. Prepared statements with parameter binding work correctly for parameterized queries.
5. Syntax errors and execution errors produce well-formed ErrorResponse messages with appropriate SQLSTATE codes.
6. 10 concurrent clients can issue queries without interference, deadlocks, or corrupted responses.
7. Graceful shutdown completes in-flight queries and cleanly closes all connections.

## Research Resources

- [PostgreSQL Frontend/Backend Protocol](https://www.postgresql.org/docs/current/protocol.html)
- [PostgreSQL Message Flow](https://www.postgresql.org/docs/current/protocol-flow.html)
- [PostgreSQL Message Formats](https://www.postgresql.org/docs/current/protocol-message-formats.html)
- [SQLSTATE Error Codes](https://www.postgresql.org/docs/current/errcodes-appendix.html)
- [pgx - PostgreSQL Driver for Go](https://github.com/jackc/pgx)
- [Building a PostgreSQL Wire Protocol Server (Phil Eaton)](https://notes.eatonphil.com/database-basics.html)
