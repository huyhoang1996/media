package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-pg/pg"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

// func handler(w http.ResponseWriter, r *http.Request) {
// 	fmt.Fprintf(w, "Hi there, I love %s!", r.URL.Path[1:])
// 	fmt.Println("method:", r.Method)
// 	if r.Method == "GET" {
// 		crutime := time.Now().Unix()
// 		h := md5.New()
// 		io.WriteString(h, strconv.FormatInt(crutime, 10))
// 		token := fmt.Sprintf("%x", h.Sum(nil))

// 		t, _ := template.ParseFiles("upload.gtpl")
// 		t.Execute(w, token)
// 	} else {
// 		r.ParseMultipartForm(32 << 20)
// 		file, handler, err := r.FormFile("file")
// 		if err != nil {
// 			fmt.Println(err)
// 			return
// 		}
// 		defer file.Close()
// 		fmt.Fprintf(w, "%v", handler.Header)
// 		f, err := os.OpenFile("./media/"+handler.Filename, os.O_WRONLY|os.O_CREATE, 0666)
// 		if err != nil {
// 			fmt.Println(err)
// 			return
// 		}
// 		defer f.Close()
// 		io.Copy(f, file)
// 	}

// }

const maxUploadSize = 2 * 1024 * 1024 // 2 mb
const uploadPath = "./mediafile"

type msgError interface {
	Error() string
}

func renderError(w http.ResponseWriter, message string, statusCode int) {
	w.WriteHeader(http.StatusBadRequest)
	w.Write([]byte(message))
}

func randToken(len int) string {
	b := make([]byte, len)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func handler(w http.ResponseWriter, r *http.Request) error {
	fmt.Fprintf(w, "Hi there, I love %s!", r.URL.Path[1:])
	// validate file size
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		renderError(w, "FILE_TOO_BIG", http.StatusBadRequest)
		return err
	}

	// parse and validate file and post parameters
	file, multipartFileHeader, err := r.FormFile("file")
	if err != nil {
		renderError(w, "INVALID_FILE", http.StatusBadRequest)
		return err
	}
	defer file.Close()
	fmt.Println("=== multipartFileHeader:: ", multipartFileHeader.Filename)

	fileBytes, err := ioutil.ReadAll(file)
	// fmt.Println("=== fileBytes:: ", string(fileBytes))
	if err != nil {
		renderError(w, "INVALID_FILE", http.StatusBadRequest)
		return err
	}

	// check file type, detectcontenttype only needs the first 512 bytes
	detectedFileType := http.DetectContentType(fileBytes)
	switch detectedFileType {
	case "image/jpeg", "image/jpg":
	case "image/gif", "image/png":
	case "application/pdf":
		break
	default:
		renderError(w, "INVALID_FILE_TYPE", http.StatusBadRequest)
		return err
	}
	fileName := randToken(12)
	fileEndings, err := mime.ExtensionsByType(detectedFileType)
	fmt.Println("fileName ", fileName)
	fmt.Println("fileEndings ", fileEndings)

	if err != nil {
		renderError(w, "CANT_READ_FILE_TYPE", http.StatusInternalServerError)
		return err
	}
	newPath := filepath.Join(uploadPath, fileName+fileEndings[0])
	fmt.Printf("FileType: %s, File: %s\n", detectedFileType, newPath)

	// write file
	newFile, err := os.Create(newPath)
	if err != nil {
		renderError(w, "CANT_WRITE_FOLDER", http.StatusInternalServerError)
		return err
	}
	defer newFile.Close() // idempotent, okay to call twice
	if _, err := newFile.Write(fileBytes); err != nil || newFile.Close() != nil {
		renderError(w, "CANT_WRITE_FILE", http.StatusInternalServerError)
		return err
	}

	db := pg.Connect(&pg.Options{
		User:                  "postgres",
		Password:              "huyhoang@123",
		Database:              "media",
		Addr:                  fmt.Sprintf("%s:%d", "127.0.0.1", 5433),
		RetryStatementTimeout: true,
		MaxRetries:            4,
		MinRetryBackoff:       250 * 6000,
		OnConnect: func(conn *pg.Conn) error {
			zone, _ := time.Now().Zone()
			_, err := conn.Exec("set search_path = ?; set timezone = ?", "media", zone)
			if err != nil {
				fmt.Println("Connect Fail")
				fmt.Println("ERR:: ", err)
				return err

			}
			fmt.Println("Connect success")
			return nil
		},
	})

	defer db.Close()

	type media struct {
		Name     string
		TypeFile string
		IsPublic bool
	}
	image := &media{Name: newPath, TypeFile: detectedFileType, IsPublic: false}
	err = db.Insert(image)
	if err != nil {
		return err
		fmt.Println("ERR:: ", err)
	}
	w.Write([]byte(fmt.Sprintf("SUCCESS:: %s", newPath)))
	return nil
}

// ContextKey is used for context.Context value. The value requires a key that is not primitive type.
type ContextKey string // can be unexported

// ContextKeyRequestID is the ContextKey for RequestID
const ContextKeyRequestID ContextKey = "requestID" // can be unexported

// AttachRequestID will attach a brand new request ID to a http request
func AssignRequestID(ctx context.Context) context.Context {

	reqID := uuid.New()
	fmt.Println("reqID:: ", reqID.String())
	ctx2 := context.WithValue(ctx, ContextKeyRequestID, reqID.String())
	fmt.Println("===2 reqID:: ", ctx.Value(ContextKeyRequestID))
	fmt.Println("===2 reqID:: ", ctx2.Value(ContextKeyRequestID))

	return ctx2
}

// GetRequestID will get reqID from a http request and return it as a string
func GetRequestID(ctx context.Context) string {

	reqID := ctx.Value(ContextKeyRequestID)
	fmt.Println("GetRequestID reqID:: ", reqID)

	if ret, ok := reqID.(string); ok {
		return ret
	}

	return ""
}

func deployLog() {
	// open a file
	f, err := os.OpenFile("testlogrus.log", os.O_APPEND|os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		fmt.Printf("error opening file: %v", err)
	}

	// defer f.Close()

	// Log as JSON instead of the default ASCII formatter.
	log.SetFormatter(&log.TextFormatter{})

	// Output to stderr instead of stdout, could also be a file.
	log.SetOutput(f)
}

//#region middlewares

func reqIDMiddleware1(next func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = AssignRequestID(ctx)
		r = r.WithContext(ctx)
		reqID := GetRequestID(ctx)
		env := os.Getenv("ENV")
		if env == "PRODUCTION" {
			deployLog()
		}
		logger := log.WithField(LogFieldKeyRequestID, reqID)

		// Only log the warning severity or above.
		log.SetLevel(log.DebugLevel)
		logger.Infof("Incomming request %s %s %s", r.Method, r.RequestURI, r.RemoteAddr)
		err := next(w, r)
		logger.Error(err)
		logger.Infof("Finished handling http req")
	})
}

const LogFieldKeyRequestID = "requestID"

//#endregion middlewares

func main() {
	// http.HandleFunc("/", reqIDMiddleware1(handler))
	http.Handle("/", reqIDMiddleware1(handler))
	log.Fatal(http.ListenAndServe(":8080", nil))
}
