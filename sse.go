// Copyright 2015 Julien Schmidt. All rights reserved.
// Use of this source code is governed by MIT license,
// a copy can be found in the LICENSE file.

// Package sse provides HTML5 Server-Sent Events for Go.
//
// See http://www.w3.org/TR/eventsource/ for the technical specification
package sse

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type client chan []byte

// Streamer receives events and broadcasts them to all connected clients.
type Streamer struct {
	event         chan []byte
	clients       map[client]bool
	connecting    chan client
	disconnecting chan client
}

// New returns a new initialized SSE Streamer
func New() *Streamer {
	s := &Streamer{
		event:         make(chan []byte, 1),
		clients:       make(map[client]bool),
		connecting:    make(chan client),
		disconnecting: make(chan client),
	}

	s.run()
	return s
}

// run starts a goroutine to handle client connects and broadcast events.
func (s *Streamer) run() {
	go func() {
		for {
			select {
			case cl := <-s.connecting:
				s.clients[cl] = true

			case cl := <-s.disconnecting:
				delete(s.clients, cl)

			case event := <-s.event:
				for cl := range s.clients {
					cl <- event
				}
			}
		}
	}()
}

func format(id, event string, dataLen int) (p []byte) {
	// calc length
	l := 6 // data\n\n
	if len(event) > 0 {
		l += 6 + len(event) + 1 // event:{event}\n
	}
	if dataLen > 0 {
		l += 1 + dataLen // :{data}
	}

	// build
	p = make([]byte, l)
	i := 0
	if len(event) > 0 {
		copy(p, "event:")
		i += 6 + copy(p[6:], event)
		p[i] = '\n'
		i++
	}
	i += copy(p[i:], "data")
	if dataLen > 0 {
		p[i] = ':'
		i += 1 + dataLen
	}
	copy(p[i:], "\n\n")

	// TODO: id

	return
}

// SendBytes sends an event with the given byte slice interpreted as a string
// as the data value to all connected clients.
// If the id or event string is empty, no id / event type is send.
func (s *Streamer) SendBytes(id, event string, data []byte) {
	p := format(id, event, len(data))
	copy(p[len(p)-(2+len(data)):], data) // fill in data
	s.event <- p
}

// SendInt sends an event with the given int as the data value to all connected
// clients.
// If the id or event string is empty, no id / event type is send.
func (s *Streamer) SendInt(id, event string, data int64) {
	const maxIntToStrLen = 20 // '-' + 19 digits

	p := format(id, event, maxIntToStrLen)
	p = strconv.AppendInt(p[:len(p)-(maxIntToStrLen+2)], data, 10)

	// Re-add \n\n at the end
	p = p[:len(p)+2]
	p[len(p)-2] = '\n'
	p[len(p)-1] = '\n'

	s.event <- p
}

// SendJSON sends an event with the given data encoded as JSON to all connected
// clients.
// If the id or event string is empty, no id / event type is send.
func (s *Streamer) SendJSON(id, event string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.SendBytes(id, event, data)
	return nil
}

// SendString sends an event with the given data string to all connected
// clients.
// If the id or event string is empty, no id / event type is send.
func (s *Streamer) SendString(id, event, data string) {
	p := format(id, event, len(data))
	copy(p[len(p)-(2+len(data)):], data) // fill in data
	s.event <- p
}

// SendUint sends an event with the given unsigned int as the data value to all
// connected clients.
// If the id or event string is empty, no id / event type is send.
func (s *Streamer) SendUint(id, event string, data uint64) {
	const maxUintToStrLen = 20

	p := format(id, event, maxUintToStrLen)
	p = strconv.AppendUint(p[:len(p)-(maxUintToStrLen+2)], data, 10)

	// Re-add \n\n at the end
	p = p[:len(p)+2]
	p[len(p)-2] = '\n'
	p[len(p)-1] = '\n'

	s.event <- p
}

// ServeHTTP implements http.Handler interface.
func (s *Streamer) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	// We need to be able to flush for SSE
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Flushing not supported", http.StatusNotImplemented)
		return
	}

	// Returns a channel that blocks until the connection is closed
	cn, ok := w.(http.CloseNotifier)
	if !ok {
		http.Error(w, "Closing not supported", http.StatusNotImplemented)
		return
	}
	close := cn.CloseNotify()

	// Set headers for SSE
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Content-Type", "text/event-stream")

	// Connect new client
	cl := make(client)
	s.connecting <- cl

	for {
		select {
		case <-close:
			// Disconnect the client when the connection is closed
			s.disconnecting <- cl
			return

		case event := <-cl:
			// Write events
			w.Write(event) // TODO: error handling
			fl.Flush()
		}
	}
}
