/*

TODO:

 * max_jobs implementeren

Meer TODOs beneden

*/

package main

import (
	"github.com/BurntSushi/toml"
	"github.com/pebbe/util"
	"github.com/rug-compling/alud/v2"

	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	VersionMajor = 0
	VersionMinor = 93
)

//. Types voor configuratie van de server ......................

type Config struct {
	Logfile         string
	About           string
	Port            int
	Tmp             string
	Interval        int
	Interval_system int
	Workers         int
	Max_jobs        int
	Max_tokens      int
	Timeout_default int
	Timeout_max     int
	Timeout_values  []int
	Alpino          []AlpinoT
}

type AlpinoT struct {
	Timeout int
	Parser  string
	Server  string
}

//. Type voor API-request ......................................

type Request struct {
	Request    string
	Id         string // output, cancel
	Data_type  string // parse, tokenize
	Timeout    int    // parse
	Parser     string // parse
	Max_tokens int    // parse
	Ud         *bool  // parse
	lines      bool   // parse, tokenize
	tokens     bool   // parse
	escape     string // parse
	label      string // parse, tokenize
}

//. Types voor API-request=parse ...............................

// Het hele corpus.
// Aangemaakt en uiteindelijk weer verwijderd in de functie `doJob`.
type Job struct {
	id        int64
	mu        sync.Mutex
	expires   time.Time
	count     uint64    // Aantal zinnen dat nog geparst moet worden.
	cancelled chan bool // Wordt gesloten als verwerking job moet stoppen.
	err       error
	code      int
	maxtokens int
	server    string // Door welke Alpino-server de zinnen geparst moeten worden.
	ud        bool   // Universal Dependencies maken?
}

// Een enkele zin uit het corpus.
// Aangemaakt in de functie `doJob`, en via channel `queue` naar
// een van de workers gestuurd.
type Task struct {
	line   string
	label  string
	lineno uint64
	meta   []string
	job    *Job // Bij welke Job deze Task hoort
	ud     bool // Universal Dependencies maken?
}

//. Globale variabelen .........................................

var (
	isTrue = map[string]bool{
		"true": true,
		"yes":  true,
		"ja":   true,
		"1":    true,
		"t":    true,
		"y":    true,
		"j":    true,
	}

	// Als true, echo log naar stdout.
	verbose = flag.Bool("v", false, "verbose")

	cfg Config

	// Alle jobs.
	jobsMu sync.Mutex
	jobs   = make(map[int64]*Job) // id : job

	// Voor het sturen van een zin uit een job naar een worker.
	queue = make(chan Task)

	chLog        = make(chan string)
	wgLogger     sync.WaitGroup
	chGlobalExit = make(chan bool)
	chLoggerExit = make(chan bool)
	//wg           sync.WaitGroup

	servers = make(map[int]map[string]string) // timeout : parser : server
	parsers = make([]string, 0)               // lijst van niet-standaard parsers

	// Standaard http-codes.
	status = map[int]string{
		200: "OK",
		202: "Accepted",
		400: "Bad Request",
		403: "Forbidden",
		405: "Method Not Allowed",
		429: "Too Many Requests",
		500: "Internal Server Error",
		501: "Not Implemented",
		503: "Service Unavailable",
	}

	alpino_build string

	reSentID = regexp.MustCompile(`<sentence sentid=".*?">`)

	// Hoe lang is de server in de lucht?
	timestart = time.Now()
)

//. init .......................................................

func init() {
	data, err := ioutil.ReadFile(filepath.Join(os.Getenv("ALPINO_HOME"), "version"))
	util.CheckErr(err)
	alpino_build = strings.TrimSpace(string(data))
}

//. main .......................................................

func main() {

	flag.Parse()

	// Configuratie inlezen.
	md, err := toml.DecodeFile(flag.Arg(0), &cfg)
	util.CheckErr(err)
	if un := md.Undecoded(); len(un) > 0 {
		fmt.Fprintf(os.Stderr, "Fout in %s: onbekend: %#v", flag.Arg(0), un)
		return
	}

	// Gegevens over beschikbare parsers verwerken.
	seen := make(map[string]bool)
	for _, a := range cfg.Alpino {
		if _, ok := servers[a.Timeout]; !ok {
			servers[a.Timeout] = make(map[string]string)
		}
		servers[a.Timeout][a.Parser] = a.Server
		if !seen[a.Parser] {
			if a.Parser != "" {
				parsers = append(parsers, a.Parser)
			}
			seen[a.Parser] = true
		}
	}

	util.CheckErr(os.RemoveAll(cfg.Tmp))
	util.CheckErr(os.MkdirAll(cfg.Tmp, 0700))

	rand.Seed(time.Now().Unix())

	go func() {
		wgLogger.Add(1)
		logger()
		wgLogger.Done()
	}()

	// Voor het schoon afsluiten.
	go func() {
		chSignal := make(chan os.Signal, 1)
		signal.Notify(chSignal, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
		sig := <-chSignal
		chLog <- fmt.Sprintf("Signal: %v", sig)
		closeAndExit(0)
	}()

	// Elke worker parst één zin tegelijk.
	for i := 0; i < cfg.Workers; i++ {
		//wg.Add(1)
		go func() {
			worker()
			//wg.Done()
		}()
	}

	// Dit cancelt (voltooide) jobs waarvan de timeout is verlopen.
	// Gecancelde jobs worden hier niet verwijderd, dat gebeurt in de functie 'doJob'.
	go func() {
		for {
			time.Sleep(time.Duration(cfg.Interval) * time.Second)

			select {
			case <-chGlobalExit:
				return
			default:
			}

			jobsMu.Lock()

			if n := len(jobs); n > 0 {
				chLog <- fmt.Sprintf("Number of jobs: %d", n)
			}

			now := time.Now()
			for _, job := range jobs {
				job.mu.Lock()
				if now.After(job.expires) {
					chLog <- fmt.Sprintf("Job %d expired", job.id)
					cancel(job)
				}
				job.mu.Unlock()
			}

			jobsMu.Unlock()
		}
	}()

	http.HandleFunc("/", noHandler)
	http.HandleFunc("/up", upHandler)
	http.HandleFunc("/json", jsonHandler)

	chLog <- fmt.Sprintf("Server beschikbaar op: http://127.0.0.1:%d/", cfg.Port)
	fmt.Println(http.ListenAndServe(fmt.Sprint(":", cfg.Port), nil))
}

//. Handlers voor http-request .................................

func noHandler(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	chLog <- "Not found: " + r.URL.Path
	http.NotFound(w, r)
}

// Om te testen of het programma reageert.
func upHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/up" {
		noHandler(w, r)
		return
	}
	logRequest(r)

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Add("Pragma", "no-cache")
	w.Write([]byte("alpiner\n"))
}

// De eigenlijk API.
func jsonHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/json" {
		noHandler(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Add("Pragma", "no-cache")

	if r.Method != "POST" {
		logRequest(r)
		x(w, fmt.Errorf("Method %s is not supported. Method POST required.", r.Method), 405)
		return
	}
	defer r.Body.Close()

	dec := json.NewDecoder(r.Body)
	var request Request
	if err := dec.Decode(&request); err != nil {
		logRequest(r)
		x(w, err, 400)
		return
	}
	logRequest(r, request.Request, request.Id)

	// Wanneer na het json-object nog data volgt in r.Body, dan heeft
	// de json-decoder daar waarschijnlijk al een deel van ingelezen.
	// Dat deel is beschikbaar in dec.Buffered(). Het nog niet ingelezen
	// deel zit in r.Body.
	switch request.Request {
	case "parse":
		reqParse(w, request, dec.Buffered(), r.Body)
	case "tokenize":
		reqTokenize(w, request, dec.Buffered(), r.Body)
	case "output":
		// alleen jobs van type "parse"
		reqOutput(w, request)
	case "cancel":
		// alleen jobs van type "parse"
		reqCancel(w, request)
	case "info":
		reqInfo(w)
	default:
		x(w, fmt.Errorf("Invalid request: %s", request.Request), 400)
	}
}

//. Handlers voor API-requests .................................

func reqParse(w http.ResponseWriter, req Request, rds ...io.Reader) {

	words := strings.Fields(req.Data_type)
	if len(words) == 0 {
		words = []string{"text"}
	}
	switch words[0] {
	case "text":
		if len(words) == 1 {
			req.label = "doc"
		} else {
			req.label = strings.Join(words[1:], " ")
			if req.label[0] == '%' || strings.Contains(req.label, "|") {
				x(w, fmt.Errorf("Label can't start with '%%' or contain '|'"), 400)
				return
			}
		}
	case "lines":
		req.lines = true
		for _, word := range words[1:] {
			switch word {
			case "tokens":
				req.tokens = true
			case "none", "half", "full":
				req.escape = word
			default:
				x(w, fmt.Errorf("Unknown option %q for data_type \"lines\"", word), 400)
				return
			}
		}
		if req.tokens {
			if req.escape == "" {
				req.escape = "half"
			}
		} else {
			if req.escape != "" {
				x(w, fmt.Errorf("Option %q for data_type \"lines\" also needs option \"tokens\"", req.escape), 400)
				return
			}
			// Gebruik tokenizer van Alpino. Die doet al full escape.
			req.escape = "none"
		}
	default:
		x(w, fmt.Errorf("Unknown data_type %q", words[0]), 400)
		return
	}

	timeout := cfg.Timeout_default
	if req.Timeout > 0 {
		timeout = cfg.Timeout_values[0]
		diff := abs(timeout - req.Timeout)
		for _, t := range cfg.Timeout_values[1:] {
			d := abs(t - req.Timeout)
			if d < diff {
				d = diff
				timeout = t
			}
		}
	}
	// Voorwaarde: alle timeouts zijn voor alle servers beschikbaar.
	server, ok := servers[timeout][req.Parser]
	if !ok {
		x(w, fmt.Errorf("Unknown parser %q", req.Parser), 400)
		return
	}

	jobID := rand.Int63()
	for jobID < 1 {
		jobID = rand.Int63()
	}

	dir := filepath.Join(cfg.Tmp, fmt.Sprint(jobID))
	if x(w, os.MkdirAll(dir, 0700), 500) {
		closeAndExit(1)
		return
	}

	fp, err := os.Create(filepath.Join(dir, "00"))
	if x(w, err, 500) {
		os.RemoveAll(dir)
		closeAndExit(1)
		return
	}
	lineno, err := tokenize(fp, req, rds...)
	fp.Close()

	if x(w, err, 500) {
		os.RemoveAll(dir)
		return
	}

	if lineno == 0 {
		os.RemoveAll(dir)
		x(w, fmt.Errorf("No data"), 400)
		return
	}

	var maxtokens int
	if req.Max_tokens > 0 && cfg.Max_tokens > 0 {
		maxtokens = min(req.Max_tokens, cfg.Max_tokens)
	} else {
		maxtokens = max(req.Max_tokens, cfg.Max_tokens)
	}
	go doJob(jobID, lineno, server, maxtokens, req.escape, req.Ud == nil || *req.Ud == true)

	w.WriteHeader(202)
	fmt.Fprintf(w, `{
    "code": 202,
    "status": %q,
    "id": "%d",
    "interval": %d,
    "number_of_lines": %d,
    "timeout": %d,
    "max_tokens": %d
}
`,
		status[202], jobID, cfg.Interval, lineno, timeout, maxtokens)
}

func reqTokenize(w http.ResponseWriter, req Request, rds ...io.Reader) {

	words := strings.Fields(req.Data_type)
	if len(words) == 0 {
		words = []string{"text"}
	}
	switch words[0] {
	case "text":
		if len(words) == 1 {
			req.label = "doc"
		} else {
			req.label = strings.Join(words[1:], " ")
			if req.label[0] == '%' || strings.Contains(req.label, "|") {
				x(w, fmt.Errorf("Label can't start with '%%' or contain '|'"), 400)
				return
			}
		}
	case "lines":
		req.lines = true
		if len(words) > 1 {
			x(w, fmt.Errorf("Too many options for data_type \"lines\""), 400)
			return
		}
	default:
		x(w, fmt.Errorf("Unknown data_type %q", words[0]), 400)
		return
	}

	pr, pw := io.Pipe()
	defer pr.Close()

	chWait := make(chan bool)

	var tokerr error
	go func() {
		_, tokerr = tokenize(pw, req, rds...)
		pw.Close()
		close(chWait)
	}()

	started := false
	reader := util.NewReader(pr)
	for {
		line, err := reader.ReadLineString()
		if err == io.EOF {
			break
		}
		if err != nil {
			if started {
				fmt.Fprintf(w, "<<<ERROR>>> %v", err)
				x(nil, err, 500)
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Add("Pragma", "no-cache")
				x(w, err, 500)
			}
			// TODO: closeAndExit(1) ?
			break
		}
		started = true
		if line == "" || line[0] == '%' {
			fmt.Fprintln(w, line)
			continue
		}
		a := strings.SplitN(line, "\t", 2)
		if a[0] != "" {
			fmt.Fprintln(w, a[0]+"|"+a[1])
		} else {
			if strings.Contains(a[1], "|") || strings.HasPrefix(a[1], "%") {
				fmt.Fprint(w, "|") // hierdoor verliezen andere '|'-tekens hun speciale betekenis
			}
			fmt.Fprintln(w, a[1])
		}
	}

	<-chWait
	if tokerr != nil {
		if started {
			fmt.Fprintf(w, "<<<ERROR>>> %v", tokerr)
			x(nil, tokerr, 500)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Add("Pragma", "no-cache")
			x(w, tokerr, 500)
		}
		// TODO: closeAndExit(1) ?
	}
}

func reqOutput(w http.ResponseWriter, req Request) {
	id, err := strconv.ParseInt(req.Id, 10, 64)
	if err != nil {
		x(w, fmt.Errorf("Invalid id: %s", req.Id), 400)
		return
	}

	jobsMu.Lock()
	job, ok := jobs[id]
	if ok {
		job.mu.Lock()
		defer job.mu.Unlock()
	}
	jobsMu.Unlock()

	if !ok {
		x(w, fmt.Errorf("Invalid id: %s", req.Id), 400)
		return

	}

	if x(w, job.err, job.code) {
		return
	}

	if time.Now().After(job.expires) {
		x(w, fmt.Errorf("Job expired"), 400)
		cancel(job)
		return
	}

	dir := filepath.Join(cfg.Tmp, fmt.Sprint(req.Id))
	files, err := ioutil.ReadDir(dir)
	if x(w, err, 500) {
		return
	}
	w.Write([]byte(`{
    "code": 200,
    "status": "OK",
    "batch": [`))
	next := false
	for _, file := range files {
		if filename := file.Name(); filename != "00" && !file.IsDir() {
			if next {
				w.Write([]byte(",\n"))
			} else {
				w.Write([]byte("\n"))
				next = true
			}
			full := filepath.Join(dir, filename)
			fp, err := os.Open(full)
			if err != nil {
				fmt.Fprintf(w, `{"line_status":"fail","log":%q}`, err.Error())
				x(nil, err, 500)
			} else {
				io.Copy(w, fp)
				fp.Close()
				os.Remove(full)
			}
		}
	}

	fmt.Fprintf(w, `
    ],
    "finished": %v
}
`, job.count == 0)

	job.expires = time.Now().Add(time.Duration(cfg.Interval_system) * time.Second)

	if job.count == 0 {
		cancel(job)
	}
}

func reqCancel(w http.ResponseWriter, req Request) {
	id, err := strconv.ParseInt(req.Id, 10, 64)
	if err != nil {
		x(w, fmt.Errorf("Invalid id: %s", req.Id), 400)
		return
	}

	jobsMu.Lock()
	job, ok := jobs[id]
	if ok {
		job.mu.Lock()
		defer job.mu.Unlock()
	}
	jobsMu.Unlock()

	if !ok {
		x(w, fmt.Errorf("Invalid id: %s", req.Id), 400)
		return

	}

	if x(w, job.err, job.code) {
		return
	}

	if time.Now().After(job.expires) {
		x(w, fmt.Errorf("Job expired"), 400)
		cancel(job)
		return
	}

	chLog <- "Job " + req.Id + " cancelled"
	cancel(job)
	w.Write([]byte(`{
    "code": 200,
    "status": "OK
}
`))
}

func reqInfo(w http.ResponseWriter) {

	jobsMu.Lock()
	njobs := len(jobs)
	jobsMu.Unlock()

	fmt.Fprintf(w, `{
    "code": 200,
    "status": "OK",
    "api_version": [ %d, %d ],
    "parser_build": %q,
    "tokenizer_build": %q,
    "ud_build": %q,
    "about": %q,
    "workers": %d,
    "total_running_jobs": %d,
    "timeout_default": %d,
    "timeout_max": %d,
    "timeout_values": [`,
		VersionMajor,
		VersionMinor,
		alpino_build,
		alpino_build,
		alud.VersionID(),
		cfg.About,
		cfg.Workers,
		njobs,
		cfg.Timeout_default,
		cfg.Timeout_max)
	p := ""
	for _, t := range cfg.Timeout_values {
		fmt.Fprintf(w, "%s %d", p, t)
		p = ","
	}
	fmt.Fprintf(w, ` ],
    "parsers": [`)
	p = ""
	for _, t := range parsers {
		fmt.Fprintf(w, "%s %q", p, t)
		p = ","
	}
	fmt.Fprintf(w, ` ],
    "max_jobs": %d,
    "max_tokens": %d,
    "extra_types": [ ]
}
`,
		cfg.Max_jobs, cfg.Max_tokens)
}

// einde API-requests

//. Hulpfunctie voor `reqParse` en `reqTokenize` ...............

func tokenize(writer io.Writer, req Request, readers ...io.Reader) (uint64, error) {

	//
	// flow: readers -> [plakken] -> source -> [tokeniseren] -> tokenizer -> [nabewerking] -> writer
	//

	// dit plakt de inhoud van 'readers' aan elkaar en stuurt het naar 'source'
	source, pw := io.Pipe()
	defer source.Close()
	go func(writer io.WriteCloser) {
		for _, reader := range readers {
			io.Copy(writer, reader)
		}
		writer.Close()
	}(pw)

	//
	// leest van 'source', tokeniseert, en koppelt resultaat aan 'tokenizer'
	//

	var tokenizer io.ReadCloser

	var chErrWait chan bool
	var cmd *exec.Cmd
	var tokerr1, tokerr2, tokerr3, tokerr4 error
	var raw bool

	if req.lines && req.tokens {

		//
		// tekst is al getokeniseerd
		//

		tokenizer = source // direct van 'source' naar 'tokenizer'

	} else {

		// lines: false OR tokens: false

		//
		// er moet getokeniseerd worden
		//

		// kies externe tokenizer voor de shell
		if req.lines {
			cmd = exec.Command("/bin/sh", "-c", "$ALPINO_HOME/Tokenization/tokenize_no_breaks.sh")
		} else {
			cmd = exec.Command("/bin/sh", "-c", "$ALPINO_HOME/Tokenization/partok -d '"+shellEscape(req.label)+"'")
		}

		// setup van stdin en stdout voor de shell
		if !req.lines {
			// lines: false

			//
			// doorlopende tekst -> partok, zorgt zelf voor labels
			//

			// shell partok input
			cmd.Stdin = source

			// shell partok output
			var err error
			tokenizer, err = cmd.StdoutPipe()
			if err != nil {
				return 0, err
			}
			defer tokenizer.Close()
		} else { // if !req.Lines
			// lines: true AND tokens: false

			//
			// een zin per regel -> tokenize_no_breaks.sh, kan niet met labels omgaan
			//

			// shell tokenize_no_breaks.sh input
			//   * commentaren en lege regels coderen
			//   * labels van zinnen afsplitsen
			raw = true
			fpin, err := cmd.StdinPipe()
			if err != nil {
				return 0, err
			}
			go func() {
				defer fpin.Close()
				firstline := true
				reader := util.NewReader(source)
				for {
					line, err := reader.ReadLineString()
					if err == io.EOF {
						break
					}
					if err != nil {
						chLog <- fmt.Sprintf("tokenize: %v", err)
						tokerr1 = err
						break
					}

					leeg := strings.TrimSpace(line) == ""

					// als eerste regel leeg is die overslaan
					if firstline && leeg {
						firstline = false
						continue
					}
					firstline = false

					// lege regels, commentaren en metadata niet tokeniseren
					if leeg || line[0] == '%' || strings.HasPrefix(strings.ToLower(line), "##meta") {
						fmt.Fprintln(fpin, "%%RAW%%", hex.EncodeToString([]byte(line)))
						continue
					}

					a := strings.SplitN(line, "|", 2)
					if len(a) == 2 {
						lbl := strings.TrimSpace(a[0])
						lne := strings.TrimSpace(a[1])
						if lne == "" {
							// TODO: geen zin: wat te doen?
							fmt.Fprintln(fpin, "%%RAW%%", hex.EncodeToString([]byte("%%ERROR-NO-TEXT%% "+line)))
							continue
						}
						fmt.Fprintln(fpin, "%%LBL%%", hex.EncodeToString([]byte(lbl)))
						fmt.Fprintln(fpin, lne)
					} else {
						fmt.Fprintln(fpin, line)
					}
				}
			}()

			// shell tokenize_no_breaks.sh output
			//   * lege regels en commentaren decoderen
			//   * labels en zinnen aan elkaar plakken
			var writer io.WriteCloser
			tokenizer, writer = io.Pipe()
			defer tokenizer.Close()
			pipe, err := cmd.StdoutPipe()
			if err != nil {
				return 0, err
			}
			go func() {
				defer pipe.Close()
				defer writer.Close()
				var lbl string
				reader := util.NewReader(pipe)
				for {
					line, err := reader.ReadLineString()
					if err == io.EOF {
						break
					}
					if err != nil {
						chLog <- fmt.Sprintf("tokenize: %v", err)
						tokerr2 = err
						break
					}
					if strings.HasPrefix(line, "%%RAW%%") {
						b, err := hex.DecodeString(strings.TrimSpace(line[7:]))
						if err != nil {
							chLog <- fmt.Sprintf("tokenize: %v", err)
							tokerr2 = err
							break
						}
						fmt.Fprintln(writer, "%%RAW%%", string(b))
					} else if strings.HasPrefix(line, "%%LBL%%") {
						b, err := hex.DecodeString(strings.TrimSpace(line[7:]))
						if err != nil {
							chLog <- fmt.Sprintf("tokenize: %v", err)
							tokerr2 = err
							break
						}
						lbl = string(b)
					} else {
						fmt.Fprintln(writer, lbl+"|"+line)
						lbl = ""
					}
				}
			}()
		} // if !req.Lines else
		// klaar met setup van stdin en stdout voor de shell

		// setup stderr voor de shell
		pipe, err := cmd.StderrPipe()
		if err != nil {
			return 0, err
		}
		// channel dat gesloten wordt als fouten van de shell verwerkt zijn
		chErrWait = make(chan bool)
		go func() {
			defer close(chErrWait)
			defer pipe.Close()
			errlines := make([]string, 0)
			reader := util.NewReader(pipe)
			for {
				line, err := reader.ReadLineString()
				if err == io.EOF {
					break
				}
				if err != nil {
					errlines = append(errlines, err.Error())
					chLog <- fmt.Sprintf("tokenize: %v", err)
					break
				}
				errlines = append(errlines, line)
				chLog <- fmt.Sprintf("tokenize: %v", line)
			}
			if len(errlines) > 0 {
				tokerr3 = fmt.Errorf("tokenize: " + strings.Join(errlines, " -- "))
			}
		}()

		// start de shell
		err = cmd.Start()
		if err != nil {
			return 0, err
		}
	} // if req.Lines && req.Tokens else

	//
	// lees van 'tokenizer', zet regels in juiste vorm, en schrijf naar 'writer'
	//

	var lineno uint64
	reader := util.NewReader(tokenizer)
	for {
		line, err := reader.ReadLineString()
		if err == io.EOF {
			break
		}
		if err != nil {
			tokerr4 = err
			break
		}

		// lege regels, commentaren en metadata ongewijzigd naar uitvoer
		if raw && strings.HasPrefix(line, "%%RAW%%") {
			fmt.Fprintln(writer, strings.TrimSpace(line[7:]))
			continue
		}
		if line == "" || line[0] == '%' || strings.HasPrefix(strings.ToLower(line), "##meta") {
			fmt.Fprintln(writer, line)
			continue
		}

		var lbl string
		a := strings.SplitN(line, "|", 2)
		if len(a) == 2 {
			lbl = strings.TrimSpace(a[0])
			line = strings.TrimSpace(a[1])
		}
		lineno++
		fmt.Fprintf(writer, "%s\t%s\n", lbl, line)
	}

	//
	// fouten van shell en goroutines afhandelen
	//

	if cmd != nil {
		err := cmd.Wait()
		if chErrWait != nil {
			<-chErrWait
		}
		if tokerr1 != nil {
			return 0, tokerr1
		}
		if tokerr2 != nil {
			return 0, tokerr2
		}
		if tokerr3 != nil {
			return 0, tokerr3
		}
		if err != nil {
			return 0, err
		}
	}

	if tokerr4 != nil {
		return 0, tokerr4
	}

	//
	// klaar: return aantal regels van de uitvoer
	//

	return lineno, nil
}

//. Hulpfunctie voor request `reqParse` ........................

// Wanneer de functie 'reqParse' de data heeft ingelezen, eventueel
// getokeniseerd, en opgeslagen, dan start het een goroutine met
// deze functie voor verdere verwerking.

func doJob(jobID int64, nlines uint64, server string, maxtokens int, escape string, ud bool) {
	chLog <- fmt.Sprintf("New job %d, %d lines", jobID, nlines)

	j := Job{
		id:        jobID,
		expires:   time.Now().Add(time.Duration(cfg.Interval_system) * time.Second),
		count:     nlines,
		cancelled: make(chan bool),
		server:    server,
		maxtokens: maxtokens,
		ud:        ud,
	}
	jobsMu.Lock()
	jobs[jobID] = &j
	jobsMu.Unlock()

	dir := filepath.Join(cfg.Tmp, fmt.Sprint(jobID))
	filename := filepath.Join(dir, "00")

	inMeta := false
	seenMeta := make(map[string]bool)
	metaLines := make([]string, 0)

	fp, err := os.Open(filename)
	if err != nil {
		x(nil, err, 500)
		j.mu.Lock()
		j.err = err
		j.code = 500
		cancel(&j)
		j.mu.Unlock()
	} else {
		reader := util.NewReader(fp)
		var lineno uint64
	READER:
		for {
			line, err := reader.ReadLineString()
			if err == io.EOF {
				break
			}
			if err != nil {
				x(nil, err, 500)
				j.mu.Lock()
				j.err = err
				j.code = 500
				cancel(&j)
				j.mu.Unlock()
				break
			}
			if line == "" || line[0] == '%' {
				continue
			}

			if strings.HasPrefix(strings.ToLower(line), "##meta") {
				if !inMeta {
					inMeta = true
					seenMeta = make(map[string]bool)
				}
				var typ, name, value string
				aa := strings.SplitN(line, "=", 2)
				if len(aa) != 2 {
					continue
				}
				value = strings.TrimSpace(aa[1])

				aa = strings.Fields(aa[0])
				if len(aa) < 3 {
					continue
				}
				typ = aa[1]
				name = strings.Join(aa[2:], " ")

				// oude waardes verwijderen, maar niet als `name` al gezien is in dit blok
				if !seenMeta[name] {
					seenMeta[name] = true
					for i := 0; i < len(metaLines); i++ {
						aa := strings.Split(metaLines[i], "\t")
						if aa[1] == name {
							metaLines = append(metaLines[:i], metaLines[i+1:]...)
							i--
						}
					}
				}

				// nieuwe waarde toevoegen
				if value != "" {
					if typ == "bool" {
						if isTrue[strings.ToLower(value)] {
							value = "true"
						} else {
							value = "false"
						}
					}
					metaLines = append(metaLines, typ+"\t"+name+"\t"+value)
				}

				continue
			}
			inMeta = false

			a := strings.SplitN(line, "\t", 2)
			if escape != "none" {
				words := strings.Fields(a[1])
				for i, word := range words {
					switch word {
					case `[`:
						words[i] = `\[`
					case `]`:
						words[i] = `\]`
					case `\[`:
						if escape == "full" {
							words[i] = `\\[`
						}
					case `\]`:
						if escape == "full" {
							words[i] = `\\]`
						}
					}
				}
				a[1] = strings.Join(words, " ")
			}
			lineno++
			ml := make([]string, len(metaLines))
			for i, s := range metaLines {
				ml[i] = s
			}
			queue <- Task{
				line:   a[1],
				label:  a[0],
				lineno: lineno,
				meta:   ml,
				job:    &j,
			}

			select {
			case <-j.cancelled:
				break READER
			default:
			}

		}
		fp.Close()
	}

	os.Remove(filename)

	<-j.cancelled

	j.mu.Lock()
	os.RemoveAll(dir)
	j.mu.Unlock()

	jobsMu.Lock()
	delete(jobs, jobID)
	jobsMu.Unlock()

	chLog <- fmt.Sprintf("Job %d finished", jobID)
}

//. worker .....................................................

// Een worker zorgt voor het parsen van de zin, één tegelijk.
// Zinnen worden ingelzen vanuit channel `queue`.

func worker() {

WORKER:
	for {
		task := <-queue

		job := task.job

		select {
		case <-chGlobalExit:
			return
		case <-job.cancelled:
			continue WORKER
		default:
		}

		job.mu.Lock()
		exp := job.expires
		if time.Now().After(exp) {
			chLog <- fmt.Sprintf("Running job %d expired", job.id)
			cancel(job)
			job.mu.Unlock()
			continue WORKER
		}
		maxtokens := job.maxtokens
		job.mu.Unlock()

		if maxtokens > 0 {
			if n := len(strings.Fields(task.line)); n > maxtokens {
				job.mu.Lock()
				fp, err := os.Create(filepath.Join(cfg.Tmp, fmt.Sprint(task.job.id), fmt.Sprintf("%08d", task.lineno)))
				if err == nil {
					fmt.Fprintf(fp, `{"line_status":"skipped","line_number":%d,`, task.lineno)
					if task.label != "" {
						fmt.Fprintf(fp, `"label":%q,`, task.label)
					}
					fmt.Fprintf(fp, `"sentence":%q,"log":"line too long: %d tokens"}`, task.line, n)
					err = fp.Close()
				}
				x(nil, err, 500)
				job.count--
				job.mu.Unlock()
				continue
			}
		}

		var log, xml, udlog string
		status := "ok"
		var conn net.Conn
		var err error
		for i := 0; i < 10; i++ {
			conn, err = net.Dial("tcp", job.server)
			if err == nil {
				break
			}
			time.Sleep(2 * time.Second)
		}
		if err == nil {
			_, err = conn.Write([]byte(task.line + "\n"))
			if err == nil {
				var b []byte
				b, err = ioutil.ReadAll(conn)
				if err == nil {
					xml = string(b)
				}
			}
			conn.Close()
		}

		if err != nil {
			x(nil, err, 500)
			job.err = err
			job.code = 500
			cancel(job)
			continue WORKER
		}

		if x := strings.TrimSpace(xml); !strings.HasSuffix(x, "</alpino_ds>") {
			status = "fail"
			log = xml
			xml = ""
		}

		var sentid string
		if task.label == "" {
			sentid = fmt.Sprint(task.lineno)
		} else {
			sentid = task.label
		}

		if job.ud && xml != "" {
			xml2, err := alud.UdAlpino([]byte(xml), "nofilename.xml", sentid)
			if xml2 != "" {
				xml = xml2
			}
			if err != nil {
				udlog = err.Error()
			}
		}

		select {
		case <-job.cancelled:
		default:
			job.mu.Lock()
			fp, err := os.Create(filepath.Join(cfg.Tmp, fmt.Sprint(task.job.id), fmt.Sprintf("%08d", task.lineno)))
			if err == nil {

				// invoegen van metadata
				if len(task.meta) > 0 {
					var buf bytes.Buffer
					fmt.Fprint(&buf, "<metadata>\n")
					for _, s := range task.meta {
						aa := strings.Split(s, "\t")
						fmt.Fprintf(&buf, "    <meta type=%q name=%q value=%q/>\n",
							html.EscapeString(aa[0]),
							html.EscapeString(aa[1]),
							html.EscapeString(aa[2]))
					}
					fmt.Fprint(&buf, "  </metadata>\n  ")
					i := strings.Index(xml, "<parser ")
					if i < 0 {
						i = strings.Index(xml, "<node ")
					}
					if i > 0 {
						xml = xml[:i] + buf.String() + xml[i:]
					}
				}

				// correctie van sentid
				xml = reSentID.ReplaceAllString(xml, `<sentence sentid="`+html.EscapeString(sentid)+`">`)

				fmt.Fprintf(fp, `{"line_status":%q,"line_number":%d,`, status, task.lineno)
				if task.label != "" {
					fmt.Fprintf(fp, `"label":%q,`, task.label)
				}
				fmt.Fprintf(fp, `"sentence":%q,`, task.line)
				if xml != "" {
					fmt.Fprintf(fp, `"alpino_ds":%q,`, xml)
				}
				fmt.Fprintf(fp, `"log":%q`, log)
				if udlog != "" {
					fmt.Fprintf(fp, `,"ud_log":%q`, udlog)
				}
				fmt.Fprint(fp, "}")
				err = fp.Close()
			}
			x(nil, err, 500)
			job.count--
			job.mu.Unlock()
		}
	}
}

//. Overige hulpfuncties .......................................

func closeAndExit(errcode int) {
	close(chGlobalExit) // signaal dat alle verwerking moet stoppen
	//wg.Wait()

	chLog <- fmt.Sprintf("Uptime: %v", time.Since(timestart))

	time.Sleep(time.Second)
	close(chLoggerExit) // signaal dat de logger moet stoppen
	wgLogger.Wait()     // wacht tot de logger is gestopt

	os.RemoveAll(cfg.Tmp)

	os.Exit(errcode)
}

// Cancel een job.
// Aanname: job is gelockt.
func cancel(job *Job) {
	select {
	case <-job.cancelled:
	default:
		close(job.cancelled)
	}
}

// http-response bij een fout.
func x(w http.ResponseWriter, err error, code int) bool {
	if err == nil {
		return false
	}
	if w != nil {
		w.WriteHeader(code)
	}
	msg := err.Error()
	var line string
	if _, filename, lineno, ok := runtime.Caller(1); ok {
		line = fmt.Sprintf("%v:%v", filepath.Base(filename), lineno)
		msg = line + ": " + msg
	}
	if w != nil {
		fmt.Fprintf(w, `{
    "code": %d,
    "status": %q,
    "message": %q
}
`, code, status[code], msg)
	}

	chLog <- fmt.Sprintf("%d %s: %s -- %v", code, status[code], line, err)

	return true
}

// De logger leest meldingen uit channel `chLog`.
// Uitvoer naar logbestand (automatisch geroteerd als het te groot wordt).
// Uitvoer naar os.Stdout, indien verbose=true.
func logger() {

	logfile := cfg.Logfile

	rotate := func() {
		for i := 4; i > 1; i-- {
			os.Rename(
				fmt.Sprintf("%s%d", logfile, i-1),
				fmt.Sprintf("%s%d", logfile, i))
		}
		os.Rename(logfile, logfile+"1")
	}

	rotate()
	fp, err := os.Create(logfile)
	util.CheckErr(err)

	n := 0
	for {
		select {
		case msg := <-chLog:
			now := time.Now()
			s := fmt.Sprintf("%04d-%02d-%02d %d:%02d:%02d %s", now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), msg)
			fmt.Fprintln(fp, s)
			fp.Sync()
			if *verbose {
				fmt.Println(s)
			}
			n++
			if n == 10000 {
				fp.Close()
				rotate()
				fp, _ = os.Create(logfile)
				n = 0
			}
		case <-chLoggerExit:
			fp.Close()
			return
		}
	}
}

// Stuur gegevens over request naar de logger.
func logRequest(r *http.Request, a ...interface{}) {
	chLog <- fmt.Sprintf("[%s] %s %s %s %v", r.Header.Get("X-Forwarded-For"), r.RemoteAddr, r.Method, r.URL, a)
}

// `s` wordt gebruikt tussen enkele quotes, dus als `s` zelf enkele quotes bevat
// moeten die vervangen worden: ' -> '\''
func shellEscape(s string) string {
	return strings.Replace(s, `'`, `'\''`, -1)
}

func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}

func max(a ...int) int {
	b := a[0]
	for _, i := range a[1:] {
		if i > b {
			b = i
		}
	}
	return b
}

func min(a ...int) int {
	b := a[0]
	for _, i := range a[1:] {
		if i < b {
			b = i
		}
	}
	return b
}
