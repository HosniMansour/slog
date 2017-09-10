package slog

import (
	"bytes"
	"fmt"
	"github.com/erikdubbelboer/gspt"
	"github.com/getsentry/raven-go"
	"github.com/op/go-logging"
	"github.com/sirupsen/logrus"
	"log"
	"math/rand"
	"net/http"
	"os"
	"testing"
	"time"
)

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

var seedDone = false

func RandStringBytes(n int) string {
	if !seedDone {
		seedDone = true
		rand.Seed(time.Now().UnixNano())
	}

	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

func TestSlog(t *testing.T) {
	dsn := os.Args[2]
	//dsn = "http://aaa:bbb@fr2-v-cdn-hop-1.be.core.pw:994/2"
	MustSetDSN(dsn)

	if false {
		//go ForceException()
		ForceException()

		raven.CapturePanic(func() {
			ForceException()
		}, nil)

		time.Sleep(time.Hour)
	}

	if false {
		logger := logging.MustGetLogger("example")

		logger.Errorf("error: %s", "arg")

		//backend := logging.NewLogBackend(os.Stdout, "prefix", 0)
		backend := NewSB()
		logging.SetBackend(backend)

		// :TRICKY: stacktrace aggregation = frames aggregation is being done by
		// function and if sources exists locally, by context_line => so
		// changing line (e.g. adding space character) will break aggregation

		// and without sources errors in the same functions will be aggregated
		// :TODO: append Message interface like for CaptureMessageAndWait()

		//logFunc := logger.Errorf
		logFunc := logger.Warningf

		logFunc("error - %s", RandStringBytes(8))

		logFunc("another error - %s",
			RandStringBytes(8))
	}

	if false {
		http.DefaultClient.Timeout = time.Second * 5
		resp, err := http.Get("http://fr2-v-cdn-hop-1.be.core.pw:994/ggg")

		_ = resp
		fmt.Println(err)
		CheckError(err)
	}

	if false {
		log.Println("msg1")

		HookStandardLog(nil)

		//log.Println("msg2")

		log.Fatal("fatal msg: ", RandStringBytes(8))
	}

	if false {
		// :REFACTOR:
		logger := logging.MustGetLogger("example")
		stderrBackend := logging.NewLogBackend(os.Stderr, "", log.LstdFlags)
		backend := NewSB()
		logging.SetBackend(stderrBackend, backend)

		//logger.Warningf("warning: %s", RandStringBytes(8))

		//logger.Warning("static message")
		//logger.Fatal("fatal message")

		type Status struct {
			Vhost  string `protobuf:"bytes,1,opt,name=vhost" json:"vhost,omitempty"`
			Node   string `protobuf:"bytes,2,opt,name=node" json:"node,omitempty"`
			Status string `protobuf:"bytes,3,opt,name=status" json:"status,omitempty"`
			Error  string `protobuf:"bytes,4,opt,name=error" json:"error,omitempty"`
		}

		logger.Errorf("Can't post status: %s, %+v", "502", Status{
			Vhost:  "test.ru",
			Node:   "test-node",
			Status: "failed",
			Error:  RandStringBytes(8),
		})
	}

	if false {
		fmt.Println(RandStringBytes(8))
		fmt.Println(RandStringBytes(8))
	}

	if false {
		stdErr := `
panic: runtime error: integer divide by zero

goroutine 26 [running]:
panic(0x978840, 0xc420010140)
        /home/ilya/opt/programming/golang/git/src/runtime/panic.go:500 +0x1a1
slog.ForceException()
        /home/ilya/opt/programming/g-core/cdn-tools/src/slog/slog.go:19 +0x2c
main.main.func1.1()
        /home/ilya/opt/programming/g-core/cdn-tools/src/mapi/main.go:166 +0x14
created by main.main.func1
        /home/ilya/opt/programming/g-core/cdn-tools/src/mapi/main.go:167 +0xc8
`
		in := bytes.NewBufferString(stdErr)

		ProcessStream(in)
	}

	if false {
		//SetProcessName("ggg")
		gspt.SetProcTitle("ggg")
		i := 0
		_ = i
	}

	if false {
		//SetupLogrus("logrus-test.log", dsn)
		SetupLogrus("logrus-test.log", dsn)

		logrus.Warnf("Random text simulate: %s", RandStringBytes(8))
		//logrus.Errorf("Random text simulate: %s", RandStringBytes(8))
	}
}

// :REFACTOR:
func CheckError(err error) {
	if err != nil {
		panic(err)
	}
}

func Assert(b bool) {
	if !b {
		panic("Assertion error")
	}
}
