package websocket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/primandproper/platform/eventstream"
	tracingnoop "github.com/primandproper/platform/observability/tracing/noop"

	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewUpgrader(T *testing.T) {
	T.Parallel()

	T.Run("nil config uses defaults", func(t *testing.T) {
		t.Parallel()

		u := NewUpgrader(nil, tracingnoop.NewTracerProvider(), nil)
		must.NotNil(t, u)
		test.EqOp(t, defaultHeartbeatInterval, u.heartbeatInterval)
		test.EqOp(t, defaultBufferSize, u.wsUpgrader.ReadBufferSize)
		test.EqOp(t, defaultBufferSize, u.wsUpgrader.WriteBufferSize)
	})

	T.Run("custom config", func(t *testing.T) {
		t.Parallel()

		u := NewUpgrader(nil, tracingnoop.NewTracerProvider(), &Config{
			HeartbeatInterval: 10 * time.Second,
			ReadBufferSize:    2048,
			WriteBufferSize:   4096,
		})
		must.NotNil(t, u)
		test.EqOp(t, 10*time.Second, u.heartbeatInterval)
		test.EqOp(t, 2048, u.wsUpgrader.ReadBufferSize)
		test.EqOp(t, 4096, u.wsUpgrader.WriteBufferSize)
	})
}

func TestUpgrader_UpgradeToEventStream(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		u := NewUpgrader(nil, tracingnoop.NewTracerProvider(), &Config{HeartbeatInterval: time.Hour})
		streamReady := make(chan eventstream.EventStream, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			stream, err := u.UpgradeToEventStream(w, r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			streamReady <- stream
			<-stream.Done()
		}))
		defer server.Close()

		conn, _, err := gorillawebsocket.DefaultDialer.Dial("ws"+server.URL[4:], http.Header{"Origin": {server.URL}})
		must.NoError(t, err)
		defer conn.Close()

		stream := <-streamReady
		must.NotNil(t, stream)
		defer stream.Close()
	})
}

func TestUpgrader_UpgradeToBidirectionalStream(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		u := NewUpgrader(nil, tracingnoop.NewTracerProvider(), &Config{HeartbeatInterval: time.Hour})
		streamReady := make(chan eventstream.BidirectionalEventStream, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			stream, err := u.UpgradeToBidirectionalStream(w, r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			streamReady <- stream
			<-stream.Done()
		}))
		defer server.Close()

		conn, _, err := gorillawebsocket.DefaultDialer.Dial("ws"+server.URL[4:], http.Header{"Origin": {server.URL}})
		must.NoError(t, err)
		defer conn.Close()

		stream := <-streamReady
		must.NotNil(t, stream)
		defer stream.Close()

		test.NotNil(t, stream.Receive())
	})
}

func TestWSStream_Send(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		u := NewUpgrader(nil, tracingnoop.NewTracerProvider(), &Config{HeartbeatInterval: time.Hour})
		received := make(chan *eventstream.Event, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			stream, err := u.UpgradeToEventStream(w, r)
			if err != nil {
				return
			}
			defer stream.Close()

			_ = stream.Send(r.Context(), &eventstream.Event{
				Type:    "test",
				Payload: json.RawMessage(`{"msg":"hello"}`),
			})
			// keep alive briefly so client can read
			time.Sleep(100 * time.Millisecond)
		}))
		defer server.Close()

		conn, _, err := gorillawebsocket.DefaultDialer.Dial("ws"+server.URL[4:], http.Header{"Origin": {server.URL}})
		must.NoError(t, err)
		defer conn.Close()

		go func() {
			var event eventstream.Event
			if readErr := conn.ReadJSON(&event); readErr == nil {
				received <- &event
			}
		}()

		select {
		case event := <-received:
			test.EqOp(t, "test", event.Type)
			test.EqOp(t, `{"msg":"hello"}`, string(event.Payload))
		case <-time.After(2 * time.Second):
			t.Fatalf("did not receive event")
		}
	})

	T.Run("send after close returns error", func(t *testing.T) {
		t.Parallel()

		u := NewUpgrader(nil, tracingnoop.NewTracerProvider(), &Config{HeartbeatInterval: time.Hour})
		streamReady := make(chan eventstream.EventStream, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			stream, err := u.UpgradeToEventStream(w, r)
			if err != nil {
				return
			}
			streamReady <- stream
			<-stream.Done()
		}))
		defer server.Close()

		conn, _, err := gorillawebsocket.DefaultDialer.Dial("ws"+server.URL[4:], http.Header{"Origin": {server.URL}})
		must.NoError(t, err)
		defer conn.Close()

		stream := <-streamReady
		must.NoError(t, stream.Close())

		sendErr := stream.Send(t.Context(), &eventstream.Event{Type: "x"})
		test.Error(t, sendErr)
		test.StrContains(t, sendErr.Error(), "stream closed")
	})
}

func TestWSStream_Done(T *testing.T) {
	T.Parallel()

	T.Run("closes on Close", func(t *testing.T) {
		t.Parallel()

		u := NewUpgrader(nil, tracingnoop.NewTracerProvider(), &Config{HeartbeatInterval: time.Hour})
		streamReady := make(chan eventstream.EventStream, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			stream, err := u.UpgradeToEventStream(w, r)
			if err != nil {
				return
			}
			streamReady <- stream
			<-stream.Done()
		}))
		defer server.Close()

		conn, _, err := gorillawebsocket.DefaultDialer.Dial("ws"+server.URL[4:], http.Header{"Origin": {server.URL}})
		must.NoError(t, err)
		defer conn.Close()

		stream := <-streamReady
		done := stream.Done()
		must.NoError(t, stream.Close())

		select {
		case <-done:
			// expected
		case <-time.After(time.Second):
			t.Fatalf("Done() channel was not closed after Close()")
		}
	})
}

func TestWSStream_Close(T *testing.T) {
	T.Parallel()

	T.Run("idempotent", func(t *testing.T) {
		t.Parallel()

		u := NewUpgrader(nil, tracingnoop.NewTracerProvider(), &Config{HeartbeatInterval: time.Hour})
		streamReady := make(chan eventstream.EventStream, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			stream, err := u.UpgradeToEventStream(w, r)
			if err != nil {
				return
			}
			streamReady <- stream
			<-stream.Done()
		}))
		defer server.Close()

		conn, _, err := gorillawebsocket.DefaultDialer.Dial("ws"+server.URL[4:], http.Header{"Origin": {server.URL}})
		must.NoError(t, err)
		defer conn.Close()

		stream := <-streamReady
		test.NoError(t, stream.Close())
		test.NoError(t, stream.Close())
	})
}

func TestBidirectionalWSStream_Receive(T *testing.T) {
	T.Parallel()

	T.Run("receives client messages", func(t *testing.T) {
		t.Parallel()

		u := NewUpgrader(nil, tracingnoop.NewTracerProvider(), &Config{HeartbeatInterval: time.Hour})
		streamReady := make(chan eventstream.BidirectionalEventStream, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			stream, err := u.UpgradeToBidirectionalStream(w, r)
			if err != nil {
				return
			}
			streamReady <- stream
			<-stream.Done()
		}))
		defer server.Close()

		conn, _, err := gorillawebsocket.DefaultDialer.Dial("ws"+server.URL[4:], http.Header{"Origin": {server.URL}})
		must.NoError(t, err)
		defer conn.Close()

		stream := <-streamReady
		defer stream.Close()

		// Client sends an event
		outgoing := &eventstream.Event{
			Type:    "ping",
			Payload: json.RawMessage(`{"seq":1}`),
		}
		must.NoError(t, conn.WriteJSON(outgoing))

		select {
		case event := <-stream.Receive():
			must.NotNil(t, event)
			test.EqOp(t, "ping", event.Type)
			test.EqOp(t, `{"seq":1}`, string(event.Payload))
		case <-time.After(2 * time.Second):
			t.Fatalf("did not receive event from client")
		}
	})

	T.Run("channel closes when stream is closed", func(t *testing.T) {
		t.Parallel()

		u := NewUpgrader(nil, tracingnoop.NewTracerProvider(), &Config{HeartbeatInterval: time.Hour})
		streamReady := make(chan eventstream.BidirectionalEventStream, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			stream, err := u.UpgradeToBidirectionalStream(w, r)
			if err != nil {
				return
			}
			streamReady <- stream
			<-stream.Done()
		}))
		defer server.Close()

		conn, _, err := gorillawebsocket.DefaultDialer.Dial("ws"+server.URL[4:], http.Header{"Origin": {server.URL}})
		must.NoError(t, err)
		defer conn.Close()

		stream := <-streamReady
		incoming := stream.Receive()

		must.NoError(t, stream.Close())

		select {
		case _, open := <-incoming:
			test.False(t, open, test.Sprintf("Receive channel should be closed"))
		case <-time.After(2 * time.Second):
			t.Fatalf("Receive channel was not closed after stream.Close()")
		}
	})
}
