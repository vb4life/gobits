# Go-BITS

[![build status](https://ci.gitlab.com/projects/5773/status.png?ref=master)](https://ci.gitlab.com/projects/5773)

This repository holds my golang implementation of the [BITS Upload Protocol](https://msdn.microsoft.com/en-us/library/aa362828(v=vs.85).aspx).

It has been tested on Windows 10 and Debian 8, so it should&trade; work on all platforms.

## How to get
```
go get gitlab.com/magan/gobits/bitsrv
go install gitlab.com/magan/gobits/bitsrv
```

## Example
You can find a full example [here](https://gitlab.com/magan/gobits/tree/master/example)

In short, it implemets the ServeHTTP handler, so you can use it on an existing webserver written in go by simply adding the following lines of code:
```golang
import gitlab.com/magan/gobits/bitsrv
```

```golang
cb := func(event bitsrv.BITSEvent, Session, Path string) {
	switch event {
	case bitsrv.EventCreateSession:
		fmt.Printf("New session created: %v\n", Session)
	case bitsrv.EventRecieveFile:
		fmt.Printf("New file created: %v\n", Path)
	case bitsrv.EventCloseSession:
		fmt.Printf("Session closed: %v\n", Session)
	case bitsrv.EventCancelSession:
		fmt.Printf("Session canceled: %v\n", Session)
	}
}
bits := bitsrv.NewHandler(bitsrv.Config{}, cb)
http.Handle("/BITS/", bits)
```

After that, test an upload from a windows machine with the following PowerShell command:
```powershell
Start-BitsTransfer -TransferType Upload -Source <path to file to upload> -Destination http://<hostname>:<port>/BITS/<filename>
```

I have not implemented the Upload-Reply part of the protocol, since there seems to be a bit of a shortage of good documentation about it.