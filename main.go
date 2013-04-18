package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"compress/gzip"
	"encoding/base64"

	"archive/zip"

	"bitbucket.org/kardianos/osext"
)

const CONFIG_APPEND = "Config.json"

var port *int
var root *string
var workingDir string

var useGzip *bool

var cert *string
var key *string

var allowIP *string

var allowUpload *bool
var uploadDir *string

var username *string
var password *string
var realm *string

var allowZipDir *bool

var useAuth bool

func getConfigFilePath() (string, error) {
	//Get Config filename
	//remove any extension from executable, then append CONFIG_APPEND const ("Config.json")
	// so staticserv.exe OR staticserv -> StaticServConfig.json

	exeAbsPath, err := osext.GetExePath()
	if err != nil {
		return "", err
	}
	dir, exe := filepath.Split(exeAbsPath)
	ext := filepath.Ext(exe)
	return filepath.Join(dir, exe[:len(exe)-len(ext)]+CONFIG_APPEND), nil
}

func printHelp() {
	flag.PrintDefaults()
}

type config struct {
	Port        int
	AllowOnly   string
	AllowUpload bool
	UploadDir   string
	TlsCert     string
	TlsKey      string
	UseGzip     bool
	AuthUser    string
	AuthPass    string
	AuthRealm   string
	AllowZip    bool
	MimeMap     map[string]string
}

func defaultConfig() *config {
	return &config{
		Port:        9000,
		AllowOnly:   "",
		AllowUpload: false,
		UploadDir:   "",
		TlsCert:     "",
		TlsKey:      "",
		UseGzip:     false,
		AuthUser:    "",
		AuthPass:    "",
		AuthRealm:   "",
		AllowZip:    true,
		MimeMap: map[string]string{
			".png": "image/png",
			".apk": "application/vnd.android.package-archive",
			".cod": "application/vnd.rim.cod",
			".jad": "text/vnd.sun.j2me.app-descriptor",
		},
	}
}

func main() {
	fmt.Printf("Doc Root: %s\nListen On: :%d\n", *root, *port)
	server := FileServer(*root, "")
	//http.HandleFunc("/", server.runRoot)
	http.HandleFunc("/$up", runUp)
	http.HandleFunc("/$choose", runChoose)
	if *cert != "" && *key != "" {
		var err = http.ListenAndServeTLS(":"+strconv.Itoa(*port), *cert, *key, http.HandlerFunc(makeGzipHandler(server)))
		if err != nil {
			fmt.Printf("Could not start http server:\n%s", err.Error())
		}
	} else {
		var err = http.ListenAndServe(":"+strconv.Itoa(*port), http.HandlerFunc(makeGzipHandler(server)))
		if err != nil {
			fmt.Printf("Could not start http server:\n%s", err.Error())
		}
	}
}

type fileHandler struct {
	root   string
	prefix string
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func makeGzipHandler(handler http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if *useGzip == false || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			handler.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		handler.ServeHTTP(gzipResponseWriter{Writer: gz, ResponseWriter: w}, r)
	}
}

// FileServer returns a handler that serves HTTP requests
// with the contents of the file system rooted at root.
// It strips prefix from the incoming requests before
// looking up the file name in the file system.
func FileServer(root, prefix string) http.Handler { return &fileHandler{root, prefix} }

func (f *fileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		}
	}()

	if useAuth {
		auth, _ := isAuthorized(r)
		if !auth {
			sendUnAuth(w)
			return
		}
	}

	urlPath := path.Clean(r.URL.Path)
	//remove any prefix to the url path like /root/files.txt
	if !strings.HasPrefix(urlPath, f.prefix) {
		http.NotFound(w, r)
		return
	}
	urlPath = urlPath[len(f.prefix):]

	if len(*allowIP) > 0 {
		ss := strings.Split(r.RemoteAddr, ":")

		if len(ss) < 1 || ss[0] != *allowIP {
			http.NotFound(w, r)

			fmt.Printf("DENY: %s  FOR: %s\n", r.RemoteAddr, urlPath)
			return
		}
	}

	//remove request header to always serve fresh
	r.Header.Del("If-Modified-Since")

	if *allowUpload {
		switch urlPath {
		case "/$choose":
			runChoose(w, r)
			return
		case "/$up":
			runUp(w, r)
			return
		}
	}

	serveFile := filepath.Join(f.root, filepath.FromSlash(urlPath))
	fileStat, err := os.Stat(serveFile)
	if err != nil {
		if os.IsNotExist(err) {
			// Ignore the common favicon request.
			if strings.HasSuffix(serveFile, "favicon.ico") {
				return
			}
			fmt.Printf("Not Found: %s\n", serveFile)
			return
		}
		fmt.Fprintf(os.Stderr, "Error at file <%s>: %s\n", serveFile, err)
		return
	}

	getZip := fileStat.IsDir() && *allowZipDir && r.URL.Query().Get("o") == "zip"
	flags := []string{}
	if getZip {
		flags = append(flags, "zip")
	}

	fmt.Printf("ALLOW: %s  FOR: %s [%s]\n", r.RemoteAddr, urlPath, strings.Join(flags, ","))

	if getZip {
		zipFileName := "file.zip"
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+zipFileName+`"`)

		zw := zip.NewWriter(w)
		defer zw.Close()
		// Walk directory.
		filepath.Walk(serveFile, func(path string, info os.FileInfo, err error) error {
			if info.IsDir() {
				return nil
			}
			// Remove base path, convert to forward slash.
			zipPath := path[len(serveFile):]
			ze, err := zw.Create(strings.Replace(zipPath, `\`, "/", -1))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Cannot create zip entry <%s>: %s\n", zipPath, err)
				return err
			}
			file, err := os.Open(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Cannot open file <%s>: %s\n", path, err)
				return err
			}
			defer file.Close()

			io.Copy(ze, file)
			return nil
		})

		return
	}

	http.ServeFile(w, r, serveFile)
}

func isAuthorized(r *http.Request) (hasAuth, tried bool) {
	hasAuth = false
	tried = false

	a := r.Header.Get("Authorization")
	if a == "" {
		return
	}
	tried = true

	basic := "Basic "
	index := strings.Index(a, basic)
	if index < 0 {
		return
	}

	upString, err := base64.StdEncoding.DecodeString(a[index+len(basic):])
	if err != nil {
		return
	}
	up := strings.SplitN(string(upString), ":", 2)
	if len(up) != 2 {
		return
	}

	if *username != up[0] || *password != up[1] {
		return
	}

	hasAuth = true
	return
}

func sendUnAuth(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf("Basic realm=%q", *realm))
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte("unauthorized"))
}

func runUp(w http.ResponseWriter, req *http.Request) {
	//err := req.ParseMultipartForm(60000)
	//if err != nil {
	//	panic(err.String())
	//}
	formFile, formHead, err := req.FormFile("TheFile")
	if err != nil {
		return
	}
	defer formFile.Close()

	//remove any directory names in the filename
	//START: work around IE sending full filepath and manually get filename
	itemHead := formHead.Header["Content-Disposition"][0]
	lookfor := "filename=\""
	fileIndex := strings.Index(itemHead, lookfor)
	if fileIndex < 0 {
		panic("runUp: no filename")
	}
	filename := itemHead[fileIndex+len(lookfor):]
	filename = filename[:strings.Index(filename, "\"")]

	slashIndex := strings.LastIndex(filename, "\\")
	if slashIndex > 0 {
		filename = filename[slashIndex+1:]
	}
	slashIndex = strings.LastIndex(filename, "/")
	if slashIndex > 0 {
		filename = filename[slashIndex+1:]
	}
	_, saveToFilename := filepath.Split(filename)
	//END: work around IE sending full filepath

	//join the filename to the upload dir
	saveToFilePath := filepath.Join(*uploadDir, saveToFilename)

	osFile, err := os.Create(saveToFilePath)
	if err != nil {
		panic(err.Error())
	}
	defer osFile.Close()

	count, err := io.Copy(osFile, formFile)
	if err != nil {
		panic(err.Error())
	}
	fmt.Printf("ALLOW: %s SAVE: %s (%d)\n", req.RemoteAddr, saveToFilename, count)
	w.Write([]byte("Upload Complete for " + filename))
}

func runChoose(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	var text = `
<!DOCTYPE html>
<html>
<head>

<script type="text/javascript">
var fileName = '';
function fileSelected() {
	try {
		var file = document.getElementById('TheFile').files[0];
		if (file) {
			fileName = file.name;
		}
	} catch(err) {
		//nothing
	}
	uploadFile();
}

function uploadFile() {
	try {
		var fd = new FormData();
		fd.append("TheFile", document.getElementById('TheFile').files[0]);
		var xhr = new XMLHttpRequest();
		xhr.upload.addEventListener("progress", uploadProgress, false);
		xhr.addEventListener("load", uploadComplete, false);
		xhr.addEventListener("error", uploadFailed, false);
		xhr.addEventListener("abort", uploadCanceled, false);
		xhr.open("POST", "/$up");
		xhr.send(fd);
	} catch(err) {
		document.getElementById("fileForm").submit();
	}
}

function uploadProgress(event) {
	if (evt.lengthComputable) {
		var percentComplete = Math.round(event.loaded * 100 / event.total);
		document.getElementById('progressNumber').innerHTML = percentComplete.toString() + '%';
	}
}

function uploadComplete(event) {
	document.getElementById('progressNumber').innerHTML = 'Upload Complete for ' + fileName;
}

function uploadFailed(event) {
	document.getElementById('progressNumber').innerHTML = 'Error';
}

function uploadCanceled(event) {
	document.getElementById('progressNumber').innerHTML = 'Upload canceled';
}
</script>

</head>
<body>
<form action="/$up" id="fileForm" enctype="multipart/form-data" method="post">
	<input type="file" name="TheFile" id="TheFile" onchange="fileSelected()" style="width: 600px; height: 40px; background: gray;"><BR>
	<div id="progressNumber"></div>
</from>
</body>
</html>
	`
	w.Write([]byte(text))
}

func init() {
	defaultConfigFilePath, err := getConfigFilePath()
	if err != nil {
		panic(err.Error())
	}

	//load config file here
	config := defaultConfig()

	configFile, err := os.Open(defaultConfigFilePath)
	if err == nil {
		decode := json.NewDecoder(configFile)
		decode.Decode(config)
	}

	for ext, mimeType := range config.MimeMap {
		err := mime.AddExtensionType(ext, mimeType)
		if err != nil {
			panic(err.Error())
		}
	}

	if workingDir, err = os.Getwd(); err != nil {
		panic(err.Error())
	}

	port = flag.Int("port", config.Port, "HTTP port number")
	root = flag.String("root", workingDir, "Doc Root")

	cert = flag.String("cert", config.TlsCert, "TLS cert.pem")
	key = flag.String("key", config.TlsKey, "TLS key.pem")

	allowIP = flag.String("allow", config.AllowOnly, "Only allow address to connect")

	useGzip = flag.Bool("gzip", config.UseGzip, "Use GZip compression when serving files")

	allowUpload = flag.Bool("up", config.AllowUpload, "Allow uploads to /$choose, /$up")
	uploadDir = flag.String("upTo", config.UploadDir, "Upload files to directory")

	username = flag.String("username", config.AuthUser, "Basic Auth Username")
	password = flag.String("password", config.AuthPass, "Basic Auth Password")
	realm = flag.String("realm", config.AuthRealm, "Basic Auth Realm")

	allowZipDir = flag.Bool("zip", config.AllowZip, "Zip children when URL query: ?o=zip")

	var printConfig = flag.Bool("printConfig", false, "Prints default Config file")
	var help = flag.Bool("h", false, "Prints help")

	flag.Parse()

	if *printConfig {
		//print indented json config file
		jsonBytes, err := json.Marshal(defaultConfig())
		if err != nil {
			panic(err.Error())
		}
		buffer := new(bytes.Buffer)
		json.Indent(buffer, jsonBytes, "", "\t")
		fmt.Printf("%s\n", buffer.String())
		os.Exit(0)
	}

	if *help {
		printHelp()
		os.Exit(0)
	}

	*root, err = filepath.Abs(*root)
	if err != nil {
		panic(err.Error())
	}
	*uploadDir, err = filepath.Abs(*uploadDir)
	if err != nil {
		panic(err.Error())
	}

	useAuth = (len(*username) != 0 || len(*password) != 0)
}
