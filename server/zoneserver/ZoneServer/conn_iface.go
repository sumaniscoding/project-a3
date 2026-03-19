package main

// WSConn is a narrow interface over *websocket.Conn so that production code
// and unit-test stubs both satisfy the same contract.  The real implementation
// (gorilla/websocket) satisfies this automatically; test helpers only need to
// implement WriteJSON.
type WSConn interface {
	WriteJSON(v interface{}) error
	Close() error
}
