package main

import (
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
const uploadPath = "./media"

func renderError(w http.ResponseWriter, message string, statusCode int) {
	w.WriteHeader(http.StatusBadRequest)
	w.Write([]byte(message))
}

func randToken(len int) string {
	b := make([]byte, len)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hi there, I love %s!", r.URL.Path[1:])
	// validate file size
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		renderError(w, "FILE_TOO_BIG", http.StatusBadRequest)
		return
	}

	// parse and validate file and post parameters
	file, _, err := r.FormFile("file")
	if err != nil {
		renderError(w, "INVALID_FILE", http.StatusBadRequest)
		return
	}
	defer file.Close()
	fileBytes, err := ioutil.ReadAll(file)
	if err != nil {
		renderError(w, "INVALID_FILE", http.StatusBadRequest)
		return
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
		return
	}
	fileName := randToken(12)
	fileEndings, err := mime.ExtensionsByType(detectedFileType)
	fmt.Println("fileName ", fileName)
	fmt.Println("fileEndings ", fileEndings)

	if err != nil {
		renderError(w, "CANT_READ_FILE_TYPE", http.StatusInternalServerError)
		return
	}
	newPath := filepath.Join(uploadPath, fileName+fileEndings[0])
	fmt.Printf("FileType: %s, File: %s\n", detectedFileType, newPath)

	// write file
	newFile, err := os.Create(newPath)
	if err != nil {
		renderError(w, "CANT_WRITE_FILE", http.StatusInternalServerError)
		return
	}
	defer newFile.Close() // idempotent, okay to call twice
	if _, err := newFile.Write(fileBytes); err != nil || newFile.Close() != nil {
		renderError(w, "CANT_WRITE_FILE", http.StatusInternalServerError)
		return
	}

	db := pg.Connect(&pg.Options{
		User:                  "postgres",
		Password:              "huyhoang@123",
		Database:              "media",
		Addr:                  fmt.Sprintf("%s:%d", "192.168.3.106", 5433),
		RetryStatementTimeout: true,
		MaxRetries:            4,
		MinRetryBackoff:       250 * 6000,
		OnConnect: func(conn *pg.Conn) error {
			zone, _ := time.Now().Zone()
			_, err := conn.Exec("set search_path = ?; set timezone = ?", "media", zone)
			if err != nil {
				fmt.Println("Connect Fail")
				fmt.Println("ERR:: ", err)

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
		fmt.Println("ERR:: ", err)
	}
	w.Write([]byte(fmt.Sprintf("SUCCESS:: %s", newPath)))

}

func main() {
	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
