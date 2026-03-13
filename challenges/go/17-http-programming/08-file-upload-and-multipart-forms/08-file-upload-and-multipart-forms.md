# 8. File Upload and Multipart Forms

<!--
difficulty: intermediate
concepts: [multipart-form, file-upload, form-parsing, content-type, max-bytes-reader, mime-type]
tools: [go, curl]
estimated_time: 25m
bloom_level: apply
prerequisites: [http-server, request-body-parsing, error-handling]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [07 - Cookie and Session Management](../07-cookie-and-session-management/07-cookie-and-session-management.md)
- Understanding of HTTP request bodies

## Learning Objectives

After completing this exercise, you will be able to:

- **Parse** multipart form data with `r.ParseMultipartForm`
- **Handle** file uploads by reading the uploaded file from the request
- **Validate** file size and MIME type before saving
- **Serve** uploaded files back to clients

## Why File Uploads

File uploads are a fundamental web operation. HTML forms use `multipart/form-data` encoding to send files alongside regular form fields. Go's `net/http` provides `ParseMultipartForm` and `FormFile` to handle this. Understanding the mechanics protects you from common vulnerabilities like unlimited file sizes and unchecked file types.

## Step 1 -- Accept a File Upload

```bash
mkdir -p ~/go-exercises/file-upload
cd ~/go-exercises/file-upload
go mod init file-upload
mkdir uploads
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	// Limit upload size to 10 MB
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	err := r.ParseMultipartForm(10 << 20) // 10 MB in memory
	if err != nil {
		http.Error(w, "File too large or bad form data", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("document")
	if err != nil {
		http.Error(w, "Missing document field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fmt.Printf("Received: %s (%d bytes)\n", header.Filename, header.Size)

	dst, err := os.Create(filepath.Join("uploads", filepath.Base(header.Filename)))
	if err != nil {
		http.Error(w, "Could not save file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		http.Error(w, "Error saving file", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Uploaded %s (%d bytes)\n", header.Filename, written)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload", uploadHandler)

	fmt.Println("Server starting on :8080")
	http.ListenAndServe(":8080", mux)
}
```

`MaxBytesReader` wraps the body and returns an error if it exceeds the limit. `ParseMultipartForm` parses the form data, storing up to the specified bytes in memory and the rest in temporary files.

### Intermediate Verification

Create a test file and upload it:

```bash
echo "Hello, this is a test file." > testfile.txt
curl -F "document=@testfile.txt" http://localhost:8080/upload
```

Expected: `Uploaded testfile.txt (28 bytes)` and the file appears in `uploads/`.

## Step 2 -- Validate MIME Type

Check the file content type before saving:

```go
func uploadHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		http.Error(w, "File too large", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("document")
	if err != nil {
		http.Error(w, "Missing document field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read first 512 bytes to detect content type
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		http.Error(w, "Cannot read file", http.StatusBadRequest)
		return
	}

	contentType := http.DetectContentType(buf[:n])
	allowed := map[string]bool{
		"text/plain":       true,
		"application/pdf":  true,
		"image/png":        true,
		"image/jpeg":       true,
	}

	if !allowed[contentType] {
		http.Error(w, fmt.Sprintf("File type %s not allowed", contentType), http.StatusBadRequest)
		return
	}

	// Seek back to start after reading for detection
	file.Seek(0, io.SeekStart)

	dst, err := os.Create(filepath.Join("uploads", filepath.Base(header.Filename)))
	if err != nil {
		http.Error(w, "Could not save file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	io.Copy(dst, file)
	fmt.Fprintf(w, "Uploaded %s (type: %s)\n", header.Filename, contentType)
}
```

### Intermediate Verification

```bash
curl -F "document=@testfile.txt" http://localhost:8080/upload
```

Expected: `Uploaded testfile.txt (type: text/plain; charset=utf-8)`

## Step 3 -- Handle Multiple Files and Form Fields

Accept multiple files and regular form fields together:

```go
func multiUploadHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20) // 50 MB total

	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		http.Error(w, "Request too large", http.StatusBadRequest)
		return
	}

	// Read regular form field
	description := r.FormValue("description")
	fmt.Fprintf(w, "Description: %s\n", description)

	// Read multiple files from the "files" field
	files := r.MultipartForm.File["files"]
	for _, header := range files {
		file, err := header.Open()
		if err != nil {
			http.Error(w, "Cannot open file", http.StatusInternalServerError)
			return
		}

		dst, err := os.Create(filepath.Join("uploads", filepath.Base(header.Filename)))
		if err != nil {
			file.Close()
			http.Error(w, "Cannot save file", http.StatusInternalServerError)
			return
		}

		io.Copy(dst, file)
		file.Close()
		dst.Close()

		fmt.Fprintf(w, "Saved: %s (%d bytes)\n", header.Filename, header.Size)
	}
}
```

Register it:

```go
mux.HandleFunc("POST /multi-upload", multiUploadHandler)
```

### Intermediate Verification

```bash
echo "file one" > file1.txt
echo "file two" > file2.txt
curl -F "description=Test batch" -F "files=@file1.txt" -F "files=@file2.txt" http://localhost:8080/multi-upload
```

Expected:

```
Description: Test batch
Saved: file1.txt (9 bytes)
Saved: file2.txt (9 bytes)
```

## Step 4 -- Serve Uploaded Files

Add a handler that serves files from the uploads directory:

```go
func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload", uploadHandler)
	mux.HandleFunc("POST /multi-upload", multiUploadHandler)
	mux.Handle("GET /files/", http.StripPrefix("/files/", http.FileServer(http.Dir("uploads"))))

	fmt.Println("Server starting on :8080")
	http.ListenAndServe(":8080", mux)
}
```

### Intermediate Verification

```bash
curl http://localhost:8080/files/testfile.txt
```

Expected: Contents of the uploaded file.

## Common Mistakes

### Not Limiting Upload Size

**Wrong:** Calling `r.ParseMultipartForm` without `MaxBytesReader`.

**What happens:** A client can send a multi-gigabyte file, exhausting server memory or disk.

**Fix:** Always wrap `r.Body` with `http.MaxBytesReader` before parsing.

### Trusting the Filename

**Wrong:** Using `header.Filename` directly in file paths.

**What happens:** A malicious filename like `../../etc/passwd` writes outside the uploads directory.

**Fix:** Use `filepath.Base(header.Filename)` to strip directory components.

### Trusting Content-Type Header

**Wrong:** Relying on the `Content-Type` sent by the client.

**What happens:** Clients can lie about the file type.

**Fix:** Use `http.DetectContentType` to sniff the actual content.

## Verify What You Learned

1. Upload a text file and confirm it appears in the uploads directory
2. Upload a file larger than the limit and confirm rejection
3. Upload multiple files in one request
4. Access an uploaded file via the file server endpoint

## What's Next

Continue to [09 - Server-Sent Events](../09-server-sent-events/09-server-sent-events.md) to learn how to push real-time updates from server to client.

## Summary

- `r.ParseMultipartForm(maxMemory)` parses multipart form data
- `r.FormFile("name")` retrieves a single uploaded file
- `http.MaxBytesReader` limits the request body size
- Use `http.DetectContentType` to validate file content, not the client-supplied header
- Always sanitize filenames with `filepath.Base` before saving
- `http.FileServer` serves uploaded files back to clients

## Reference

- [http.Request.ParseMultipartForm](https://pkg.go.dev/net/http#Request.ParseMultipartForm)
- [http.Request.FormFile](https://pkg.go.dev/net/http#Request.FormFile)
- [http.MaxBytesReader](https://pkg.go.dev/net/http#MaxBytesReader)
- [http.DetectContentType](https://pkg.go.dev/net/http#DetectContentType)
