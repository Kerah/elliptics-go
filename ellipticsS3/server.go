package ellipticsS3

import (
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
)

var (
	rift S3Backend
)

// Buckets

func bucketExists(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Not implemented")
}

func bucketCreate(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucket := vars["bucket"]

	err := rift.CreateBucket(bucket)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	fmt.Fprintf(w, "OK")
}

// Objects

func objectGet(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucket := vars["bucket"]
	key := vars["key"]

	data, err := rift.GetObject(key, bucket)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	fmt.Fprintf(w, "%s", data)
}

func objectPut(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucket := vars["bucket"]
	key := vars["key"]

	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer r.Body.Close()

	// boto client check ETag header for proper MD5 summ
	h := md5.New()
	h.Write(data)
	etag := fmt.Sprintf("\"%x\"", h.Sum(nil))
	w.Header().Set("ETag", etag)

	err = rift.UploadObject(key, bucket, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	fmt.Fprintf(w, "OK")
}

func objectExists(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucket := vars["bucket"]
	key := vars["key"]

	exists, err := rift.ObjectExists(key, bucket)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if exists {
		fmt.Fprintf(w, "")
	} else {
		http.Error(w, "", http.StatusNotFound)
	}
}

func GetRouter(endpoint string) (h http.Handler, err error) {
	rift, err = NewRiftbackend(endpoint)
	if err != nil {
		return
	}
	//main router
	router := mux.NewRouter()
	router.StrictSlash(true)
	// buckets
	router.HandleFunc("/{bucket}/", bucketExists).Methods("HEAD")
	router.HandleFunc("/{bucket}/", bucketCreate).Methods("PUT")
	// objects
	router.HandleFunc("/{bucket}/{key}", objectExists).Methods("HEAD")
	router.HandleFunc("/{bucket}/{key}", objectGet).Methods("GET")
	router.HandleFunc("/{bucket}/{key}", objectPut).Methods("PUT")
	// debug
	h = handlers.LoggingHandler(os.Stdout, router)
	return
}