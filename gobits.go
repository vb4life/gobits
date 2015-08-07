/*
GoBITS - A server implementation of Microsoft BITS (Background Intelligent Transfer Service) written in go.
Copyright (C) 2015  Magnus Andersson
*/

package main

import (
	"code.google.com/p/gcfg"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	METHOD = "BITS_POST"
	
	// BITS 1.5 Upload Protocol
	// https://msdn.microsoft.com/en-us/library/aa362833(v=vs.85).aspx
	DEFAULT_PROTOCOL = "{7df0354d-249b-430f-820d-3d2a9bef4931}"
	DEFAULT_LISTEN   = ":80"
	DEFAULT_URI      = "/"
	DEFAULT_ROOT     = "."
)

type Config struct {
	Settings struct {
		Listen   string
		URI      string
		Root     string
		TmpDir   string
		Protocol string
	}
}

const (
	BG_ERROR_CONTEXT_NONE                        = 0
  	BG_ERROR_CONTEXT_UNKNOWN                     = 1
  	BG_ERROR_CONTEXT_GENERAL_QUEUE_MANAGER       = 2
  	BG_ERROR_CONTEXT_QUEUE_MANAGER_NOTIFICATION  = 3
  	BG_ERROR_CONTEXT_LOCAL_FILE                  = 4
  	BG_ERROR_CONTEXT_REMOTE_FILE                 = 5
  	BG_ERROR_CONTEXT_GENERAL_TRANSPORT           = 6
	BG_ERROR_CONTEXT_REMOTE_APPLICATION          = 7
)

var (
	cfg Config

	Trace   *log.Logger
	Info    *log.Logger
	Warning *log.Logger
	Error   *log.Logger

	logLevel0 = flag.Bool("qq", false, "Don't print anything, ever!") // Super quiet, don't print warnings
	logLevel1 = flag.Bool("q", false, "Only print errors")            // Only print errors
	logLevel3 = flag.Bool("v", false, "Print informational messages") // Print info messages
	logLevel4 = flag.Bool("vv", false, "Print trace messages")        // Print info and trace

	configFile = flag.String("config", "", "Configuration file to use")

	flagListen   = flag.String("listen", "", "Address to listen on. (i.e. 0.0.0.0:8080)")
	flagProtocol = flag.String("protocol", "", "BITS protocol to use. If you want to use normal BITS, go with the default. Custom client can change this.")
	flagURI      = flag.String("uri", "", "URI to accept uploads from")
	flagRoot     = flag.String("root", "", "Path to store completed files in")
	flagTmpDir   = flag.String("tmpdir", "", "Temporary working directory. Uses system default if not specified")
	
	flagHelp	 = flag.Bool("h", false, "Displays usage")
)

func getSettings() {
	// Setup defaults
	cfg.Settings.Listen = DEFAULT_LISTEN
	cfg.Settings.Protocol = DEFAULT_PROTOCOL
	cfg.Settings.Root = DEFAULT_ROOT
	cfg.Settings.URI = DEFAULT_URI
	cfg.Settings.TmpDir = path.Join(os.TempDir(), "GoBITS")

	// Check loglevel
	if *logLevel0 {
		initLogging(ioutil.Discard, ioutil.Discard, ioutil.Discard, ioutil.Discard) // Discard everything
	} else if *logLevel1 {
		initLogging(ioutil.Discard, ioutil.Discard, ioutil.Discard, os.Stderr) // Only errors are logged
	} else if *logLevel4 {
		initLogging(os.Stdout, os.Stdout, os.Stdout, os.Stderr) // Everything is logged
	} else if *logLevel3 {
		initLogging(ioutil.Discard, os.Stdout, os.Stdout, os.Stderr) // Info, Warn and Err is logged
	} else {
		initLogging(ioutil.Discard, ioutil.Discard, os.Stdout, os.Stderr) // Warn and Err is logged
	}

	if *configFile != "" {
		if err := gcfg.ReadFileInto(&cfg, *configFile); err != nil {
			Warning.Fatal(err)
		}
	}

	if *flagListen != "" {
		cfg.Settings.Listen = *flagListen
	}

	if *flagProtocol != "" {
		cfg.Settings.Protocol = *flagProtocol
	}

	if *flagRoot != "" {
		cfg.Settings.Root = *flagRoot
	}

	if *flagTmpDir != "" {
		cfg.Settings.Root = *flagRoot
	}
	
	if *flagURI != "" {
		cfg.Settings.URI = *flagURI
	}

}

// Make sure the temporary folder and the destination folder exists
func validateSettings() {

	// Check temp folder
	if b, _ := exists(cfg.Settings.TmpDir); !b {
		Info.Printf("%v doesn't exist, trying to create it", cfg.Settings.TmpDir)
		if err := os.MkdirAll(cfg.Settings.TmpDir, 0600); err != nil {
			Error.Fatal(err)
		}
		Info.Printf("%v created", cfg.Settings.TmpDir)
	}


	// Validate root folder
	if b, _ := exists(cfg.Settings.Root); !b {
		Info.Printf("%v doesn't exist, trying to create it", cfg.Settings.Root)
		if err := os.MkdirAll(cfg.Settings.Root, 0666); err != nil {
			Error.Fatal(err)
		}
		Info.Printf("%v created", cfg.Settings.Root)
	}
	
}

// Setup the loghandlers
func initLogging(traceWriter, infoWriter, warnWriter, errWriter io.Writer) {
	Trace = log.New(traceWriter, "TRACE: ", log.Ldate|log.Ltime|log.Lshortfile)
	Info = log.New(infoWriter, " INFO: ", log.Ldate|log.Ltime)
	Warning = log.New(warnWriter, " WARN: ", log.Ldate|log.Ltime)
	Error = log.New(errWriter, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
}

func Usage() {
	fmt.Printf("Usage: %v [-qq|-q|-v|-vv] [args]\n\n", os.Args[0])
	fmt.Printf("Logging:\n")
	fmt.Printf("-q           : %v\n", "Only print errors")
	fmt.Printf("-qq          : %v\n", "Don't print anything, ever!")
	fmt.Printf("-v           : %v\n", "Print informational messages")
	fmt.Printf("-vv          : %v\n\n", "Print debug information")
	fmt.Printf("Config file:\n")
	fmt.Printf("-config=file    : %v\n", "Give the name of a config file")
	fmt.Printf("                : %v\n\n", "Settings given on the command line will override settings from the config file")
	fmt.Printf("Settings:\n")
	fmt.Printf("-listen=ip:port : %v\n", "What endpoint to listen to (i.e. 0.0.0.0:8080)")
	fmt.Printf("-protocol=proto : %v\n", "BITS protocol to use. If you want to use normal BITS, go with the default.")
	fmt.Printf("-uri=path       : %v\n", "Local URI for the webserver. '/' to accept any file and a path to filter by path")
	fmt.Printf("-root=path      : %v\n", "Path to the directory where the complete files will be moved to")
	fmt.Printf("-tmpdir=path    : %v\n", "Path to a directory where the incomplete files are uploaded to")
}

func main() {

	flag.Usage = Usage
	flag.Parse()
	
	if *flagHelp{
		flag.Usage()
		return		
	}

	getSettings()
	validateSettings()

	Trace.Printf("Listen  : %v", cfg.Settings.Listen)
	Trace.Printf("Protocol: %v", cfg.Settings.Protocol)
	Trace.Printf("Root    : %v", cfg.Settings.Root)
	Trace.Printf("URI     : %v", cfg.Settings.URI)
	Trace.Printf("TmpDir  : %v", cfg.Settings.TmpDir)

	http.HandleFunc(cfg.Settings.URI, bitsHandler)
	Error.Fatal(http.ListenAndServe(cfg.Settings.Listen, nil))
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

func bitsHandler(w http.ResponseWriter, r *http.Request) {
	Trace.Println("bitsHandler")
	Trace.Println(r)

	// Only allow BITS requests
	if r.Method != METHOD {
		Warning.Printf("unknown method: %v", r.Method)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	packetType := strings.ToLower(r.Header.Get("BITS-Packet-Type"))
	sessionId := r.Header.Get("BITS-Session-Id")

	switch packetType {
	case "ping":
		bitsPing(w, r)
	case "create-session":
		bitsCreate(w, r)
	case "cancel-session":
		bitsCancel(w, r, sessionId)
	case "close-session":
		bitsClose(w, r, sessionId)
	case "fragment":
		bitsFragment(w, r, sessionId)
	default:
		bitsError(w, "", http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
	}
}

func bitsPing(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("BITS-Packet-Type", "Ack")
	w.Write(nil)
}

func bitsCreate(w http.ResponseWriter, r *http.Request) {
	Trace.Println("bitsCreate")

	// Check for correct protocol
	var protocol string
	Trace.Println("Checking for matching protocol...")
	protocols := strings.Split(r.Header.Get("BITS-Supported-Protocols"), " ")
	for _, protocol = range protocols {
		if protocol == cfg.Settings.Protocol {
			Trace.Println("Match found: %v", protocol)
			break
		}
	}
	if protocol != cfg.Settings.Protocol {
		Warning.Printf("unknown protocols: %v", protocols)
		bitsError(w, "", http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Create new session UUID
	Trace.Println("Creating new UUID")
	uuid, err := newUUID()
	if err != nil {
		Error.Println(err)
		bitsError(w, "", http.StatusInternalServerError, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}
	Trace.Println(uuid)

	// Create session directory
	tmpDir := path.Join(cfg.Settings.TmpDir, uuid)
	Trace.Println("Creating directory %v", tmpDir)
	if err = os.MkdirAll(tmpDir, 0600); err != nil {
		Error.Println(err)
	}

	w.Header().Add("BITS-Packet-Type", "Ack")
	w.Header().Add("BITS-Protocol", protocol)
	w.Header().Add("BITS-Session-Id", uuid)
	w.Header().Add("Accept-Encoding", "Identity")
	w.WriteHeader(http.StatusCreated)
	w.Write(nil)

	Trace.Println("~bitsCreate")
}

func bitsFragment(w http.ResponseWriter, r *http.Request, uuid string) {
	Trace.Println("bitsFragment")

	var err error

	// Check for correct session
	if uuid == "" {
		Warning.Println("empty session")
		bitsError(w, "", http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Check for existing session
	var srcDir string
	srcDir = path.Join(cfg.Settings.TmpDir, uuid)
	if b, _ := exists(srcDir); !b {
		Warning.Printf("invalid session: %v", uuid)
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Get filename and make sure the path is correct
	URI, filename := path.Split(r.RequestURI)
	if URI != cfg.Settings.URI {
		Warning.Printf("invalid URI: %v", URI)
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}
	if filename == "" {
		Warning.Printf("missing filename: %v", r.RequestURI)
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Get absolute paths to file
	var src, dst string
	if src, err = filepath.Abs(filepath.Join(srcDir, filename)); err != nil {
		Warning.Printf("Failed to get absolute path: %v", err)
		src = filepath.Join(srcDir, filename)
	}
	if dst, err = filepath.Abs(filepath.Join(cfg.Settings.Root, filename)); err != nil {
		Warning.Printf("Failed to get absolute path: %v", err)
		dst = filepath.Join(cfg.Settings.Root, filename)
	}

	// Parse range
	var rangeStart, rangeEnd, fileLength uint64
	if rangeStart, rangeEnd, fileLength, err = parseRange(r.Header.Get("Content-Range")); err != nil {
		Warning.Printf("failed to parse range: %v", err)
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Get the length of the posted data
	var fragmentSize uint64
	if fragmentSize, err = strconv.ParseUint(r.Header.Get("Content-Length"), 10, 64); err != nil {
		Warning.Printf("Invalid content-length: %v", err)
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Get posted data and confirm size
	data, err := ioutil.ReadAll(r.Body)
	if uint64(len(data)) != fragmentSize {
		Warning.Printf("Invalid content-length: %v, %v", len(data), fragmentSize)
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Check that content-range matches content-length
	if rangeEnd - rangeStart + 1 != fragmentSize {
		Warning.Printf("invalid content-range: (range size: %v, content-length: %v)", rangeEnd - rangeStart + 1, fragmentSize)
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Open file
	var file *os.File
	var fileSize uint64
	if exist, _ := exists(src); !exist {
		// Create file
		if file, err = os.OpenFile(src, os.O_CREATE|os.O_WRONLY, 0600); err != nil {
			Error.Println(err)
			bitsError(w, uuid, http.StatusInternalServerError, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
			return
		}

		// New file, size is zero
		fileSize = 0

	} else {
		// Open file for append
		if file, err = os.OpenFile(src, os.O_APPEND|os.O_WRONLY, 0666); err != nil {
			Error.Println(err)
			bitsError(w, uuid, http.StatusInternalServerError, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
			return
		}

		// Get size on disk
		if info, err := file.Stat(); err != nil {
			file.Close()
			Error.Printf("Stat: %v", err)
			bitsError(w, uuid, http.StatusInternalServerError, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
			return
		} else  {
			fileSize = uint64(info.Size())
		}			

	}
	defer file.Close()

	// Sanity checks
	if rangeEnd < fileSize {
		// The range is already written to disk
		Warning.Printf("Range is already written to disk: %v, %v", rangeEnd, fileSize)
		bitsError(w, uuid, http.StatusRequestedRangeNotSatisfiable, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	} else if rangeStart > fileSize {
		// start must be <= fileSize, else there will be a gap
		Warning.Printf("Range is too far ahead: %v, %v", rangeStart, fileSize)
		bitsError(w, uuid, http.StatusRequestedRangeNotSatisfiable, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Calculate the offset in the slice, if overlapping
	var dataOffset = fileSize - rangeStart

	// Write the data to file
	var written uint64
	if wr, err := file.Write(data[dataOffset:]); err != nil {
		Error.Println(err)
		bitsError(w, uuid, http.StatusInternalServerError, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	} else {
		written =  uint64(wr)
	}

	// Make sure we wrote everything we wanted
	if written != fragmentSize - dataOffset {
		Error.Printf("Not all data was written do disk! (written: %v, data: %v)", written, fragmentSize - dataOffset)
		bitsError(w, uuid, http.StatusInternalServerError, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	Info.Printf("Appended %v byte(s) to file %v, session %v", written, filename, uuid)

	// Check if we have written everything
	if rangeEnd + 1 == fileLength {
		Info.Printf("File %v, session %v is complete (%v bytes)!", filename, uuid, fileLength)
		// File is done! Move it!
		file.Close()
		if err = moveFile(src, dst); err != nil {
			Error.Println(err)
		}
	}

	w.Header().Add("BITS-Packet-Type", "Ack")
	w.Header().Add("BITS-Session-Id", uuid)
	w.Header().Add("BITS-Received-Content-Range", strconv.FormatUint(fileSize+uint64(written), 10))
	w.Write(nil)

	Trace.Println("~bitsFragment")
}

func bitsCancel(w http.ResponseWriter, r *http.Request, uuid string) {
	Trace.Println("bitsCancel")

	// Check for correct session
	if uuid == "" {
		Warning.Printf("empty session")
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}
	destDir := path.Join(cfg.Settings.TmpDir, uuid)
	if exist, _ := exists(destDir); !exist {
		Warning.Printf("invalid session: %v", uuid)
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Remove session folder and all files in it
	if err := os.RemoveAll(destDir); err != nil {
		Error.Println(err)
	}

	w.Header().Add("BITS-Packet-Type", "Ack")
	w.Header().Add("BITS-Session-Id", uuid)
	w.Write(nil)

	Trace.Println("~bitsCancel")
}

func bitsClose(w http.ResponseWriter, r *http.Request, uuid string) {
	Trace.Println("bitsClose")

	// Check for correct session
	if uuid == "" {
		Warning.Printf("empty session")
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}
	destDir := path.Join(cfg.Settings.TmpDir, uuid)
	if exist, _ := exists(destDir); !exist {
		Warning.Printf("invalid session: %v", uuid)
		bitsError(w, uuid, http.StatusBadRequest, 0, BG_ERROR_CONTEXT_REMOTE_FILE)
		return
	}

	// Remove session folder and all files in it
	if err := os.RemoveAll(destDir); err != nil {
		Error.Println(err)
	}

	w.Header().Add("BITS-Packet-Type", "Ack")
	w.Header().Add("BITS-Session-Id", uuid)
	w.Write(nil)

	Trace.Println("~bitsClose")
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

func moveFile(src, dst string) (err error) {
	var fs os.FileInfo
	if fs, err = os.Stat(src); err != nil {
		return err
	}
	if !fs.Mode().IsRegular() {
		return fmt.Errorf("source must be a file")
	}
	
	var fd os.FileInfo
	if fd, err = os.Stat(dst); err != nil {
		if !os.IsNotExist(err) {
			// Some error with Stat
			return err
		}
		// File doesnt exist
	} else {
		// File exists
		if !fd.Mode().IsRegular() {
			return fmt.Errorf("destination must be a file", dst)
		}
		if os.SameFile(fs, fd) {
			// No need to move the file, they are the same
			return nil
		}
	}

	// Best solution: Create a hard link and remove the old file
	if err = os.Link(src, dst); err != nil {
		// Ok, try and rename the file (move it)
		if err = os.Rename(src, dst); err != nil {
			// Failed to move it, then copy it
			if err = copyFileContents(src, dst); err != nil {
				// Well, what else can we do!?
				return err
			}
		}
	}
	err = os.Remove(src)
	return err
}

// copyFileContents copies the contents of the file named src to the file named
// by dst. The file will be created if it does not already exist. If the
// destination file exists, all it's contents will be replaced by the contents
// of the source file.
func copyFileContents(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return
	}
	err = out.Sync()
	return
}
