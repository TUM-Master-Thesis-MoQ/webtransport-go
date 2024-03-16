package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/quic-go/webtransport-go"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/interop/http09"
	"github.com/quic-go/quic-go/interop/utils"
)

var tlsConf *tls.Config

func main() {
	logFile, err := os.Create("/logs/log.txt")
	if err != nil {
		fmt.Printf("Could not create log file: %s\n", err.Error())
		os.Exit(1)
	}
	defer logFile.Close()
	log.SetOutput(logFile)

	keyLog, err := utils.GetSSLKeyLog()
	if err != nil {
		fmt.Printf("Could not create key log: %s\n", err.Error())
		os.Exit(1)
	}
	if keyLog != nil {
		defer keyLog.Close()
	}

	testcase := os.Getenv("TESTCASE")

	quicConf := &quic.Config{
		Tracer:          utils.NewQLOGConnectionTracer,
		EnableDatagrams: true,
	}
	cert, err := tls.LoadX509KeyPair("/certs/cert.pem", "/certs/priv.key")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	tlsConf = &tls.Config{
		Certificates: []tls.Certificate{cert},
		KeyLogWriter: keyLog,
	}

	switch testcase {
	case "handshake":
		err = runHTTP09Server(quicConf)
	default:
		fmt.Printf("unsupported test case: %s\n", testcase)
		os.Exit(127)
	}

	if err != nil {
		fmt.Printf("Error running server: %s\n", err.Error())
		os.Exit(1)
	}
}

type stream struct {
	webtransport.Stream
}

var _ quic.Stream = &stream{}

func (s stream) Context() context.Context { panic("implement me") }

func (s stream) CancelRead(code quic.StreamErrorCode) {
	s.Stream.CancelRead(webtransport.StreamErrorCode(code))
}

func (s stream) CancelWrite(code quic.StreamErrorCode) {
	s.Stream.CancelWrite(webtransport.StreamErrorCode(code))
}

func runHTTP09Server(quicConf *quic.Config) error {
	// create a new webtransport.Server, listening on (UDP) port 443
	s := webtransport.Server{
		H3: http3.Server{
			Addr:       ":443",
			TLSConfig:  tlsConf,
			QuicConfig: quicConf,
		},
	}

	fileSystemHandler := http.NewServeMux()
	fileSystemHandler.Handle("/", http.FileServer(http.Dir("/www")))
	h09 := http09.Server{Server: &http.Server{Handler: fileSystemHandler}}

	// Create a new HTTP endpoint /webtransport.
	http.HandleFunc("/webtransport", func(w http.ResponseWriter, r *http.Request) {
		c, err := s.Upgrade(w, r)
		if err != nil {
			log.Printf("upgrading failed: %s", err)
			w.WriteHeader(500)
			return
		}
		for {
			str, err := c.AcceptStream(context.Background())
			if err != nil {
				log.Printf("Error accepting stream: %s\n", err.Error())
				return
			}
			go func() {
				if err := h09.HandleStream(stream{Stream: str}); err != nil {
					log.Printf("Handling stream failed: %s\n", err.Error())
				}
			}()
		}
		// Handle the connection. Here goes the application logic.
	})

	return s.ListenAndServe()
}
