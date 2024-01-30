// LogReadWriter write log to websocket

package dashboard

import (
	"io"
	"strings"

	"github.com/gorilla/websocket"
)

type LogWriter struct {
	conn   *websocket.Conn
	stream io.ReadCloser
}

func (l *LogWriter) Write(p []byte) (int, error) {
	splits := strings.Split(string(p), "\n")
	var err error
	for _, v := range splits {
		if v == "" {
			continue
		}
		if err != nil {
			return len(p), nil
		}
		err = l.conn.WriteMessage(websocket.TextMessage, []byte(v))
		if err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

func (l *LogWriter) Read(p []byte) (int, error) {
	return l.stream.Read(p)
}
