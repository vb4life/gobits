/*
GoBITS - A server implementation of Microsoft BITS (Background Intelligent Transfer Service) written in go.
Copyright (C) 2015  Magnus Andersson
*/

package bitsrv

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {

	// Directory to store unfinished files in
	TempDir string

	// Allowed method name
	AllowedMethod string

	// Protocol to use
	Protocol string
	
	// Max size of uploaded file
	MaxSize uint64
}

type BITSHandler struct {
	cfg Config

	callback CallbackFunc
}

type BITSEvent int

const (
	EventCreateSession BITSEvent = iota
	EventRecieveFile
	EventCloseSession
	EventCancelSession
)

type CallbackFunc func(event BITSEvent, Session, Path string)

const (
	BG_ERROR_CONTEXT_NONE                       = 0
	BG_ERROR_CONTEXT_UNKNOWN                    = 1
	BG_ERROR_CONTEXT_GENERAL_QUEUE_MANAGER      = 2
	BG_ERROR_CONTEXT_QUEUE_MANAGER_NOTIFICATION = 3
	BG_ERROR_CONTEXT_LOCAL_FILE                 = 4
	BG_ERROR_CONTEXT_REMOTE_FILE                = 5
	BG_ERROR_CONTEXT_GENERAL_TRANSPORT          = 6
	BG_ERROR_CONTEXT_REMOTE_APPLICATION         = 7
)

func NewHandler(cfg Config, cb CallbackFunc) (b *BITSHandler) {
	b = new(BITSHandler)
	b.cfg = cfg
	b.callback = cb

	// Set defaults
	if b.cfg.AllowedMethod == "" {
		b.cfg.AllowedMethod = "BITS_POST"
	}
	if b.cfg.Protocol == "" {
		// BITS 1.5 Upload Protocol
		// https://msdn.microsoft.com/en-us/library/aa362833(v=vs.85).aspx
		b.cfg.Protocol = "{7df0354d-249b-430f-820d-3d2a9bef4931}"
	}
	if b.cfg.TempDir == "" {
		b.cfg.TempDir = path.Join(os.TempDir(), "gobits")
	}

	return
}

func (b *BITSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only allow BITS requests
	if r.Method != b.cfg.AllowedMethod {
		http.Error(w, "Unauthorized", http.StatusBadRequest)
		return
	}

	packetType := strings.ToLower(r.Header.Get("BITS-Packet-Type"))
	sessionId := r.Header.Get("BITS-Session-Id")

	switch packetType {
	case "ping":
		b.bitsPing(w, r)
	case "create-session":
		b.bitsCreate(w, r)
	case "cancel-session":
		b.bitsCancel(w, r, sessionId)
	case "close-session":
		b.bitsClose(w, r, sessionId)
	case "fragment":
		b.bitsFragment(w, r, sessionId)
	default:
		bitsError(w, "", http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
	}
}

func bitsError(w http.ResponseWriter, uuid string, status, code, context int) {
	w.Header().Add("BITS-Packet-Type", "Ack")
	if uuid != "" {
		w.Header().Add("BITS-Session-Id", uuid)
	}
	w.Header().Add("BITS-Error-Code", strconv.FormatInt(int64(code), 16))
	w.Header().Add("BITS-Error-Context", strconv.FormatInt(int64(context), 16))
	w.WriteHeader(status)
	w.Write(nil)
}

func (b *BITSHandler) bitsPing(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("BITS-Packet-Type", "Ack")
	w.Write(nil)
}

func (b *BITSHandler) bitsCreate(w http.ResponseWriter, r *http.Request) {
	// Check for correct protocol
	var protocol string
	protocols := strings.Split(r.Header.Get("BITS-Supported-Protocols"), " ")
	for _, protocol = range protocols {
		if protocol == b.cfg.AllowedMethod {
			break
		}
	}
	if protocol != b.cfg.Protocol {
		bitsError(w, "", http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Create new session UUID
	uuid, err := newUUID()
	if err != nil {
		bitsError(w, "", http.StatusInternalServerError, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Create session directory
	tmpDir := path.Join(b.cfg.TempDir, uuid)
	if err = os.MkdirAll(tmpDir, 0600); err != nil {
		// Handle error
	}

	b.callback(EventCreateSession, uuid, tmpDir)

	w.Header().Add("BITS-Packet-Type", "Ack")
	w.Header().Add("BITS-Protocol", protocol)
	w.Header().Add("BITS-Session-Id", uuid)
	w.Header().Add("Accept-Encoding", "Identity")
	w.Write(nil)

}

func (b *BITSHandler) bitsFragment(w http.ResponseWriter, r *http.Request, uuid string) {
	var err error

	// Check for correct session
	if uuid == "" {
		bitsError(w, "", http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Check for existing session
	var srcDir string
	srcDir = path.Join(b.cfg.TempDir, uuid)
	if b, _ := exists(srcDir); !b {
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Get filename and make sure the path is correct
	_, filename := path.Split(r.RequestURI)
	if filename == "" {
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Get absolute paths to file
	var src string
	if src, err = filepath.Abs(filepath.Join(srcDir, filename)); err != nil {
		src = filepath.Join(srcDir, filename)
	}

	// Parse range
	var rangeStart, rangeEnd, fileLength uint64
	if rangeStart, rangeEnd, fileLength, err = parseRange(r.Header.Get("Content-Range")); err != nil {
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}
	
	// Check filesize
	if fileLength > b.cfg.MaxSize && b.cfg.MaxSize > 0 {
		bitsError(w, uuid, http.StatusRequestEntityTooLarge, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Get the length of the posted data
	var fragmentSize uint64
	if fragmentSize, err = strconv.ParseUint(r.Header.Get("Content-Length"), 10, 64); err != nil {
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Get posted data and confirm size
	data, err := ioutil.ReadAll(r.Body)
	if uint64(len(data)) != fragmentSize {
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Check that content-range matches content-length
	if rangeEnd-rangeStart+1 != fragmentSize {
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Open file
	var file *os.File
	var fileSize uint64
	if exist, _ := exists(src); !exist {
		// Create file
		if file, err = os.OpenFile(src, os.O_CREATE|os.O_WRONLY, 0600); err != nil {
			bitsError(w, uuid, http.StatusInternalServerError, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
			return
		}

		// New file, size is zero
		fileSize = 0

	} else {
		// Open file for append
		if file, err = os.OpenFile(src, os.O_APPEND|os.O_WRONLY, 0666); err != nil {
			bitsError(w, uuid, http.StatusInternalServerError, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
			return
		}

		// Get size on disk
		if info, err := file.Stat(); err != nil {
			file.Close()
			bitsError(w, uuid, http.StatusInternalServerError, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
			return
		} else {
			fileSize = uint64(info.Size())
		}

	}
	defer file.Close()

	// Sanity checks
	if rangeEnd < fileSize {
		// The range is already written to disk
		w.Header().Add("BITS-Recieved-Content-Range", strconv.FormatUint(fileSize, 10))
		bitsError(w, uuid, http.StatusRequestedRangeNotSatisfiable, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	} else if rangeStart > fileSize {
		// start must be <= fileSize, else there will be a gap
		w.Header().Add("BITS-Recieved-Content-Range", strconv.FormatUint(fileSize, 10))
		bitsError(w, uuid, http.StatusRequestedRangeNotSatisfiable, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Calculate the offset in the slice, if overlapping
	var dataOffset = fileSize - rangeStart

	// Write the data to file
	var written uint64
	if wr, err := file.Write(data[dataOffset:]); err != nil {
		bitsError(w, uuid, http.StatusInternalServerError, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	} else {
		written = uint64(wr)
	}

	// Make sure we wrote everything we wanted
	if written != fragmentSize-dataOffset {
		bitsError(w, uuid, http.StatusInternalServerError, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Check if we have written everything
	if rangeEnd+1 == fileLength {
		// File is done! Move it!
		file.Close()

		// Call the callback
		b.callback(EventRecieveFile, uuid, src)

	}

	w.Header().Add("BITS-Packet-Type", "Ack")
	w.Header().Add("BITS-Session-Id", uuid)
	w.Header().Add("BITS-Received-Content-Range", strconv.FormatUint(fileSize+uint64(written), 10))
	w.Write(nil)

}

func (b *BITSHandler) bitsCancel(w http.ResponseWriter, r *http.Request, uuid string) {
	// Check for correct session
	if uuid == "" {
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}
	destDir := path.Join(b.cfg.TempDir, uuid)
	if exist, _ := exists(destDir); !exist {
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	b.callback(EventCancelSession, uuid, destDir)

	w.Header().Add("BITS-Packet-Type", "Ack")
	w.Header().Add("BITS-Session-Id", uuid)
	w.Write(nil)
}

func (b *BITSHandler) bitsClose(w http.ResponseWriter, r *http.Request, uuid string) {
	// Check for correct session
	if uuid == "" {
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}
	destDir := path.Join(b.cfg.TempDir, uuid)
	if exist, _ := exists(destDir); !exist {
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	b.callback(EventCloseSession, uuid, destDir)

	w.Header().Add("BITS-Packet-Type", "Ack")
	w.Header().Add("BITS-Session-Id", uuid)
	w.Write(nil)
}

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

func exists(path string) (bool, error) {
	var err error
	if _, err = os.Stat(path); err != nil && os.IsNotExist(err) {
		return false, nil
	}
	return true, err
}

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
