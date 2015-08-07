This repository holds my golang implementation of the [BITS Upload Protocol](https://msdn.microsoft.com/en-us/library/aa362828(v=vs.85).aspx).

## How to get
```
go get gitlab.com/magan/go-bits
```

## Example
Start the server with:
```
go-bits -listen=:8080 -uri=/ -root=./
```

After that, test an upload from a windows machine with the following PowerShell command:
```powershell
Start-BitsTransfer -TransferType Upload -Source <path to file to upload> -Destination http://<hostname>:<port>/<filename>
```

So, assuming that you have started go-bits on the local machine, and that yopu have a file called test.bin in the current PowerShell directory, the command would look like this:
```powershell
Start-BitsTransfer -TransferType Upload -Source test.bin -Destination http://localhost:8080/test.bin
```
