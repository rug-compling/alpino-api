package main

import (
	"github.com/BurntSushi/toml"
	"github.com/pebbe/util"

	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	VersionMajor = 0
	VersionMinor = 1
)

type Config struct {
	Logfile         string
	About           string
	Port            int
	Tmp             string
	Interval        int
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

type Request struct {
	Request   string
	Id        string // output, cancel
	Lines     bool   // parse, tokenize
	Tokens    bool   // parse
	Label     string // parse, tokenize
	Timeout   int    // parse
	Parser    string // parse
	Maxtokens int    // parse
}

type Task struct {
	line      string
	label     string
	lineno    uint64
	maxtokens int
	job       *Job
}

type Job struct {
	id        int64
	mu        sync.Mutex
	expires   time.Time
	count     uint64
	cancelled chan bool
	err       error
	code      int
	server    string
}

var (
	cfg Config

	jobsMu sync.Mutex
	jobs   = make(map[int64]*Job)
	queue  = make(chan Task)

	verbose = flag.Bool("v", false, "verbose")
	started = time.Now()
	chLog   = make(chan string)
	//wg           sync.WaitGroup
	wgLogger     sync.WaitGroup
	chGlobalExit = make(chan bool)
	chLoggerExit = make(chan bool)

	servers = make(map[int]map[string]string) // timeout > parser > server
	parsers = make([]string, 0)

	status = map[int]string{
		200: "OK",
		202: "Accepted",
		400: "Bad Request",
		403: "Forbidden",
		429: "Too Many Requests",
		500: "Internal Server Error",
		501: "Not Implemented",
		503: "Service Unavailable",
	}
)

func main() {

	flag.Parse()

	md, err := toml.DecodeFile(flag.Arg(0), &cfg)
	util.CheckErr(err)
	if un := md.Undecoded(); len(un) > 0 {
		fmt.Fprintf(os.Stderr, "Fout in %s: onbekend: %#v", flag.Arg(0), un)
		return
	}

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
	util.CheckErr(os.Mkdir(cfg.Tmp, 0700))

	rand.Seed(time.Now().Unix())

	go func() {
		wgLogger.Add(1)
		logger()
		wgLogger.Done()
	}()

	go func() {
		chSignal := make(chan os.Signal, 1)
		signal.Notify(chSignal, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
		sig := <-chSignal
		chLog <- fmt.Sprintf("Signal: %v", sig)

		close(chGlobalExit)
		//wg.Wait()

		chLog <- fmt.Sprintf("Uptime: %v", time.Now().Sub(started))
		close(chLoggerExit)
		wgLogger.Wait()

		os.Exit(0)
	}()

	for i := 0; i < cfg.Workers; i++ {
		//wg.Add(1)
		go func() {
			worker()
			//wg.Done()
		}()
	}

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

func noHandler(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	chLog <- "Not found: " + r.URL.Path
	http.NotFound(w, r)
}

func upHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/up" {
		noHandler(w, r)
		return
	}
	logRequest(r)

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Add("Pragma", "no-cache")
	w.Write([]byte("up\n"))
}

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
	switch request.Request {
	case "parse":
		reqParse(w, request, dec.Buffered(), r.Body)
	case "tokenize":
		reqTokenize(w, request, dec.Buffered(), r.Body)
	case "output":
		// alleen jobs van type "parse"
		reqOutput(w, request)
	case "cancel":
		reqCancel(w, request)
	case "info":
		reqInfo(w)
	default:
		x(w, fmt.Errorf("Invalid request: %s", request.Request), 400)
	}
}

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
	var tokerr1, tokerr2, tokerr3 error

	if req.Lines && req.Tokens {

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
		if req.Lines {
			cmd = exec.Command("/bin/sh", "-c", "$ALPINO_HOME/Tokenization/tokenize_no_breaks.sh")
		} else {
			if req.Label == "" {
				req.Label = "doc"
			}
			cmd = exec.Command("/bin/sh", "-c", "$ALPINO_HOME/Tokenization/partok -t '"+shellEscape(req.Label)+".p.%p.s.%l|'")
		}

		// setup van stdin en stdout voor de shell
		if !req.Lines {
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

			// shell tokenize_no_breaks.sh input: labels van zinnen afsplitsen
			fpin, err := cmd.StdinPipe()
			if err != nil {
				return 0, err
			}
			go func() {
				defer fpin.Close()
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
					if strings.HasPrefix(line, "%") {
						// volgens specificatie van Alpino is dit een commentaarregel
						continue
					}
					a := strings.SplitN(line, "|", 2)
					if len(a) == 2 {
						fmt.Fprintln(fpin, "##LABEL##", hex.EncodeToString([]byte(a[0])))
						fmt.Fprintln(fpin, a[1])
					} else {
						fmt.Fprintln(fpin, line)
					}
				}
			}()

			// shell tokenize_no_breaks.sh output: labels en zinnen aan elkaar plakken
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
					if strings.HasPrefix(line, "##LABEL##") {
						b, err := hex.DecodeString(strings.TrimSpace(line[9:]))
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
			return 0, err
		}
		line = strings.TrimSpace(line)
		var lbl string
		a := strings.SplitN(line, "|", 2)
		if len(a) == 2 {
			lbl = strings.TrimSpace(a[0])
			line = strings.TrimSpace(a[1])
		}
		if line == "" {
			continue
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

	//
	// klaar: return aantal regels van de uitvoer
	//

	return lineno, nil
}

func reqTokenize(w http.ResponseWriter, req Request, rds ...io.Reader) {

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
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Add("Pragma", "no-cache")
				x(w, err, 500)
			}
			break
		}
		started = true
		a := strings.SplitN(line, "\t", 2)
		if a[0] != "" {
			fmt.Fprintln(w, a[0]+"|"+a[1])
		} else {
			if strings.Contains(a[1], "|") {
				fmt.Fprint(w, "|") // hierdoor verliezen andere '|'-tekens hun speciale betekenis
			}
			fmt.Fprintln(w, a[1])
		}
	}

	<-chWait
	if tokerr != nil {
		if started {
			fmt.Fprintf(w, "<<<ERROR>>> %v", tokerr)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Add("Pragma", "no-cache")
			x(w, tokerr, 500)
		}
	}
}

func reqParse(w http.ResponseWriter, req Request, rds ...io.Reader) {
	if req.Label == "" {
		req.Label = "doc"
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
	// voorwaarde: alle timeouts zijn voor alle servers beschikbaar
	server, ok := servers[timeout][req.Parser]
	if !ok {
		x(w, fmt.Errorf("Unknown parser %q", req.Parser), 501)
		return
	}

	jobID := rand.Int63()
	for jobID < 1 {
		jobID = rand.Int63()
	}

	dir := filepath.Join(cfg.Tmp, fmt.Sprint(jobID))
	if x(w, os.Mkdir(dir, 0700), 500) {
		return
	}

	fp, err := os.Create(filepath.Join(dir, "00"))
	if x(w, err, 500) {
		os.RemoveAll(dir)
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
	if req.Maxtokens > 0 && cfg.Max_tokens > 0 {
		maxtokens = min(req.Maxtokens, cfg.Max_tokens)
	} else {
		maxtokens = max(req.Maxtokens, cfg.Max_tokens)
	}
	go doJob(jobID, lineno, server, maxtokens)

	fmt.Fprintf(w, `{
    "code": 202,
    "status": %q,
    "id": "%d",
    "interval": %d,
    "lines": %d,
    "timeout": %d
}
`, status[202], jobID, cfg.Interval, lineno, timeout)
}

func doJob(jobID int64, nlines uint64, server string, maxtokens int) {
	chLog <- fmt.Sprintf("New job %d, %d lines", jobID, nlines)

	j := Job{
		id:        jobID,
		expires:   time.Now().Add(2 * time.Duration(cfg.Interval) * time.Second),
		count:     nlines,
		cancelled: make(chan bool),
		server:    server,
	}
	jobsMu.Lock()
	jobs[jobID] = &j
	jobsMu.Unlock()

	dir := filepath.Join(cfg.Tmp, fmt.Sprint(jobID))
	filename := filepath.Join(dir, "00")

	fp, err := os.Open(filename)
	if err != nil {
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
				j.mu.Lock()
				j.err = err
				j.code = 500
				cancel(&j)
				j.mu.Unlock()
				break
			}
			a := strings.SplitN(line, "\t", 2)
			lineno++
			queue <- Task{
				line:      a[1],
				label:     a[0],
				lineno:    lineno,
				job:       &j,
				maxtokens: maxtokens,
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
				fmt.Fprintf(w, `{"status":"internal","log":%q}`, err.Error())
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

	job.expires = time.Now().Add(2 * time.Duration(cfg.Interval) * time.Second)

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
    "api": {
        "major": %d,
        "minor": %d
    },
    "server": {
        "about": %q,
        "workers": %d,
        "jobs": %d,
        "timeout_default": %d,
        "timeout_max": %d,
        "timeout_values": [`,
		VersionMajor,
		VersionMinor,
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
	fmt.Fprintf(w, ` ]
    },
    "limits": {
        "jobs": %d,
        "tokens": %d
    }
}
`,
		cfg.Max_jobs, cfg.Max_tokens)
}

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
			cancel(job)
			job.mu.Unlock()
			continue WORKER
		}
		job.mu.Unlock()

		if task.maxtokens > 0 {
			if n := len(strings.Fields(task.line)); n > task.maxtokens {
				job.mu.Lock()
				fp, err := os.Create(filepath.Join(cfg.Tmp, fmt.Sprint(task.job.id), fmt.Sprintf("%08d", task.lineno)))
				if err == nil {
					fmt.Fprintf(fp, `{"status":"skipped","lineno":%d,`, task.lineno)
					if task.label != "" {
						fmt.Fprintf(fp, `"label":%q,`, task.label)
					}
					fmt.Fprintf(fp, `"sentence":%q,"log":"line too long: %d tokens"}`, task.line, n)
					err = fp.Close()
				}
				job.count--
				job.mu.Unlock()
				continue
			}
		}

		var log, xml string
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

		select {
		case <-job.cancelled:
		default:
			job.mu.Lock()
			fp, err := os.Create(filepath.Join(cfg.Tmp, fmt.Sprint(task.job.id), fmt.Sprintf("%08d", task.lineno)))
			if err == nil {
				fmt.Fprintf(fp, `{"status":%q,"lineno":%d,`, status, task.lineno)
				if task.label != "" {
					fmt.Fprintf(fp, `"label":%q,`, task.label)
				}
				fmt.Fprintf(fp, `"sentence":%q,`, task.line)
				if xml != "" {
					fmt.Fprintf(fp, `"xml":%q,`, xml)
				}
				fmt.Fprintf(fp, `"log":%q}`, log)
				err = fp.Close()
			}
			job.count--
			job.mu.Unlock()
		}
	}
}

// aanname: job is gelockt
func cancel(job *Job) {
	select {
	case <-job.cancelled:
	default:
		close(job.cancelled)
	}
}

func x(w http.ResponseWriter, err error, code int) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	var line string
	if _, filename, lineno, ok := runtime.Caller(1); ok {
		line = fmt.Sprintf("%v:%v", filepath.Base(filename), lineno)
		msg = line + ": " + msg
	}
	fmt.Fprintf(w, `{
    "code": %d,
    "status": %q,
    "message": %q
}
`, code, status[code], msg)
	chLog <- fmt.Sprintf("%d %s: %s -- %v", code, status[code], line, err)
	return true
}

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

func logRequest(r *http.Request, a ...interface{}) {
	chLog <- fmt.Sprintf("[%s] %s %s %s %v", r.Header.Get("X-Forwarded-For"), r.RemoteAddr, r.Method, r.URL, a)
}

func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}

func shellEscape(s string) string {
	return strings.Replace(s, `'`, `'\''`, -1)
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
