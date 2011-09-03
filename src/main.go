package main

import (
	"http"
	"flag"
	"os"
	"io"
	"strconv"
	"fmt"
	"path/filepath"
	"path"
	"strings"
	"mime"
	"json"
	"bytes"
	
	"encoding/base64"
)

const CONFIG_APPEND = "Config.json"

var port *int
var root *string
var workingDir string

var cert *string
var key *string

var allowIP *string

var allowUpload *bool
var uploadDir *string

var username *string
var password *string
var realm *string

var useAuth bool

func getConfigFilePath() (string, os.Error) {
	//Get Config filename
	//remove any extension from executable, then append CONFIG_APPEND const ("Config.json")
	// so StaticServ.exe OR StaticServ -> StaticServConfig.json
	
	exeAbsPath, err := filepath.Abs(os.Args[0])
	if err != nil {
		return "", err
	}
	dir, exe := filepath.Split(exeAbsPath)
	ext := filepath.Ext(exe)
	return filepath.Join(dir, exe[:len(exe) - len(ext)] + CONFIG_APPEND), nil
}

func printHelp() {
	flag.PrintDefaults()
}

type config struct {
	Port int
	AllowOnly string
	AllowUpload bool
	UploadDir string
	TlsCert string
	TlsKey string
	AuthUser string
	AuthPass string
	AuthRealm string
	MimeMap map[string]string
}
func defaultConfig() *config {
	return &config {
		Port: 9000,
		AllowOnly: "",
		AllowUpload: false,
		UploadDir: "",
		TlsCert: "",
		TlsKey: "",
		AuthUser: "",
		AuthPass: "",
		AuthRealm: "",
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
		var err = http.ListenAndServeTLS(":"+strconv.Itoa(*port), *cert, *key, server)
		if err != nil {
			fmt.Printf("Could not start http server:\n%s", err.String())
		}
	} else {
		var err = http.ListenAndServe(":"+strconv.Itoa(*port), server)
		if err != nil {
			fmt.Printf("Could not start http server:\n%s", err.String())
		}
	}
}

type fileHandler struct {
	root   string
	prefix string
}

// FileServer returns a handler that serves HTTP requests
// with the contents of the file system rooted at root.
// It strips prefix from the incoming requests before
// looking up the file name in the file system.
func FileServer(root, prefix string) http.Handler { return &fileHandler{root, prefix} }

func (f *fileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Printf("Error: %s", err)
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
	
		if len(ss) < 1 || ss[0] != *allowIP  {
			http.NotFound(w, r)
			
			fmt.Printf("DENY: %s  FOR: %s\n", r.RemoteAddr, urlPath)
			return
		}
	}
	fmt.Printf("ALLOW: %s  FOR: %s\n", r.RemoteAddr, urlPath)
	
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
	
	http.ServeFile(w, r, filepath.Join(f.root, filepath.FromSlash(urlPath)))
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
	
	upString, err := base64.StdEncoding.DecodeString(a[index + len(basic):])
	if err != nil {
		return
	}
	up := strings.SplitN(string(upString), ":", 2)
	if(len(up) != 2) {
		return
	}
	
	if(*username != up[0] || *password != up[1]) {
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
	filename := itemHead[fileIndex + len(lookfor):]
	filename = filename[:strings.Index(filename, "\"")]
	
	slashIndex := strings.LastIndex(filename, "\\")
	if slashIndex > 0 {
		filename = filename[slashIndex + 1:]
	}
	slashIndex = strings.LastIndex(filename, "/")
	if slashIndex > 0 {
		filename = filename[slashIndex + 1:]
	}
	_, saveToFilename := filepath.Split(filename)
	//END: work around IE sending full filepath
	
	//join the filename to the upload dir
	saveToFilePath := filepath.Join(*uploadDir, saveToFilename)
	
	osFile, err := os.Create(saveToFilePath)
	if err != nil {
		panic(err.String())
	}
	defer osFile.Close()
	
	count, err := io.Copy(osFile, formFile)
	if err != nil {
		panic(err.String())
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
		panic(err.String())
	}
	
	//load config file here
	config := defaultConfig()
	
	configFile, err := os.Open(defaultConfigFilePath)
	if err == nil {
		decode := json.NewDecoder(configFile)
		decode.Decode(config)
	}
	
	for ext, mimeType := range(config.MimeMap) {
		err := mime.AddExtensionType(ext, mimeType)
		if err != nil {
			panic(err.String())
		}
	}
	
	if workingDir, err = os.Getwd(); err != nil {
		panic(err.String())
	}
	
	port = flag.Int("port", config.Port, "HTTP port number")
	root = flag.String("root", workingDir, "Doc Root")
	
	
	cert = flag.String("cert", config.TlsCert, "TLS cert.pem")
	key = flag.String("key", config.TlsKey, "TLS key.pem")

	allowIP = flag.String("allow", config.AllowOnly, "Only allow address to connect")
	
	allowUpload = flag.Bool("up", config.AllowUpload, "Allow uploads to /$choose, /$up")
	uploadDir = flag.String("upTo", config.UploadDir, "Upload files to directory")
	
	username = flag.String("username", config.AuthUser, "Basic Auth Username")
	password = flag.String("password", config.AuthPass, "Basic Auth Password")
	realm = flag.String("realm", config.AuthRealm, "Basic Auth Realm")

	var printConfig = flag.Bool("printConfig", false, "Prints default Config file")
	var help = flag.Bool("h", false, "Prints help")

	flag.Parse()
	
	if *printConfig {
		//print indented json config file
		jsonBytes, err := json.Marshal(defaultConfig())
		if err != nil {
			panic(err.String())
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
		panic(err.String())
	}
	*uploadDir, err = filepath.Abs(*uploadDir)
	if err != nil {
		panic(err.String())
	}
	
	useAuth = (len(*username) != 0 || len(*password) != 0)
}

