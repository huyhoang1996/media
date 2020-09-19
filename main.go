package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-pg/pg"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

const maxUploadSize = 2 * 1024 * 1024 // 2 mb
const uploadPath = "/mediafile"

const LogFieldKeyRequestID = "requestID"

type msgError interface {
	Error() string
}
type ContextKey string // can be unexported

const ContextKeyRequestID ContextKey = "requestID" // can be unexported

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
	// fmt.Println("fileName ", fileName)
	// fmt.Println("fileEndings ", fileEndings)

	if err != nil {
		renderError(w, "CANT_READ_FILE_TYPE", http.StatusInternalServerError)
		return err
	}
	newPath := filepath.Join(os.Getenv("PROJECT_PATH")+uploadPath, fileName+fileEndings[0])
	// fmt.Printf("FileType: %s, File: %s\n", detectedFileType, newPath)

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
	type DBConfig struct {
		user        string
		password    string
		database    string
		addr        string
		search_path string
	}

	env := os.Getenv("ENV")
	dbconfig := DBConfig{"postgres", "huyhoang@123", "media",
		fmt.Sprintf("%s:%d", "192.168.3.212", 5433),
		"media"}
	if env == "PRODUCTION" {
		dbconfig = DBConfig{"postgres", "huyhoang@123", "media", fmt.Sprintf("%s:%d", "127.0.0.1", 5433), "media"}
	}

	db := pg.Connect(&pg.Options{
		User:                  dbconfig.user,
		Password:              dbconfig.password,
		Database:              dbconfig.database,
		Addr:                  dbconfig.addr,
		RetryStatementTimeout: true,
		MaxRetries:            4,
		MinRetryBackoff:       250 * 6000,
		OnConnect: func(conn *pg.Conn) error {
			zone, _ := time.Now().Zone()
			_, err := conn.Exec("set search_path = ?; set timezone = ?", dbconfig.search_path, zone)
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

func AssignRequestID(ctx context.Context) context.Context {
	reqID := uuid.New()
	ctx2 := context.WithValue(ctx, ContextKeyRequestID, reqID.String())
	return ctx2
}

// GetRequestID will get reqID from a http request and return it as a string
func GetRequestID(ctx context.Context) string {
	reqID := ctx.Value(ContextKeyRequestID)
	if ret, ok := reqID.(string); ok {
		return ret
	}
	return ""
}

func deployLog(env string) {
	// open a file
	f, err := os.OpenFile(os.Getenv("PROJECT_PATH")+"testlogrus.log", os.O_APPEND|os.O_CREATE|os.O_RDWR, 0666)
	fmt.Println("=== ERR", err)
	if err != nil {
		fmt.Printf("error opening file: %v", err)
	}
	logrus.SetOutput(os.Stdout)

	if env == "PRODUCTION" {
		// logrus.SetFormatter(&logrus.JSONFormatter{})
		logrus.SetOutput(f)
	}
	// defer f.Close()
}

func reqIDMiddleware1(next func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = AssignRequestID(ctx)
		r = r.WithContext(ctx)
		reqID := GetRequestID(ctx)
		fmt.Println("=== reqID:: ", reqID)

		deployLog(os.Getenv("ENV"))
		logger := logrus.WithField(LogFieldKeyRequestID, reqID)

		logger.Info("Incomming request %s %s %s", r.Method, r.RequestURI, r.RemoteAddr)
		err := next(w, r)
		logger.Error(err)
		logger.Info("Finished handling http req")
	})
}

func init() {
	// Log as JSON instead of the default ASCII formatter.
	// logrus.SetFormatter(&logrus.JSONFormatter{})

	// Output to stdout instead of the default stderr
	// Can be any io.Writer, see below for File example
}

func main() {
	logrus.WithFields(logrus.Fields{
		"animal": "walrus",
		"size":   10,
	}).Info("Start Server")
	http.Handle("/", reqIDMiddleware1(handler))
	log.Fatal(http.ListenAndServe(":8080", nil))
}
