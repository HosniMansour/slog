package slog

import (
	"github.com/op/go-logging"
	"log"
	"os"
	"strings"
	"github.com/getsentry/raven-go"
	"bytes"
	"errors"
	"runtime"
	"path"
	"unsafe"
	"time"
	"fmt"
	"github.com/kardianos/osext"
	"syscall"
	"io"
	"github.com/maruel/panicparse/stack"
	"io/ioutil"
	"os/signal"
	"reflect"
	"github.com/erikdubbelboer/gspt"
	"github.com/sirupsen/logrus"
	"github.com/evalphobia/logrus_sentry"
	flag "github.com/spf13/pflag"
)

func ForceException() {
	i := 0
	i = 1 / i
}

// go program may neither avoid crash from this, nor manage it -
// the panic mandatory goes to stderr + os.exit(2)
// see https://github.com/golang/go/issues/20161
func UncontrolledCrash() {
	go func() {
		ForceException()
	}()
}

type SentryBackend struct {
	// we use DefaultClient and global raven.SetDSN()
	//Client *raven.Client
}

func SendToSentry(s string, tags map[string]string, isWarning bool) {
	// Capture vs Capture*AndWait
	// we prefer wait to safely submit errors
	if isWarning {
		// without stacktrace
		raven.CaptureMessageAndWait(s, tags)
	} else {
		raven.CaptureErrorAndWait(errors.New(s), tags)
	}
}

type LoggingRecord struct {
	ID     uint64
	Time   time.Time
	Module string
	Level  logging.Level
	Args   []interface{}

	// message is kept as a pointer to have shallow copies update this once
	// needed.
	message   *string
	fmt       *string
	formatter logging.Formatter
	formatted string
}

func Record2Level(rec *logging.Record) raven.Severity {
	res := raven.ERROR
	switch rec.Level {
	case logging.WARNING:
		res = raven.WARNING
	case logging.CRITICAL:
		res = raven.FATAL
	}

	return res
}

// :TRICKY: have to fork this code with losses (shouldExcludeErr) because want Params != nil
func CaptureMessageAndWait(message string, tags map[string]string, rec *logging.Record, calldepth int) string {
	client := raven.DefaultClient

	if client == nil {
		return ""
	}

	//if client.shouldExcludeErr(message) {
	//	return ""
	//}

	var fn string
	pc, pathname, line, ok := runtime.Caller(calldepth)
	if ok {
		fn = path.Base(pathname)
	}

	// * aggregation key

	// .fmt is private, f*ck
	//key := rec.fmt

	//key := message
	//if ok {
	//	if f := runtime.FuncForPC(pc); f != nil {
	//		key = fmt.Sprintf("%s:%d:%s", fn, line, f.Name())
	//	}
	//}
	_ = pc

	key := message
	lRec := (*LoggingRecord)(unsafe.Pointer(rec))
	if lRec.fmt != nil {
		key = *lRec.fmt
	}

	packet := raven.NewPacket(message, &raven.Message{key, rec.Args})

	if ok {
		extra := packet.Extra
		extra["filename"] = fn
		extra["lineno"] = line
		extra["pathname"] = pathname
	}

	packet.Level = Record2Level(rec)

	eventID, ch := client.Capture(packet, tags)
	<-ch

	return eventID
}

// just like Python' raven
var sentryLogger = logging.MustGetLogger("sentry.errors")

func CaptureAndWait(message string, stacktrace *raven.Stacktrace, tags map[string]string, level raven.Severity) string {
	client := raven.DefaultClient

	if client == nil {
		return ""
	}

	//if client.shouldExcludeErr(err.Error()) {
	//	return ""
	//}

	// :TRICKY: original CaptureError() use Exception type, which needs proper error type,
	// but we do not have it for go-logging and log packages

	// aggregating is done by stacktrace

	packet := raven.NewPacket(message, stacktrace)

	packet.Level = level

	eventID, ch := client.Capture(packet, tags)
	err := <-ch

	if err != nil {
		sentryLogger.Error(err)
	}

	return eventID
}

func CaptureErrorAndWait(message string, tags map[string]string, calldepth int, level raven.Severity) string {
	client := raven.DefaultClient

	if client == nil {
		return ""
	}

	stacktrace := raven.NewStacktrace(calldepth, 3, client.IncludePaths())
	return CaptureAndWait(message, stacktrace, tags, level)
}


func (l *SentryBackend) Log(level logging.Level, calldepth int, rec *logging.Record) error {
	if (level <= logging.WARNING) && (rec.Module != "sentry.errors") {
		cd := calldepth+2

		//s := rec.Formatted(calldepth+2)
		buf := new(bytes.Buffer)
		logging.DefaultFormatter.Format(cd, rec, buf)
		s := buf.String()
		//fmt.Println(s)

		var tags map[string]string
		if rec.Module != "" {
			tags = map[string]string{
				"module": rec.Module,
			}
		}

		isWarning := level == logging.WARNING
		if isWarning {
			//SendToSentry(s, tags, isWarning)
			CaptureMessageAndWait(s, tags, rec, cd)
		} else {
			//SendToSentry(s, tags, isWarning)
			CaptureErrorAndWait(s, tags, cd, Record2Level(rec))
		}
	}
	return nil
}

//
// :TRICKY: we want LeveledBackend interface to force level WARNING
//

func (l *SentryBackend) GetLevel(module string) logging.Level {
	return logging.WARNING
}

func (l *SentryBackend) SetLevel(level logging.Level, module string) {
}

func (l *SentryBackend) IsEnabledFor(level logging.Level, module string) bool {
	return level <= logging.WARNING
}

func NewSB() logging.LeveledBackend {
	return &SentryBackend{}
}

//
// log
//

type SentryLog struct {
	Writer io.Writer
}

// to emulate standard logger
//var std = log.New(os.Stderr, "", log.LstdFlags)
//var std = log.New(os.Stderr, "", 0)

func SkipSpace(s string) string {
	idx := strings.IndexRune(s, ' ')
	if idx != -1 {
		s = s[idx+1:]
	}
	return s
}

// io.Writer interface for log
func (w *SentryLog) Write(p []byte) (n int, err error) {
	n, err = w.Writer.Write(p)

	s := string(p)
	// because of log.LstdFlags we need to skip 2 spaces
	s = SkipSpace(SkipSpace(s))
	//SendToSentry(s, nil, false)
	CaptureErrorAndWait(s, nil, 4, raven.ERROR)

	return n, err
}

func HookStandardLog(w io.Writer) {
	if w == nil {
		w = os.Stderr
	}

	log.SetOutput(&SentryLog{
		Writer: w,
	})
}

// it's hack,
// use gspt.SetProcTitle()
func SetProcessName(name string) {
	argv0str := (*reflect.StringHeader)(unsafe.Pointer(&os.Args[0]))
	argv0 := (*[1 << 30]byte)(unsafe.Pointer(argv0str.Data))[:argv0str.Len]

	n := copy(argv0, name)
	if n < len(argv0) {
		argv0[n] = 0
	}
}

func WatcheePid() int {
	return os.Getppid()
}

func init() {
	// os.LookupEnv()
	dsn := os.Getenv("_SLOG_WATCHER")

	if dsn != "" {
		go func() {
			// Do not let systemd or so stop the watcher before event is submitted.

			// https://golang.org/pkg/os/signal/#example_Notify
			c := make(chan os.Signal, 1)
			// A SIGHUP, SIGINT, or SIGTERM signal causes the program to exit =>
			// so handle them.
			signal.Notify(c, os.Interrupt)
			signal.Notify(c, syscall.SIGTERM)
			signal.Notify(c, syscall.SIGHUP)

			for s := range c {
				log.Printf("Watcher ignored signal: %s", s)
			}
		}()

		MustSetDSN(dsn)

		s := fmt.Sprintf("Go watcher for pid: %d", WatcheePid())
		//log.Print(s)

		//os.Args[0] = fmt.Sprintf("Go watcher for pid: %d", os.Getppid())
		//SetProcessName(s)
		gspt.SetProcTitle(s)

		ProcessStream(os.Stdin)

		os.Exit(0)
	}
}

func CheckFatal(format string, err error) {
	if err != nil {
		log.Fatalf(format, err)
	}
}

func OpenLog(errFileName string) *os.File {
	logFile, err := os.OpenFile(errFileName,
		os.O_WRONLY|os.O_CREATE|os.O_APPEND, os.FileMode(0640))
	CheckFatal("Can't open: %s", err)
	return logFile
}

func StartWatcher(dsn string, errFileName string) {
	cx, err := osext.Executable()
	CheckFatal("osext.Executable(): %s", err)

	mark := fmt.Sprintf("%s=%s", "_SLOG_WATCHER", dsn)
	env := os.Environ()
	env = append(env, mark)

	var errFile *os.File
	var logFile *os.File
	if errFileName == "" {
		errFile = os.Stderr
		//logFile, err = os.Open(os.DevNull)
		//CheckFatal("Can't open: %s", err)
	} else {
		logFile = OpenLog(errFileName)
		errFile = logFile
	}
	defer func() {
		if logFile != nil {
			logFile.Close()
		}
	}()

	// bad file descriptor
	//in := os.Stderr
	in, wpipe, err := os.Pipe()
	CheckFatal("os.Pipe(): %s", err)

	f := []*os.File{
		in,          // (0) stdin
		os.Stdout,   // (1) stdout
		errFile,     // (2) stderr
	}

	attr := &os.ProcAttr{
		//Dir:   d.WorkDir,
		Env:   env,
		Files: f,
		Sys: &syscall.SysProcAttr{
			//Chroot:     d.Chroot,
			//Credential: d.Credential,
			//Setsid:     true,
		},
	}

	_, err = os.StartProcess(cx, os.Args, attr)
	CheckFatal("Can't start watcher: ", err)

	in.Close()
	// redirect stderr to watcher stdin
	syscall.Dup2(int(wpipe.Fd()), 2)
}

func ProcessStream(in io.Reader) {
	// :TRICKY: stack.ParseDump() searches for
	//    goroutine <N> [<status>]:
	// but every crash starts like that:
	//    panic: <real err>\n
	// see printpanics()

	// :TODO: for debugging purposes add prints if _SLOG_WATCHER_DEBUG=true
	// e.g. systemd kills all processes by default, by KillMode=control-group
	//fmt.Fprintln(os.Stderr, "ProcessStream1")

	wr := NewWR(in)
	goroutines, err := stack.ParseDump(wr, ioutil.Discard)
	if err != nil {
		log.Fatalf("ParseDump: %s", err)
	}

	if len(goroutines) != 0 {
		failedG := goroutines[0]
		//fmt.Println(failedG)

		calls := failedG.Stack.Calls
		var frames []*raven.StacktraceFrame
		for i := range calls {
			call := calls[len(calls)-1-i]

			f := call.Func
			// NewStacktraceFrame
			frame := &raven.StacktraceFrame{
				Filename:     call.SourcePath,
				Function: f.Name(),
				Module: f.PkgName(),

				AbsolutePath: call.SourcePath,
				Lineno: call.Line,
				InApp: false,
			}

			frames = append(frames, frame)
		}

		stacktrace := &raven.Stacktrace{frames}

		accOut := wr.Buf.String()

		CaptureAndWait(fmt.Sprintf("Post-mortem %v, pid=%d: %s", os.Args, WatcheePid(), accOut), stacktrace, nil, raven.FATAL)
	}
}

//type DoubleWriter struct {
//	origWriter io.Writer
//	Buf        *bytes.Buffer
//}
//
//func NewDW(out io.Writer) *DoubleWriter {
//	return &DoubleWriter{
//		origWriter: out,
//		Buf: bytes.NewBuffer(nil),
//	}
//}
//
//func (dw *DoubleWriter) Write(p []byte) (int, error) {
//	n, err := dw.origWriter.Write(p)
//	dw.Buf.Write(p)
//
//	return n, err
//}

type WatchReader struct {
	origReader io.Reader
	Buf        *bytes.Buffer
}

func NewWR(in io.Reader) *WatchReader {
	return &WatchReader{
		origReader: in,
		Buf: bytes.NewBuffer(nil),
	}
}

func (wr *WatchReader) Read(p []byte) (int, error) {
	n, err := wr.origReader.Read(p)
	if n > 0 {
		dat := p[:n]
		os.Stderr.Write(dat)
		wr.Buf.Write(dat)
	}
	return n, err
}

func MustSetDSN(dsn string) {
	err := raven.SetDSN(dsn)
	if err != nil {
		log.Fatalf("Bad Sentry DSN '%s': %s", dsn, err)
	}

	// we don't want to get stuck if not working DSN
	// 5 seconds should be enough to send to Sentry
	ht, ok := raven.DefaultClient.Transport.(*raven.HTTPTransport)
	if ok {
		ht.Timeout = time.Second * 5
	}
}

func SetupLog(logPath string, dsn string) {
	var logWriter io.Writer
	if logPath != "" {
		logWriter = OpenLog(logPath)
	}

	if dsn != "" {
		MustSetDSN(dsn)

		HookStandardLog(logWriter)
		StartWatcher(dsn, logPath)
	} else {
		if logWriter != nil {
			// :TODO: log panics to logPath
			log.SetOutput(logWriter)
		}
	}
}

func SetupLogrus(logPath string, dsn string) {
	// :REFACTOR:
	var logWriter io.Writer
	if logPath != "" {
		logWriter = OpenLog(logPath)
	}

	// *
	if logWriter != nil {
		logrus.SetFormatter(&logrus.TextFormatter{DisableColors: true})
		logrus.SetOutput(logWriter)
	}

	// *
	if dsn != "" {
		MustSetDSN(dsn)

		// :TRICKY: with right timeout 5 sec
		//hook, err := logrus_sentry.NewSentryHook(dsn, []logrus.Level{
		hook, err := logrus_sentry.NewWithClientSentryHook(raven.DefaultClient, []logrus.Level{
			logrus.PanicLevel,
			logrus.FatalLevel,
			logrus.ErrorLevel,
			logrus.WarnLevel,
		})
		CheckFatal("Can't create logrus_sentry.SentryHook: ", err)

		// :TODO: now Sentry errors are being logged to os.Stderr,- "Failed to fire hook: ..."
		// log to a local file, if needed
		hook.Timeout = time.Second * (5 + 1)
		hook.StacktraceConfiguration.Enable = true

		// :TRICKY: logrus doesn't keep original format and args; instead it does fmt.Sprintf(format, args...)
		// several times, so we have to turn on stackraces for warnings also
		// :TODO: fix upstream:
		// - by correcting func (hook *SentryHook) Fire(entry *logrus.Entry)
		// - making packet like packet := raven.NewPacket(message, &raven.Message{format, args})
		hook.StacktraceConfiguration.Level = logrus.WarnLevel

		logrus.AddHook(hook)

		StartWatcher(dsn, logPath)
	}
}

func AddForceErrorOption() *string {
	return flag.StringP("force-error", "", "no", "emulate error for logging {no, error, panic}, default = no")
}

func RunForceErrorOption(forceError string, errorFunc func(string)) {
	switch forceError {
	case "no":
	case "error":
		errorFunc("--force-error")
	case "panic":
		ForceException()
	default:
		log.Fatalf("--force-error' bad choice: %s is not in [no, error, panic]", forceError)
	}
}