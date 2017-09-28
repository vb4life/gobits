package gobits

import (
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ServeHTTP handler
func (b *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only allow BITS requests
	if r.Method != b.cfg.AllowedMethod {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// get packet type and session id
	packetType := strings.ToLower(r.Header.Get("BITS-Packet-Type"))
	sessionID := r.Header.Get("BITS-Session-Id")

	// Take appropriate action based on what type of packet we got
	switch packetType {
	case "ping":
		b.bitsPing(w, r)
	case "create-session":
		b.bitsCreate(w, r)
	case "cancel-session":
		b.bitsCancel(w, r, sessionID)
	case "close-session":
		b.bitsClose(w, r, sessionID)
	case "fragment":
		b.bitsFragment(w, r, sessionID)
	default:
		bitsError(w, "", http.StatusBadRequest, 0, ErrorContextRemoteFile)
	}
}

// use the Ping packet to establish a connection and negotiate security with the server.
// https://msdn.microsoft.com/en-us/library/aa363135(v=vs.85).aspx
func (b *Handler) bitsPing(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("BITS-Packet-Type", "Ack")
	w.Write(nil)
}

// use the Create-Session packet to request an upload session with the BITS server.
// https://msdn.microsoft.com/en-us/library/aa362833(v=vs.85).aspx
func (b *Handler) bitsCreate(w http.ResponseWriter, r *http.Request) {

	// Check for correct protocol
	var protocol string
	protocols := strings.Split(r.Header.Get("BITS-Supported-Protocols"), " ")
	for _, protocol = range protocols {
		if protocol == b.cfg.AllowedMethod {
			break
		}
	}
	if protocol != b.cfg.Protocol {
		// no matching protocol found
		bitsError(w, "", http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}

	// Create new session UUID
	uuid, err := newUUID()
	if err != nil {
		bitsError(w, "", http.StatusInternalServerError, 0, ErrorContextRemoteFile)
		return
	}

	// Create session directory
	tmpDir := path.Join(b.cfg.TempDir, uuid)
	if err = os.MkdirAll(tmpDir, 0600); err != nil {
		bitsError(w, "", http.StatusInternalServerError, 0, ErrorContextRemoteFile)
		return
	}

	// make sure we actually have a callback before calling it
	if b.callback != nil {
		b.callback(EventCreateSession, uuid, tmpDir)
	}

	// https://msdn.microsoft.com/en-us/library/aa362771(v=vs.85).aspx
	w.Header().Add("BITS-Packet-Type", "Ack")
	w.Header().Add("BITS-Protocol", protocol)
	w.Header().Add("BITS-Session-Id", uuid)
	w.Header().Add("Accept-Encoding", "Identity")
	w.Write(nil)

}

// Use the Fragment packet to send a fragment of the upload file to the server
// https://msdn.microsoft.com/en-us/library/aa362842(v=vs.85).aspx
func (b *Handler) bitsFragment(w http.ResponseWriter, r *http.Request, uuid string) {

	// Check for correct session
	if uuid == "" {
		bitsError(w, "", http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}

	// Check for existing session
	var srcDir string
	srcDir = path.Join(b.cfg.TempDir, uuid)
	if b, _ := exists(srcDir); !b {
		bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}

	// Get filename and make sure the path is correct
	_, filename := path.Split(r.RequestURI)
	if filename == "" {
		bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}

	var err error
	var match bool

	// See if filename is blacklisted. If so, return an error
	for _, reg := range b.cfg.Disallowed {
		match, err = regexp.MatchString(reg, filename)
		if err != nil {
			bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
			return
		}
		if match {
			// File is blacklisted
			bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
			return
		}
	}

	// See if filename is whitelisted
	allowed := false
	for _, reg := range b.cfg.Allowed {
		match, err = regexp.MatchString(reg, filename)
		if err != nil {
			bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
			return
		}
		if match {
			allowed = true
			break
		}
	}
	if !allowed {
		// No whitelisting rules matched!
		bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}

	var src string

	// Get absolute paths to file
	src, err = filepath.Abs(filepath.Join(srcDir, filename))
	if err != nil {
		src = filepath.Join(srcDir, filename)
	}

	// Parse range
	var rangeStart, rangeEnd, fileLength uint64
	rangeStart, rangeEnd, fileLength, err = parseRange(r.Header.Get("Content-Range"))
	if err != nil {
		bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}

	// Check filesize
	if b.cfg.MaxSize > 0 && fileLength > b.cfg.MaxSize {
		bitsError(w, uuid, http.StatusRequestEntityTooLarge, 0, ErrorContextRemoteFile)
		return
	}

	// Get the length of the posted data
	var fragmentSize uint64
	fragmentSize, err = strconv.ParseUint(r.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}

	// Get posted data and confirm size
	data, err := ioutil.ReadAll(r.Body) // should probably not read everything into memory like this
	if err != nil {
		bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}
	if uint64(len(data)) != fragmentSize {
		bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}

	// Check that content-range size matches content-length
	if rangeEnd-rangeStart+1 != fragmentSize {
		bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}

	// Open or create file
	var file *os.File
	var fileSize uint64
	var exist bool
	exist, err = exists(src)
	if err != nil {
		bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}
	if exist {
		// Create file
		file, err = os.OpenFile(src, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			bitsError(w, uuid, http.StatusInternalServerError, 0, ErrorContextRemoteFile)
			return
		}
		defer file.Close()

		// New file, size is zero
		fileSize = 0

	} else {
		// Open file for append
		file, err = os.OpenFile(src, os.O_APPEND|os.O_WRONLY, 0666)
		if err != nil {
			bitsError(w, uuid, http.StatusInternalServerError, 0, ErrorContextRemoteFile)
			return
		}
		defer file.Close()

		// Get size on disk
		var info os.FileInfo
		info, err = file.Stat()
		if err != nil {
			bitsError(w, uuid, http.StatusInternalServerError, 0, ErrorContextRemoteFile)
			return
		}
		fileSize = uint64(info.Size())

	}

	// Sanity checks
	if rangeEnd < fileSize {
		// The range is already written to disk
		w.Header().Add("BITS-Recieved-Content-Range", strconv.FormatUint(fileSize, 10))
		bitsError(w, uuid, http.StatusRequestedRangeNotSatisfiable, 0, ErrorContextRemoteFile)
		return
	} else if rangeStart > fileSize {
		// start must be <= fileSize, else there will be a gap
		w.Header().Add("BITS-Recieved-Content-Range", strconv.FormatUint(fileSize, 10))
		bitsError(w, uuid, http.StatusRequestedRangeNotSatisfiable, 0, ErrorContextRemoteFile)
		return
	}

	// Calculate the offset in the slice, if overlapping
	var dataOffset = fileSize - rangeStart

	// Write the data to file
	var written uint64
	var wr int
	wr, err = file.Write(data[dataOffset:])
	if err != nil {
		bitsError(w, uuid, http.StatusInternalServerError, 0, ErrorContextRemoteFile)
		return
	}
	written = uint64(wr)

	// Make sure we wrote everything we wanted
	if written != fragmentSize-dataOffset {
		bitsError(w, uuid, http.StatusInternalServerError, 0, ErrorContextRemoteFile)
		return
	}

	// Check if we have written everything
	if rangeEnd+1 == fileLength {
		// File is done! Manually close it, since the callback probably don't wnat the file to be open
		file.Close()

		// Call the callback
		if b.callback != nil {
			b.callback(EventRecieveFile, uuid, src)
		}

	}

	// https://msdn.microsoft.com/en-us/library/aa362773(v=vs.85).aspx
	w.Header().Add("BITS-Packet-Type", "Ack")
	w.Header().Add("BITS-Session-Id", uuid)
	w.Header().Add("BITS-Received-Content-Range", strconv.FormatUint(fileSize+uint64(written), 10))
	w.Write(nil)

}

// Use the Cancel-Session packet to terminate the upload session with the BITS server.
// https://msdn.microsoft.com/en-us/library/aa362829(v=vs.85).aspx
func (b *Handler) bitsCancel(w http.ResponseWriter, r *http.Request, uuid string) {
	// Check for correct session
	if uuid == "" {
		bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}
	destDir := path.Join(b.cfg.TempDir, uuid)
	exist, err := exists(destDir)
	if err != nil {
		bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}
	if !exist {
		bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}

	// do the callback
	if b.callback != nil {
		b.callback(EventCancelSession, uuid, destDir)
	}

	w.Header().Add("BITS-Packet-Type", "Ack")
	w.Header().Add("BITS-Session-Id", uuid)
	w.Write(nil)
}

// Use the Close-Session packet to tell the BITS server that file upload is complete and to end the session.
// https://msdn.microsoft.com/en-us/library/aa362830(v=vs.85).aspx
func (b *Handler) bitsClose(w http.ResponseWriter, r *http.Request, uuid string) {
	// Check for correct session
	if uuid == "" {
		bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}
	destDir := path.Join(b.cfg.TempDir, uuid)
	exist, err := exists(destDir)
	if err != nil {
		bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}
	if !exist {
		bitsError(w, uuid, http.StatusBadRequest, 0, ErrorContextRemoteFile)
		return
	}

	// do the callback
	if b.callback != nil {
		b.callback(EventCloseSession, uuid, destDir)
	}

	// https://msdn.microsoft.com/en-us/library/aa362712(v=vs.85).aspx
	w.Header().Add("BITS-Packet-Type", "Ack")
	w.Header().Add("BITS-Session-Id", uuid)
	w.Write(nil)
}
