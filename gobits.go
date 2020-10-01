/*
GoBITS - A server implementation of Microsoft BITS (Background Intelligent Transfer Service) written in go.
Copyright (C) 2017  Magnus Andersson
*/

package gobits

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Event if the type of the event for the callback
type Event int

// Events that is sent to the callback
const (
	EventCreateSession Event = 0 // A new session is created
	EventRecieveFile   Event = 1 // a file is recieved
	EventCloseSession  Event = 2 // a session is closed
	EventCancelSession Event = 3 // a session is canceled
)

// CallbackFunc is the function that is called when an event occurs
type CallbackFunc func(event Event, Session, Path string)

// Config contains configuration information
type Config struct {
	TempDir       string   // Directory to store unfinished files in
	AllowedMethod string   // Allowed method name
	Protocol      string   // Protocol to use
	MaxSize       uint64   // Max size of uploaded file
	Allowed       []string // Whitelisted filter
	Disallowed    []string // Blacklisted filter
}

// Handler contains the config and the callback
type Handler struct {
	cfg      Config
	callback CallbackFunc
}

// ErrorContext is the type of the event for the callback
type ErrorContext int

// BITS error constants
// https://msdn.microsoft.com/en-us/library/aa362798(v=vs.85).aspx
const (
	ErrorContextNone                     ErrorContext = 0 // An error has not occurred
	ErrorContextUnknown                  ErrorContext = 1 // The error context is unknown
	ErrorContextGeneralQueueManager      ErrorContext = 2 // The transfer queue manager generated the error
	ErrorContextQueueManagerNotification ErrorContext = 3 // The error was generated while the queue manager was notifying the client of an event
	ErrorContextLocalFile                ErrorContext = 4 // The error was related to the specified local file. For example, permission was denied or the volume was unavailable
	ErrorContextRemoteFile               ErrorContext = 5 // The error was related to the specified remote file. For example, the URL was not accessible
	ErrorContextGeneralTransport         ErrorContext = 6 // The transport layer generated the error. These errors are general transport failures (these errors are not specific to the remote file)
	ErrorContextRemoteApplication        ErrorContext = 7 // The server application that BITS passed the upload file to generated an error while processing the upload file
)

// NewHandler return a new Handler with sane defaults
func NewHandler(cfg Config, cb CallbackFunc) (b *Handler, err error) {
	b = &Handler{
		cfg:      cfg,
		callback: cb,
	}

	// make sure we have a method
	if b.cfg.AllowedMethod == "" {
		b.cfg.AllowedMethod = "BITS_POST"
	}

	// this will probably never change, unless a very custom server is made
	if b.cfg.Protocol == "" {
		// https://msdn.microsoft.com/en-us/library/aa362833(v=vs.85).aspx
		b.cfg.Protocol = "{7df0354d-249b-430f-820d-3d2a9bef4931}" // BITS 1.5 Upload Protocol
	}

	// setup the temporary directory
	if b.cfg.TempDir == "" {
		b.cfg.TempDir = path.Join(os.TempDir(), "gobits")
	}

	// if the allowed filter isn't specified, allow everything
	if len(b.cfg.Allowed) == 0 {
		b.cfg.Allowed = []string{".*"}
	}

	// Make sure all regexp compiles
	for _, n := range b.cfg.Allowed {
		_, err = regexp.Compile(n)
		if err != nil {
			return nil, fmt.Errorf("failed to compile regexp '%s': %v", n, err)
		}
	}
	for _, n := range b.cfg.Disallowed {
		_, err = regexp.Compile(n)
		if err != nil {
			return nil, fmt.Errorf("failed to compile regexp '%s': %v", n, err)
		}
	}

	return
}

// returns a BITS error
func bitsError(w http.ResponseWriter, uuid string, status, code int, context ErrorContext) {
	w.Header().Add("BITS-Packet-Type", "Ack")
	if uuid != "" {
		w.Header().Add("BITS-Session-Id", uuid)
	}
	w.Header().Add("BITS-Error-Code", strconv.FormatInt(int64(code), 16))
	w.Header().Add("BITS-Error-Context", strconv.FormatInt(int64(context), 16))
	w.WriteHeader(status)
	w.Write(nil)
}

// generate a new UUID
func newUUID() (string, error) {
	// Stolen from http://play.golang.org/p/4FkNSiUDMg
	uuid := make([]byte, 16)
	if n, err := io.ReadFull(rand.Reader, uuid); n != len(uuid) || err != nil {
		return "", err
	}

	// https://tools.ietf.org/html/rfc4122#section-4.1.1
	uuid[8] = uuid[8]&^0xc0 | 0x80

	// https://tools.ietf.org/html/rfc4122#section-4.1.3
	uuid[6] = uuid[6]&^0xf0 | 0x40

	return fmt.Sprintf("%x-%x-%x-%x-%x", uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:]), nil
}

func isValidUUID(uuid string) bool {
	const match = "[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}"

	b, _ := regexp.Match(match, []byte(uuid))
	return b
}

// check if file exists
func exists(path string) (bool, error) {
	var err error
	if _, err = os.Stat(path); err != nil && os.IsNotExist(err) {
		return false, nil
	}
	return true, err
}

// parse a HTTP range header
func parseRange(rangeString string) (rangeStart, rangeEnd, fileLength uint64, err error) {

	// We only support "range #-#/#" syntax
	if !strings.HasPrefix(rangeString, "bytes ") {
		return 0, 0, 0, errors.New("invalid range syntax")
	}

	// Remove leading 6 characters
	rangeArray := strings.Split(rangeString[6:], "/")
	if len(rangeArray) != 2 {
		return 0, 0, 0, errors.New("invalid range syntax")
	}

	// Parse total length
	if fileLength, err = strconv.ParseUint(rangeArray[1], 10, 64); err != nil {
		return 0, 0, 0, err
	}

	// Get start and end of range
	rangeArray = strings.Split(rangeArray[0], "-")
	if len(rangeArray) != 2 {
		return 0, 0, 0, errors.New("invalid range syntax")
	}

	// Parse start value
	if rangeStart, err = strconv.ParseUint(rangeArray[0], 10, 64); err != nil {
		return 0, 0, 0, err
	}

	// Parse end value
	if rangeEnd, err = strconv.ParseUint(rangeArray[1], 10, 64); err != nil {
		return 0, 0, 0, err
	}

	// Return values
	return rangeStart, rangeEnd, fileLength, nil

}
